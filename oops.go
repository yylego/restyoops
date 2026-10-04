package restyoops

import (
	"strconv"
	"strings"
	"time"
)

// Oops describes a request fault and the advice on repeats.
// Oops implements error and supports errors.As.
//
// Oops 描述一次请求故障：发生了什么，以及再发一次是否有意义
// Oops 本身就是 error，因此能走通常的错误链路，也能被 errors.As 取出
type Oops struct {
	Kind        Kind          // Nature of the fault // 故障性质
	StatusCode  int           // HTTP status code, 0 when no response arrived // HTTP 状态码，没拿到响应时为 0
	ContentType string        // Response Content-Type // 响应的 Content-Type
	Method      string        // Request method // 请求方法
	URL         string        // Request URL // 请求 URL
	Attempt     int           // Which attempt produced it, counting from 1 // 这是第几次尝试产生的，从 1 开始
	Retryable   bool          // Permits repeats subject to business limits. // 再发一次是否有意义
	WaitTime    time.Duration // Requested wait; use WaitTimeOf to distinguish unset from zero // 建议等待时长；用 WaitTimeOf 区分未指定与零等待
	Cause       error         // Underlying cause; can be nil with status-based faults // 底层原因；状态码故障可以为 nil

	// fixedWait distinguishes an explicit zero from an unspecified wait.
	//
	// fixedWait 标记等待时长是被有意给出的，用来把"故意写 0"和"没写"区分开
	fixedWait bool
}

// NewOops builds an Oops to express faults found in custom checks.
// Without a rule the fault counts as not retryable, since giving up is the safe default
//
// NewOops 构造一个 Oops，供自定义检查使用，用来表达内置检测看不出的故障
// 不给规则时故障按不可重试处理，因为放弃才是安全的默认值
func NewOops(kind Kind, cause error, rules ...Rule) *Oops {
	oops := &Oops{
		Kind:      kind,
		Retryable: false,
		Cause:     cause,
	}
	for _, rule := range rules {
		oops.take(rule)
	}
	return oops
}

// take lets a rule decide the outcome of this fault
// take 让一条规则决定这个故障的处置结果
func (o *Oops) take(rule Rule) {
	waitTime, fixed := rule.pick()
	o.Retryable = rule.retryable
	o.WaitTime = waitTime
	o.fixedWait = fixed
}

// Error spells out the fault, leading with the pieces needed to locate it
// Error 把故障说清楚，把定位所需的信息放在前面
func (o *Oops) Error() string {
	var buf strings.Builder
	buf.WriteString("restyoops: ")
	buf.WriteString(string(o.Kind))
	if o.Method != "" {
		buf.WriteString(" ")
		buf.WriteString(o.Method)
	}
	if o.URL != "" {
		buf.WriteString(" ")
		buf.WriteString(o.URL)
	}
	if o.StatusCode > 0 {
		buf.WriteString(" status=")
		buf.WriteString(strconv.Itoa(o.StatusCode))
	}
	if o.Attempt > 0 {
		buf.WriteString(" attempt=")
		buf.WriteString(strconv.Itoa(o.Attempt))
	}
	if o.Retryable {
		buf.WriteString(" retryable")
		if o.WaitTime > 0 {
			buf.WriteString(" wait=")
			buf.WriteString(o.WaitTime.String())
		}
	}
	if o.Cause != nil {
		buf.WriteString(": ")
		buf.WriteString(o.Cause.Error())
	}
	return buf.String()
}

// Unwrap exposes the underlying cause, so errors.Is and errors.As reach through
// Unwrap 暴露底层原因，让 errors.Is 和 errors.As 能穿透到内层
func (o *Oops) Unwrap() error {
	return o.Cause
}
