// Package restyoops classifies request faults and advises on repeats and waits.
// Detect supports business-owned loops; Setup configures Resty's built-in loop.
//
// restyoops 对请求故障分类，并给出是否重试及等待多久的建议。
// Detect 供业务自己的重试循环使用；Setup 配置 Resty 内置的重试循环和响应检查。
package restyoops

import (
	"time"

	"github.com/go-resty/resty/v2"
)

// defaultDetective holds default rules shared across package functions.
// defaultDetective 保存包级函数共用的默认策略。
var defaultDetective = NewDetective()

// Setup configures a Resty client with default rules and supplied options.
// It returns the same client, so it can wrap a client while the client is being built
//
// Setup 把默认策略装到 resty 客户端上，附带给出的调整项
// 它返回同一个客户端，因此可以在构建客户端的过程中顺手包住它
func Setup(client *resty.Client, options ...Option) *resty.Client {
	if len(options) == 0 {
		return defaultDetective.Setup(client)
	}
	return NewDetective(options...).Setup(client)
}

// Detect classifies an attempt with default rules; nil denotes no fault.
// It accepts Resty's response and cause.
//
// Detect 用默认策略对一次已完成的尝试分类，没出问题时返回 nil
// 它接收 resty 调用的返回值，因此可以直接读取那次调用
func Detect(resp *resty.Response, cause error) *Oops {
	return defaultDetective.Detect(resp, cause)
}

// WaitTimeOf returns the wait duration and its presence flag.
// An unspecified wait delegates to backoff.
//
// WaitTimeOf 报告一个故障要求停多久，以及该等待时长是否被明确给出
// 未给出的等待时长归退避算法决定，这也是这两个答案要分开的原因
func WaitTimeOf(oops *Oops) (time.Duration, bool) {
	if oops == nil {
		return 0, false
	}
	return oops.WaitTime, oops.fixedWait
}
