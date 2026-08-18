package restyoops

// Kind names the nature of a request fault, so callers can branch on it
// Each kind implies a different remedy, that is the reason they are kept apart
//
// Kind 表示请求故障的性质，让调用方能据此分支
// 每个 kind 对应不同的补救方式，这也是把它们分开的理由
type Kind string

// Kinds produced from the built-in detection
// 内置检测会产出的分类
const (
	// KindUnknown means the fault does not match any known shape
	// Remedy: log it and look into it, the detection needs a fresh branch
	//
	// KindUnknown 表示故障不符合任何已知形态
	// 补救：记录并排查，检测逻辑需要新增分支
	KindUnknown Kind = "UNKNOWN"

	// KindNetwork means the request never got a complete response: refused, reset, timeout
	// Remedy: retrying makes sense, the peer might be back soon
	//
	// KindNetwork 表示请求没能拿到完整响应：连接被拒、连接重置、超时
	// 补救：重试有意义，对端可能很快恢复
	KindNetwork Kind = "NETWORK"

	// KindCanceled means the caller stopped it: context canceled
	// Remedy: stop. The context is dead, a retry fails at once
	//
	// KindCanceled 表示调用方主动停止：context 被取消
	// 补救：停手。context 已经死了，重试会立刻再次失败
	KindCanceled Kind = "CANCELED"

	// KindRequest means the request itself cannot succeed: no such host, bad scheme, too many redirects
	// Remedy: stop and fix the request. Retrying repeats the same mistake
	//
	// KindRequest 表示请求本身不可能成功：域名不存在、协议不支持、重定向次数超限
	// 补救：停手并修请求。重试只是把同一个错误再犯一遍
	KindRequest Kind = "REQUEST"

	// KindTLS means the secure channel could not be established: untrusted cert, handshake denied
	// Remedy: stop and fix trust settings. A cert does not become trusted through retries
	//
	// KindTLS 表示安全通道建立失败：证书不受信任、握手被拒
	// 补救：停手并修信任配置。证书不会因为重试就变得可信
	KindTLS Kind = "TLS"

	// KindThrottle means the peer is rate limiting: HTTP 429
	// Remedy: retry, and wait as long as the peer asks through Retry-After
	//
	// KindThrottle 表示对端在限流：HTTP 429
	// 补救：重试，并按对端 Retry-After 要求的时长等待
	KindThrottle Kind = "THROTTLE"

	// KindUpstream means the serving side broke: HTTP 5xx
	// Remedy: retry, unless the request is unsafe to send twice
	//
	// KindUpstream 表示服务端出问题：HTTP 5xx
	// 补救：重试，除非该请求重复发送不安全
	KindUpstream Kind = "UPSTREAM"

	// KindClient means this side sent something the peer rejects: HTTP 4xx besides 429
	// Remedy: stop and fix the request. The same request keeps being rejected
	//
	// KindClient 表示本方发出的东西被对端拒绝：除 429 外的 HTTP 4xx
	// 补救：停手并修请求。同样的请求会一直被拒
	KindClient Kind = "CLIENT"
)

// Kinds meant to come from a custom check: the built-in detection cannot see them
// Reading a captcha page or a business code needs knowledge about the peer
//
// 以下分类由自定义检查产出：内置检测看不出来
// 识别验证码页面或业务码需要关于对端的专门知识
const (
	// KindBlock means the peer served a captcha, a WAF page, or a login redirect
	// Remedy: swap proxy, swap account, solve the captcha. Plain retries feed the block
	//
	// KindBlock 表示对端返回了验证码、WAF 拦截页或登录跳转
	// 补救：换代理、换账号、过验证码。单纯重试只会喂大封禁
	KindBlock Kind = "BLOCK"

	// KindBusiness means HTTP said fine but the payload carries a business fault code
	// Remedy: depends on the code, so the check decides whether a retry helps
	//
	// KindBusiness 表示 HTTP 说成功、但报文里带着业务失败码
	// 补救：取决于业务码，因此由检查函数自行决定重试是否有用
	KindBusiness Kind = "BUSINESS"

	// KindParse means the payload could not be read as expected
	// Remedy: usually stop, since the same bytes parse the same way
	//
	// KindParse 表示报文无法按预期解析
	// 补救：通常停手，因为同样的字节解析结果不会变
	KindParse Kind = "PARSE"
)

// String makes Kind print as its own name
// String 让 Kind 按自身名称打印
func (k Kind) String() string {
	return string(k)
}
