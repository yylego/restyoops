package restyoops

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/go-resty/resty/v2"
)

// CheckFunc inspects an attempt and reports endpoint-specific faults.
// The response can be absent / incomplete. A nil result proceeds to the next check,
// then built-in detection if no check reports a fault.
//
// CheckFunc 检查一次已完成的尝试，报告内置检测看不出的故障
// 响应可能为 nil 或不完整，读取前应检查对应字段。
// 返回 nil 时继续后续检查；均未报告故障时使用内置检测。
type CheckFunc func(resp *resty.Response, cause error) *Oops

// Detective holds detection rules and can configure a Resty client.
// Detective 保存一套检测策略，并把它装到 resty 客户端上
type Detective struct {
	attempts    int
	waitTime    time.Duration
	maxWaitTime time.Duration
	statusRules map[int]Rule
	kindRules   map[Kind]Rule
	checks      []CheckFunc
	methods     map[string]bool
	faultAsOops bool
}

// NewDetective builds a Detective with default fault and repeat rules.
// Options adapt those rules to the endpoint's contract.
//
// NewDetective 构造使用默认分类与重试规则的 Detective。
// 具体接口的约定通过 Option 调整。
func NewDetective(options ...Option) *Detective {
	detective := &Detective{
		attempts:    3,
		waitTime:    100 * time.Millisecond,
		maxWaitTime: 30 * time.Second,
		statusRules: make(map[int]Rule),
		kindRules:   make(map[Kind]Rule),
		checks:      make([]CheckFunc, 0),
		methods:     safeMethods(),
		// Status faults become errors, so "if err != nil" also catches them.
		//
		// 把故障变成 error，意味着一个 "if err != nil" 就能兜住全部情况，
		// 而不是要求每个调用点还得记着再查一次 resp.IsError()
		faultAsOops: true,
	}
	for _, option := range options {
		option(detective)
	}
	return detective
}

// safeMethods lists HTTP methods with idempotent semantics.
// safeMethods 列出 HTTP 语义上具有幂等性的方法；具体接口仍需遵守该约定。
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
// An existing verdict is reused unless the request context has ended.
//
// Detect 对一次已完成的尝试分类，没出问题时返回 nil
// 已有分类通常直接沿用；请求 context 已结束时返回停止结论。
func (d *Detective) Detect(resp *resty.Response, cause error) *Oops {
	// Fault rules cannot revive a stopped request context.
	// 请求 context 已经结束时，分类规则不能让它继续重试。
	if resp != nil && resp.Request != nil {
		if stopped := resp.Request.Context().Err(); stopped != nil {
			return d.settle(NewOops(KindCanceled, stopped, Abort()), resp)
		}
	}
	if errors.Is(cause, context.Canceled) {
		return d.settle(NewOops(KindCanceled, cause, Abort()), resp)
	}
	// Reuse an existing classification.
	// 上游已经分类过了，沿用那个结论，别再造一个
	var settled *Oops
	if cause != nil && errors.As(cause, &settled) {
		return settled
	}

	// Custom checks override status-based rules.
	// 自定义检查了解对端，因此它的结论高于任何从状态码推出来的结论
	for _, check := range d.checks {
		if oops := check(resp, cause); oops != nil {
			return d.settle(oops, resp)
		}
	}

	if cause != nil {
		oops := detectCause(cause)
		d.useRule(oops)
		return d.settle(oops, resp)
	}

	if resp == nil || resp.RawResponse == nil || resp.StatusCode() < 400 {
		return nil
	}

	oops := detectStatus(resp.StatusCode())
	d.useRule(oops)
	return d.settle(oops, resp)
}

// useRule selects a status-specific rule if present, else a kind-based rule.
//
// useRule 让配置的规则覆盖内置结论
// 针对单个状态码的规则优先于覆盖整个分类的规则
func (d *Detective) useRule(oops *Oops) {
	if rule, ok := d.statusRules[oops.StatusCode]; ok && oops.StatusCode > 0 {
		oops.take(rule)
		return
	}
	if rule, ok := d.kindRules[oops.Kind]; ok {
		oops.take(rule)
	}
}

