package restyoops

import (
	"strings"
	"time"
)

// Option adjusts a Detective where the peer needs something the defaults cannot know about
// Option 在对端有默认值无从知晓的特殊要求时，用来调整 Detective
type Option func(*Detective)

// WithAttempts sets how many further attempts follow the first one
// Zero means the request goes out once and its verdict stands
//
// WithAttempts 设置首次之后还要再试几次
// 设为 0 表示请求只发一次，结果就是最终结果
func WithAttempts(attempts int) Option {
	return func(d *Detective) {
		d.attempts = attempts
	}
}

// WithWaitTime sets the floor and the ceiling of the backoff
// The ceiling also caps a wait the peer asks for, so leaving room matters when peers throttle
//
// WithWaitTime 设置退避的下限和上限
// 该上限同样会截断对端要求的等待时长，因此在对端会限流时要留足空间
func WithWaitTime(waitTime, maxWaitTime time.Duration) Option {
	return func(d *Detective) {
		d.waitTime = waitTime
		d.maxWaitTime = maxWaitTime
	}
}

// WithStatus decides one status code, outranking whatever the kind of that status says
// It refines a verdict rather than creating one, so it reaches only codes counted as faults,
// meaning 400 and above. Turning a lesser code into a fault is what WithCheck is there for
//
// WithStatus 决定某个状态码的处置，优先于该状态所属分类的处置
// 它调整的是已有结论而不是造出结论，因此只作用于被判为故障的状态码，即 400 及以上
// 想把更小的状态码变成故障，那是 WithCheck 的职责
func WithStatus(statusCode int, rule Rule) Option {
	return func(d *Detective) {
		d.statusRules[statusCode] = rule
	}
}

// WithKind decides a whole kind of fault at once
// It reaches the verdicts the built-in detection reaches, not the ones a check reports, since a
// check saw the response itself and its verdict is the better informed of the two
//
// WithKind 一次决定一整类故障的处置
// 它作用于内置检测得出的结论，而不是检查函数报出的结论，
// 因为检查函数亲眼看过响应，两者之中它的结论掌握的信息更多
func WithKind(kind Kind, rule Rule) Option {
	return func(d *Detective) {
		d.kindRules[kind] = rule
	}
}

// WithCheck adds a check that sees faults the built-in detection cannot
// Checks run in the order given, and the first one reporting a fault decides
//
// WithCheck 添加一个检查，用来发现内置检测看不出的故障
// 多个检查按给定顺序执行，第一个报出故障的说了算
func WithCheck(check CheckFunc) Option {
	return func(d *Detective) {
		d.checks = append(d.checks, check)
	}
}

// WithRepeatMethods states which methods are safe to send twice, replacing the default set
// Passing a method that changes state each time it lands accepts that it may land twice
//
// WithRepeatMethods 声明哪些方法重复发送是安全的，替换掉默认集合
// 传入一个每次到达都会改变状态的方法，就等于接受它可能到达两次
func WithRepeatMethods(methods ...string) Option {
	return func(d *Detective) {
		d.methods = make(map[string]bool, len(methods))
		for _, method := range methods {
			d.methods[strings.ToUpper(method)] = true
		}
	}
}

// WithErrorOnFault decides whether a fault seen through the status code alone becomes an error
// Turning it off keeps resty's own shape, where a 500 arrives with a nil error
//
// WithErrorOnFault 决定只通过状态码发现的故障是否要变成 error 返回
// 关掉它就保持 resty 原本的形态，即 500 会伴随 nil 的 error 返回
func WithErrorOnFault(errorOnFault bool) Option {
	return func(d *Detective) {
		d.errorOnFault = errorOnFault
	}
}
