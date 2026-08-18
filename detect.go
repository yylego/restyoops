package restyoops

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
)

// detectCause classifies a transport fault, one that kept a complete response from arriving
// The branches run from precise to broad, since the broad shapes wrap the precise ones
// Getting the order backwards hides every precise judgement behind the catch-all
//
// detectCause 对传输层故障分类，即那些导致完整响应没能到达的故障
// 分支从精确排到宽泛，因为宽泛的形态会把精确的包在里面
// 顺序写反会让所有精确判断被兜底分支吃掉
func detectCause(cause error) *Oops {
	// The caller called it off, so the context is dead and another attempt dies at once
	// 调用方喊停，context 已经死了，再试一次会立刻再死
	if errors.Is(cause, context.Canceled) {
		return NewOops(KindCanceled, cause, Abort())
	}

	// Ran out of time, which often clears up, so another attempt is worth it
	// 时间用完了，这种情况常常会好转，值得再试
	if errors.Is(cause, context.DeadlineExceeded) || errors.Is(cause, os.ErrDeadlineExceeded) {
		return NewOops(KindNetwork, cause, Again())
	}

	// Trust could not be established, and no amount of retrying makes a cert trusted
	// 信任无法建立，重试多少次证书也不会变得可信
	if isTrustFault(cause) {
		return NewOops(KindTLS, cause, Abort())
	}

	// The name does not resolve. Absent names stay absent, other lookup faults may clear up
	// 域名解析不了。不存在的域名会一直不存在，其它解析故障则可能好转
	var dnsFault *net.DNSError
	if errors.As(cause, &dnsFault) {
		if dnsFault.IsNotFound {
			return NewOops(KindRequest, cause, Abort())
		}
		return NewOops(KindNetwork, cause, Again())
	}

	// The connection itself failed: refused, reset, unreachable. The peer may come back
	// 连接本身失败：被拒、被重置、不可达。对端可能会回来
	var opFault *net.OpError
	if errors.As(cause, &opFault) {
		return NewOops(KindNetwork, cause, Again())
	}

	// net/http wraps every transport fault in url.Error, so it arrives last on purpose
	// Some of what it carries can never succeed, and those must not be sent again
	//
	// net/http 会把所有传输故障包进 url.Error，因此它被有意排在最后
	// 它携带的部分故障永远不可能成功，那些绝不能再发一次
	var urlFault *url.Error
	if errors.As(cause, &urlFault) {
		if isRequestFault(urlFault) {
			return NewOops(KindRequest, cause, Abort())
		}
		return NewOops(KindNetwork, cause, Again())
	}

	// Anything else carrying network semantics counts as a network fault
	// 其余带网络语义的都算网络故障
	var netFault net.Error
	if errors.As(cause, &netFault) {
		return NewOops(KindNetwork, cause, Again())
	}

	// Shape unknown, so giving up beats hammering a peer over something unrecognized
	// 形态未知，与其为一个认不出的东西反复敲对端，不如放弃
	return NewOops(KindUnknown, cause, Abort())
}

// isTrustFault reports whether the secure channel could not be established
// isTrustFault 判断安全通道是否建立失败
func isTrustFault(cause error) bool {
	var verifyFault *tls.CertificateVerificationError
	if errors.As(cause, &verifyFault) {
		return true
	}
	var recordFault tls.RecordHeaderError
	if errors.As(cause, &recordFault) {
		return true
	}
	var authorityFault x509.UnknownAuthorityError
	if errors.As(cause, &authorityFault) {
		return true
	}
	var hostnameFault x509.HostnameError
	if errors.As(cause, &hostnameFault) {
		return true
	}
	var invalidFault x509.CertificateInvalidError
	return errors.As(cause, &invalidFault)
}

// isRequestFault reports whether a url.Error carries something the request can never get past
// The standard library states these through plain text, so matching the text is the way in
//
// isRequestFault 判断 url.Error 携带的是不是请求本身永远迈不过去的问题
// 标准库用纯文本表达这些故障，因此只能通过匹配文本识别
func isRequestFault(urlFault *url.Error) bool {
	if urlFault.Err == nil {
		return false
	}
	if urlFault.Timeout() {
		return false
	}
	text := urlFault.Err.Error()
	for _, mark := range []string{
		"unsupported protocol scheme",
		"no Host in request URL",
		"stopped after ",
		"http: nil Request.URL",
		"malformed HTTP",
	} {
		if strings.Contains(text, mark) {
			return true
		}
	}
	return false
}

// detectStatus classifies an HTTP status code that reports a fault
// detectStatus 对表示故障的 HTTP 状态码分类
func detectStatus(statusCode int) *Oops {
	oops := detectStatusKind(statusCode)
	oops.StatusCode = statusCode
	return oops
}

// detectStatusKind picks kind and rule from the status code
// detectStatusKind 根据状态码选出分类和处置规则
func detectStatusKind(statusCode int) *Oops {
	switch {
	// The peer is rate limiting, and it commonly states how long to hold off
	// 对端在限流，而且通常会说明该停多久
	case statusCode == http.StatusTooManyRequests:
		return NewOops(KindThrottle, nil, Again())

	// The peer gave up waiting, or asked to come back later, so coming back works
	// 对端等烦了，或者请求稍后再来，那就再来一次
	case statusCode == http.StatusRequestTimeout, statusCode == http.StatusTooEarly:
		return NewOops(KindClient, nil, Again())

	// The peer will never support it, so the same request stays unsupported
	// 对端永远不会支持，同样的请求会一直不被支持
	case statusCode == http.StatusNotImplemented, statusCode == http.StatusHTTPVersionNotSupported:
		return NewOops(KindUpstream, nil, Abort())

	// The serving side broke, which is the case retries were invented for
	// 服务端出了问题，这正是重试机制存在的理由
	case statusCode >= 500:
		return NewOops(KindUpstream, nil, Again())

	// This side sent something the peer rejects, and it keeps rejecting it
	// 本方发出的东西被对端拒绝，而且会一直被拒绝
	case statusCode >= 400:
		return NewOops(KindClient, nil, Abort())
	}
	return NewOops(KindUnknown, nil, Abort())
}
