package restyoops

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/go-resty/resty/v2"
)

// CheckFunc looks at a finished attempt and reports a fault the built-in detection cannot see
// It gets the whole response, so headers, payload and the request behind it are all reachable
// Returning nil hands the decision back, letting the built-in detection carry on
//
// CheckFunc 检查一次已完成的尝试，报告内置检测看不出的故障
// 它拿到整个响应，因此头、报文和背后的请求都能读到
// 返回 nil 表示把决定权交还回去，继续走内置检测
type CheckFunc func(resp *resty.Response, cause error) *Oops

// Detective holds one detection policy and installs it onto a resty client
// Detective 保存一套检测策略，并把它装到 resty 客户端上
type Detective struct {
	attempts     int
	waitTime     time.Duration
	maxWaitTime  time.Duration
	statusRules  map[int]Rule
	kindRules    map[Kind]Rule
	checks       []CheckFunc
	methods      map[string]bool
	errorOnFault bool
}

// NewDetective builds a Detective that already decides correctly without being configured
// Options adjust it where the peer needs something the defaults cannot know about
//
// NewDetective 构造一个不配置也能做出正确判断的 Detective
// 当对端有默认值无从知晓的特殊要求时，再用 Option 去调整
func NewDetective(options ...Option) *Detective {
	detective := &Detective{
		attempts:    3,
		waitTime:    100 * time.Millisecond,
		maxWaitTime: 30 * time.Second,
		statusRules: make(map[int]Rule),
		kindRules:   make(map[Kind]Rule),
		checks:      make([]CheckFunc, 0),
		methods:     safeMethods(),
		// Turning a fault into an error means one "if err != nil" catches the whole set,
		// instead of asking each call site to also remember resp.IsError()
		//
		// 把故障变成 error，意味着一个 "if err != nil" 就能兜住全部情况，
		// 而不是要求每个调用点还得记着再查一次 resp.IsError()
		errorOnFault: true,
	}
	for _, option := range options {
		option(detective)
	}
	return detective
}

// safeMethods lists the methods that can be sent twice without changing the outcome
// safeMethods 列出重复发送不会改变结果的方法
func safeMethods() map[string]bool {
	return map[string]bool{
		"GET":     true,
		"HEAD":    true,
		"OPTIONS": true,
		"TRACE":   true,
		"PUT":     true,
		"DELETE":  true,
	}
}

// Detect classifies one finished attempt, returning nil when nothing went wrong
// Detect is safe to call again on its own output, so it can sit anywhere in the flow
//
// Detect 对一次已完成的尝试分类，没出问题时返回 nil
// Detect 可以安全地作用在自己的输出上，因此放在流程的任何位置都可以
func (d *Detective) Detect(resp *resty.Response, cause error) *Oops {
	// Already classified upstream, so keep that verdict rather than build a second one
	// 上游已经分类过了，沿用那个结论，别再造一个
	var settled *Oops
	if cause != nil && errors.As(cause, &settled) {
		return settled
	}

	// Custom checks know the peer, so their verdict outranks anything derived from status
	// 自定义检查了解对端，因此它的结论高于任何从状态码推出来的结论
	for _, check := range d.checks {
		if oops := check(resp, cause); oops != nil {
			return d.settle(oops, resp)
		}
	}

	if cause != nil {
		oops := detectCause(cause)
		d.applyRule(oops)
		return d.settle(oops, resp)
	}

	if resp == nil || resp.RawResponse == nil || resp.StatusCode() < 400 {
		return nil
	}

	oops := detectStatus(resp.StatusCode())
	d.applyRule(oops)
	return d.settle(oops, resp)
}

// applyRule lets configured rules override the built-in verdict
// A rule pinned to one status code beats a rule covering the whole kind
//
// applyRule 让配置的规则覆盖内置结论
// 针对单个状态码的规则优先于覆盖整个分类的规则
func (d *Detective) applyRule(oops *Oops) {
	if rule, ok := d.statusRules[oops.StatusCode]; ok && oops.StatusCode > 0 {
		oops.take(rule)
		return
	}
	if rule, ok := d.kindRules[oops.Kind]; ok {
		oops.take(rule)
	}
}

// settle fills in what the request knows, honours the peer's wait, and guards unsafe repeats
// settle 补上请求侧已知的信息，尊重对端要求的等待，并守住不安全的重复发送
func (d *Detective) settle(oops *Oops, resp *resty.Response) *Oops {
	if resp != nil {
		if resp.Request != nil {
			oops.Method = resp.Request.Method
			oops.URL = resp.Request.URL
			oops.Attempt = resp.Request.Attempt
		}
		if resp.RawResponse != nil {
			if oops.StatusCode == 0 {
				oops.StatusCode = resp.StatusCode()
			}
			if oops.ContentType == "" {
				oops.ContentType = resp.Header().Get("Content-Type")
			}
		}
	}

	// The peer stating how long to hold off outranks any guess made on this side,
	// though a wait asked for on purpose still stands, since that was a deliberate choice
	//
	// 对端明确说了该停多久，这高于本方的任何猜测，
	// 但被有意指定的等待时长仍然作数，因为那是明确做出的选择
	if oops.Retryable && !oops.fixedWait {
		if waitTime, ok := retryAfterOf(resp); ok {
			oops.WaitTime = waitTime
			oops.fixedWait = true
		}
	}

	if oops.Retryable && !d.allowRepeat(oops) {
		oops.Retryable = false
		oops.WaitTime = 0
		oops.fixedWait = false
	}
	return oops
}