// settle adds request metadata, respects endpoint waits and guards unsafe repeats.
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

	// Configured waits must respect the endpoint's minimum wait.
	// 本地配置的等待不能缩短对端要求的最短等待。
	if oops.Retryable {
		if waitTime, ok := responseWaitDuration(resp); ok {
			oops.WaitTime = max(oops.WaitTime, waitTime)
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

// allowRepeat checks the configured method set before repeating an operation.
//
// allowRepeat 判断把这个请求再发一次是否安全
// 安全集合之外的方法可能已经生效了，重复发送会让它生效两次
func (d *Detective) allowRepeat(oops *Oops) bool {
	if oops.Method == "" {
		return true
	}
	if d.methods[strings.ToUpper(oops.Method)] {
		return true
	}
	return false
}

// Setup configures fault detection and repeats on a Resty client.
// Resty owns the loop, backoff and attempt count.
//
// Setup 把这套策略装到 resty 客户端上，并把客户端返回以便继续链式调用
// 重试循环、退避算法和尝试计数 resty 本来就有，这里一概不重造
// 这里补上的是 resty 留给使用方自己决定的那部分判断
//
// Invoke once before use. RetryAfter also checks accepted repeats.
// Existing business loops should use Detect without Setup.
// 使用前调用一次；即使其他条件允许重试，RetryAfter 仍会检查本包的策略。
// 业务已有重试循环时，应直接使用 Detect，不调用 Setup。
func (d *Detective) Setup(client *resty.Client) *resty.Client {
	client.SetRetryCount(d.attempts)
	client.SetRetryWaitTime(d.waitTime)
	client.SetRetryMaxWaitTime(d.maxWaitTime)

	// Resty accepts the first true condition, so the wait callback also checks the verdict.
	// Resty 遇到第一个 true 就允许重试，因此等待回调还会检查分类结论。
	client.AddRetryCondition(func(resp *resty.Response, cause error) bool {
		oops := d.Detect(resp, cause)
		keepOops(resp, oops)
		// Stop if the stated wait exceeds the budget.
		// 等待超出预算时停止，避免 Resty 截短等待后提前请求。
		return oops != nil && oops.Retryable && (!oops.fixedWait || oops.WaitTime <= d.maxWaitTime)
	})

	// Request conditions can accept first. The hook retains the cause,
	// since RetryAfter receives no error argument.
	// 请求级条件可能在本包条件执行前就放行。此时通过 hook 记录原因，
	// 因为 RetryAfter 没有 error 参数。
	client.AddRetryHook(func(resp *resty.Response, cause error) {
		if takeOops(resp) == nil {
			keepOops(resp, d.Detect(resp, cause))
		}
	})

	client.SetRetryAfter(func(_ *resty.Client, resp *resty.Response) (time.Duration, error) {
		oops := takeOops(resp)
		if oops == nil {
			oops = d.Detect(resp, nil)
		}
		if oops == nil {
			return 0, errors.New("restyoops: RETRY DECLINED WITHOUT A CLASSIFIED FAULT")
		}
		if !oops.Retryable || (oops.fixedWait && oops.WaitTime > d.maxWaitTime) {
			return 0, oops
		}
		if !oops.fixedWait {
			return 0, nil // Use Resty's backoff. // 使用 Resty 的退避。
		}
		if oops.WaitTime <= 0 {
			return time.Nanosecond, nil // Resty applies its minimum wait. // Resty 仍会应用等待下限。
		}
		return oops.WaitTime, nil
	})

	client.OnBeforeRequest(func(_ *resty.Client, req *resty.Request) error {
		initOops(req)
		return nil
	})

	// Response checks turn detected faults into errors.
	//
	// 只靠状态码表达的故障会让 err 为 nil，读起来像成功，习惯上就被跳过了
	// 把它作为 error 报出来，就把故障放回了这门语言期待它出现的位置
	if d.faultAsOops {
		client.OnAfterResponse(func(_ *resty.Client, resp *resty.Response) error {
			if oops := d.Detect(resp, nil); oops != nil {
				return oops
			}
			return nil
		})
	}

	return client
}

// pocket holds the attempt's verdict across Resty callbacks.
//
// pocket 保存当前这一轮的结论，让前后相邻的两个 resty 回调能共用它
// 把指针放进请求 context 就能一直留着，因为 context 里的值本身不变、变的是它指向的内容
type pocket struct {
	oops *Oops
}

// pocketMark is a private context slot type to avoid collisions.
// pocketMark 命名 context 中的槽位，用私有类型确保外部无法与之冲突
type pocketMark struct{}

// initOops starts each attempt with a fresh verdict slot.
// initOops 为每次尝试创建独立的分类槽位，避免沿用上一次的结论。
func initOops(req *resty.Request) {
	if req == nil {
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

// takeOops reads the attempt's saved verdict, nil if absent.
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
