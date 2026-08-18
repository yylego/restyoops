package restyoops

import "time"

// Rule is the decision about one fault: send it again, and how long to hold before that
// Rule 是针对一种故障的决定：要不要再发一次，以及发之前先停多久
type Rule struct {
	retryable bool
	waitTime  time.Duration
	fixed     bool // whether the wait was stated, telling a stated zero from an absent one // 等待时长是否被明确给出，用来区分"明确写 0"和"没写"
}

// Again allows another attempt
// Without a duration the backoff picks the wait, growing it with each attempt, which suits
// almost every case. With a duration that wait is used as-is and stays flat across attempts,
// so a stated zero asks for the shortest wait rather than a growing one
//
// One limit comes from resty and cannot be lifted here: it raises any wait up to the floor set
// through WithWaitTime. A wait shorter than that floor, a stated zero included, becomes the floor
//
// Again 允许再试一次
// 不给时长时由退避算法决定等多久，且随尝试次数增长，这适合绝大多数场景
// 给了时长就按该时长等待，且不随尝试次数变化，因此明确写 0 表示要最短的等待而非增长的等待
//
// 有一条限制来自 resty，本包无法解除：它会把任何等待时长抬高到 WithWaitTime 设定的下限
// 短于该下限的等待时长（包括明确写下的 0）都会变成该下限
func Again(waitTimes ...time.Duration) Rule {
	rule := Rule{retryable: true}
	for _, waitTime := range waitTimes {
		rule.waitTime = waitTime
		rule.fixed = true
	}
	return rule
}

// Abort refuses another attempt
// Abort 拒绝再试
func Abort() Rule {
	return Rule{retryable: false}
}

// pick reads the wait a rule asks for, telling whether the rule stated one
// pick 读出规则要求的等待时长，并说明该规则是否明确给出了时长
func (r Rule) pick() (time.Duration, bool) {
	return r.waitTime, r.fixed
}