// allowRepeat reports whether sending this request a second time is safe
// A method outside the safe set may have taken effect already, and a repeat would double it
// Rate limiting is the exception, since the peer turned the request away without acting on it
//
// allowRepeat 判断把这个请求再发一次是否安全
// 安全集合之外的方法可能已经生效了，重复发送会让它生效两次
// 限流是例外，因为对端是把请求挡回来的，并没有对它做任何处理
func (d *Detective) allowRepeat(oops *Oops) bool {
	if oops.Method == "" {
		return true
	}
	if d.methods[strings.ToUpper(oops.Method)] {
		return true
	}
	return oops.Kind == KindThrottle
}

// Setup installs this policy onto a resty client and hands the client back for chaining
// resty already owns the retry loop, the backoff and the attempt counting, so none of that
// gets rebuilt here. What gets supplied is the judgement resty leaves to its users
//
// Setup 把这套策略装到 resty 客户端上，并把客户端返回以便继续链式调用
// 重试循环、退避算法和尝试计数 resty 本来就有，这里一概不重造
// 这里补上的是 resty 留给使用方自己决定的那部分判断
func (d *Detective) Setup(client *resty.Client) *resty.Client {
	client.SetRetryCount(d.attempts)
	client.SetRetryWaitTime(d.waitTime)
	// Left at the resty default the ceiling sits at two seconds, which would quietly cut a
	// peer's request to wait far longer down to two seconds and defeat the point of reading it
	//
	// 保持 resty 默认值时上限只有两秒，那会把对端"请等更久"的要求悄悄砍成两秒，
	// 让读取该要求这件事失去意义
	client.SetRetryMaxWaitTime(d.maxWaitTime)

	// resty starts each round assuming a transport fault is worth repeating, then lets every
	// condition overwrite that assumption. One condition answering "no" therefore silences the
	// assumption entirely. Holding the only condition keeps that from happening by accident
	//
	// resty 每轮先假定传输故障值得重试，然后让每个条件去覆盖这个假定
	// 因此只要有一个条件回答"否"，这个假定就被整个抹掉
	// 由本方持有唯一的条件，可以避免这件事在无意中发生
	client.AddRetryCondition(func(resp *resty.Response, cause error) bool {
		oops := d.Detect(resp, cause)
		keepOops(resp, oops)
		return oops != nil && oops.Retryable
	})

	// resty asks this right after the condition, within the same round, but hands over only the
	// response. The verdict the condition just reached gets carried across, so a transport fault
	// keeps the wait its rule asked for instead of losing it on the way
	//
	// resty 在条件之后、同一轮之内问这个，但只把响应交过来
	// 因此把条件刚得出的结论带过来，让传输故障保住其规则要求的等待时长，而不是在中途丢掉
	client.SetRetryAfter(func(_ *resty.Client, resp *resty.Response) (time.Duration, error) {
		oops := takeOops(resp)
		if oops == nil {
			oops = d.Detect(resp, nil)
		}
		if oops == nil || !oops.fixedWait {
			return 0, nil // Nothing better than the backoff, so let the backoff have it // 没有比退避更好的答案，就交给退避
		}
		if oops.WaitTime <= 0 {
			return time.Nanosecond, nil // Asked for on purpose: go again now // 明确要求的：立刻再来
		}
		return oops.WaitTime, nil
	})

	client.OnBeforeRequest(func(_ *resty.Client, req *resty.Request) error {
		carryOops(req)
		return nil
	})

	// A fault reported through the status code alone leaves err nil, which reads as success and
	// gets skipped by habit. Reporting it as an error puts it where the language expects it
	//
	// 只靠状态码表达的故障会让 err 为 nil，读起来像成功，习惯上就被跳过了
	// 把它作为 error 报出来，就把故障放回了这门语言期待它出现的位置
	if d.errorOnFault {
		client.OnAfterResponse(func(_ *resty.Client, resp *resty.Response) error {
			if oops := d.Detect(resp, nil); oops != nil {
				return oops
			}
			return nil
		})
	}

	return client
}

// pocket holds the verdict of the round in progress, letting the two resty callbacks that run
// back to back share it. A pointer parked in the request context survives, since the context
// value never changes even though what it points at does
//
// pocket 保存当前这一轮的结论，让前后相邻的两个 resty 回调能共用它
// 把指针放进请求 context 就能一直留着，因为 context 里的值本身不变、变的是它指向的内容
type pocket struct {
	oops *Oops
}

// pocketMark names the context slot, using a private type so nothing else can collide with it
// pocketMark 命名 context 中的槽位，用私有类型确保外部无法与之冲突
type pocketMark struct{}

// carryOops parks a pocket on the request, once, so it stays put across the whole round
// carryOops 在请求上放置一个 pocket，只放一次，让它在整轮中一直存在
func carryOops(req *resty.Request) {
	if req == nil || req.Context().Value(pocketMark{}) != nil {
		return
	}
	req.SetContext(context.WithValue(req.Context(), pocketMark{}, &pocket{}))
}

// keepOops records the verdict so the next callback in the round can read it
// keepOops 记下结论，让同一轮的下一个回调能读到
func keepOops(resp *resty.Response, oops *Oops) {
	if resp == nil || resp.Request == nil {
		return
	}
	if box, ok := resp.Request.Context().Value(pocketMark{}).(*pocket); ok {
		box.oops = oops
	}
}

// takeOops reads back the verdict recorded earlier in the round, nil when there is none
// takeOops 取回本轮早先记下的结论，没有时返回 nil
func takeOops(resp *resty.Response) *Oops {
	if resp == nil || resp.Request == nil {
		return nil
	}
	if box, ok := resp.Request.Context().Value(pocketMark{}).(*pocket); ok {
		return box.oops
	}
	return nil
}
