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

// detectCause classifies transport faults, testing specific causes before broad wrappers.
//
// detectCause 对传输层故障分类，即那些导致完整响应没能到达的故障
// 分支从精确排到宽泛，因为宽泛的形态会把精确的包在里面
// 顺序写反会让所有精确判断被兜底分支吃掉
func detectCause(cause error) *Oops {
	// Cancellation prevents progress with the same context.
	// 请求 context 已取消，停止继续尝试。
	if errors.Is(cause, context.Canceled) {
		return NewOops(KindCanceled, cause, Abort())
	}

	// An attempt timeout can be transient if the request context remains active.
	// 单次超时可能是暂时故障；请求 context 已结束的情况由 Detect 先行处理。
	if errors.Is(cause, context.DeadlineExceeded) || errors.Is(cause, os.ErrDeadlineExceeded) {
		return NewOops(KindNetwork, cause, Again())
	}

	// Trust could not be established, and no amount of retrying makes a cert trusted
	// 信任无法建立，重试多少次证书也不会变得可信
	if isTrustFault(cause) {
		return NewOops(KindTLS, cause, Abort())
	}

	// Missing DNS names need correction; transient lookup faults can resolve.
	// 域名解析不了。不存在的域名会一直不存在，其它解析故障则可能好转
	var dnsFault *net.DNSError
	if errors.As(cause, &dnsFault) {
		if dnsFault.IsNotFound {
			return NewOops(KindRequest, cause, Abort())
		}
		return NewOops(KindNetwork, cause, Again())
	}

	// Connection faults can be transient.
	// 连接本身失败：被拒、被重置、不可达。对端可能会回来
	var opFault *net.OpError
	if errors.As(cause, &opFault) {
		return NewOops(KindNetwork, cause, Again())
	}

	// Inspect url.Error once specific causes have been checked.
	//
	// net/http 会把所有传输故障包进 url.Error，因此它被有意排在最后
	// 它携带的部分故障永远不可能成功，那些绝不能再发一次
	var linkFault *url.Error
	if errors.As(cause, &linkFault) {
		if isRequestFault(linkFault) {
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

	// Unrecognized causes need investigation before repeats.
	// 形态未知，与其为一个认不出的东西反复敲对端，不如放弃
	return NewOops(KindUnknown, cause, Abort())
}

// isTrustFault detects certificate and TLS record faults.
// isTrustFault 判断安全通道是否建立失败
func isTrustFault(cause error) bool {
	var certFault *tls.CertificateVerificationError
	if errors.As(cause, &certFault) {
		return true
	}
	var recordFault tls.RecordHeaderError
	if errors.As(cause, &recordFault) {
		return true
	}
	var certTrustFault x509.UnknownAuthorityError
	if errors.As(cause, &certTrustFault) {
		return true
	}
	var hostnameFault x509.HostnameError
	if errors.As(cause, &hostnameFault) {
		return true
	}
	var invalidFault x509.CertificateInvalidError
	return errors.As(cause, &invalidFault)
}

// isRequestFault detects request faults expressed through net/http's fixed messages.
//
// isRequestFault 判断 url.Error 携带的是不是请求本身永远迈不过去的问题
// 标准库用纯文本表达这些故障，因此只能通过匹配文本识别
func isRequestFault(linkFault *url.Error) bool {
	if linkFault.Err == nil {
		return false
	}
	if linkFault.Timeout() {
		return false
	}
	text := linkFault.Err.Error()
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
	// Rate limits can include a minimum wait.
	// 对端在限流，而且通常会说明该停多久
	case statusCode == http.StatusTooManyRequests:
		return NewOops(KindThrottle, nil, Again())

	// These status codes can permit a repeat with suitable request semantics.
	// 这些状态码允许结合请求语义考虑重试。
	case statusCode == http.StatusRequestTimeout, statusCode == http.StatusTooEarly:
		return NewOops(KindClient, nil, Again())

	// Unsupported operations need request changes.
	// 不支持的操作需要调整请求。
	case statusCode == http.StatusNotImplemented, statusCode == http.StatusHTTPVersionNotSupported:
		return NewOops(KindUpstream, nil, Abort())

	// Upstream faults can be transient.
	// 服务端出了问题，这正是重试机制存在的理由
	case statusCode >= 500:
		return NewOops(KindUpstream, nil, Again())

	// Remaining 4xx codes default to no repeats.
	// 本方发出的东西被对端拒绝，而且会一直被拒绝
	case statusCode >= 400:
		return NewOops(KindClient, nil, Abort())
	}
	return NewOops(KindUnknown, nil, Abort())
}
