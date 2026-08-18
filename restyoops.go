// Package restyoops decides whether a failed resty request is worth sending again, how long to
// hold off before doing that, and what kind of fault it was
//
// resty already owns a retry loop, an exponential backoff and the attempt counting. What it
// leaves to its users is the judgement: out of the box it repeats every transport fault, even
// an untrusted certificate that can never become trusted, while a 500 gets no repeat at all
// unless a condition is added by hand. This package supplies that judgement, installs it in one
// call, and reports each fault as an error carrying its classification
//
// restyoops 判断一次失败的 resty 请求是否值得再发一次、发之前该停多久、以及那是什么性质的故障
//
// 重试循环、指数退避和尝试计数 resty 本来就有。它留给使用方的是判断本身：开箱状态下它会重试
// 每一个传输故障，包括永远不可能变得可信的证书错误；而 500 则完全不会重试，除非手动添加条件
// 本包补上这部分判断，一次调用即可装好，并把每个故障作为携带分类信息的 error 报出来
package restyoops

import (
	"time"

	"github.com/go-resty/resty/v2"
)

// scout carries the default policy, shared by the package level helpers below
// scout 持有默认策略，供下面的包级辅助函数共用
var scout = NewDetective()

// Setup installs the default policy onto a resty client, plus any adjustments given
// It returns the same client, so it can wrap a client while the client is being built
//
// Setup 把默认策略装到 resty 客户端上，附带给出的调整项
// 它返回同一个客户端，因此可以在构建客户端的过程中顺手包住它
func Setup(client *resty.Client, options ...Option) *resty.Client {
	if len(options) == 0 {
		return scout.Setup(client)
	}
	return NewDetective(options...).Setup(client)
}

// Detect classifies one finished attempt with the default policy, nil when nothing went wrong
// It takes what a resty call returns, so it can read that call directly
//
// Detect 用默认策略对一次已完成的尝试分类，没出问题时返回 nil
// 它接收 resty 调用的返回值，因此可以直接读取那次调用
func Detect(resp *resty.Response, cause error) *Oops {
	return scout.Detect(resp, cause)
}

// WaitTimeOf reports how long a fault asks to hold off, and whether that wait was stated at all
// An unstated wait belongs to the backoff, which is why the two answers stay apart
//
// WaitTimeOf 报告一个故障要求停多久，以及该等待时长是否被明确给出
// 未给出的等待时长归退避算法决定，这也是这两个答案要分开的原因
func WaitTimeOf(oops *Oops) (time.Duration, bool) {
	if oops == nil {
		return 0, false
	}
	return oops.WaitTime, oops.fixedWait
}
