package restyoops

import "time"

// Rule is the decision about one fault: send it again, and how long to hold before that
// Rule 是针对一种故障的决定：要不要再发一次，以及发之前先停多久
type Rule struct {
	retryable bool
	waitTime  time.Duration
	fixed     bool // Marks an explicit wait, including zero. // 等待时长是否被明确给出，包括零。
}

// Again permits repeats. An omitted duration uses backoff; an explicit duration stays fixed.
// Resty applies WithWaitTime's minimum, and Retry-After can increase the wait.
//
// Again 允许重试。未指定时长时使用退避算法，指定时长时使用固定等待。
// Resty 仍会应用 WithWaitTime 的等待下限，Retry-After 也可以延长等待。
func Again(waitTimes ...time.Duration) Rule {
	rule := Rule{retryable: true}
	for _, waitTime := range waitTimes {
		rule.waitTime = waitTime
		rule.fixed = true
	}
	return rule
}

// Abort stops repeats.
// Abort 拒绝再试
func Abort() Rule {
	return Rule{retryable: false}
}

// pick returns the wait and its presence flag.
// pick 读出规则要求的等待时长，并说明该规则是否明确给出了时长
func (rule Rule) pick() (time.Duration, bool) {
	return rule.waitTime, rule.fixed
}
