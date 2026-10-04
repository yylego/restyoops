package restyoops

// Kind names the nature of a request fault, so callers can branch on it
// Distinct kinds support distinct responses to faults.
//
// Kind 表示请求故障的性质，让调用方能据此分支
// 是否重试还取决于具体状态码、配置规则和请求方法。
type Kind string

// Kinds produced from the built-in detection
// 内置检测会产出的分类
const (
	// KindUnknown denotes an unrecognized fault that needs investigation.
	//
	// KindUnknown 表示故障不符合任何已知形态
	// 处理：记录并排查错误原因。
	KindUnknown Kind = "UNKNOWN"

	// KindNetwork denotes a connection fault / timeout; repeats depend on request semantics.
	//
	// KindNetwork 表示请求没能拿到完整响应：连接被拒、连接重置、超时
	// 处理：结合请求是否允许重复执行及总时限，决定是否重试。
	KindNetwork Kind = "NETWORK"

	// KindCanceled denotes an ended request context; stop attempts with that context.
	//
	// KindCanceled 表示请求 context 已取消或已到期
	// 处理：停止使用该 context 重试。
	KindCanceled Kind = "CANCELED"

	// KindRequest denotes a request that needs correction: DNS name, scheme, redirects.
	//
	// KindRequest 表示域名不存在、协议不支持或重定向次数超限等请求问题。
	// 处理：停止重试，检查请求配置。
	KindRequest Kind = "REQUEST"

	// KindTLS denotes certificate / TLS record faults that need investigation.
	//
	// KindTLS 表示证书校验或 TLS 记录格式错误
	// 处理：停止重试，检查证书与 TLS 配置。
	KindTLS Kind = "TLS"

	// KindThrottle denotes HTTP 429; respect Retry-After and request semantics.
	//
	// KindThrottle 表示对端在限流：HTTP 429
	// 处理：确认请求允许重复执行，并遵守 Retry-After 要求的等待。
	KindThrottle Kind = "THROTTLE"

	// KindUpstream denotes HTTP 5xx; consult the status and request semantics before repeats.
	//
	// KindUpstream 表示服务端出问题：HTTP 5xx
	// 处理：结合状态码与请求语义决定；默认不重试 501、505。
	KindUpstream Kind = "UPSTREAM"

	// KindClient denotes HTTP 4xx except 429; consult the status and endpoint contract.
	//
	// KindClient 表示本方发出的东西被对端拒绝：除 429 外的 HTTP 4xx
	// 处理：结合状态码与接口约定决定；默认仅 408、425 允许重试。
	KindClient Kind = "CLIENT"
)

// Kinds meant to come from a custom check: the built-in detection cannot see them
// Endpoint-specific knowledge is needed to interpret response content.
//
// 以下分类由自定义检查产出：内置检测看不出来
// 识别验证码页面或业务码需要关于对端的专门知识
const (
	// KindBlock denotes a captcha, WAF response, login redirect: stop unattended repeats.
	//
	// KindBlock 表示对端返回了验证码、WAF 拦截页或登录跳转
	// 处理：停止自动重试，检查访问要求或登录状态
	KindBlock Kind = "BLOCK"

	// KindBusiness means HTTP said fine but the payload carries a business fault code
	// A custom check interprets the code and decides on repeats.
	//
	// KindBusiness 表示 HTTP 说成功、但报文里带着业务失败码
	// 补救：取决于业务码，因此由检查函数自行决定重试是否有用
	KindBusiness Kind = "BUSINESS"

	// KindParse means the payload could not be read as expected
	// Inspect the response format before repeating the request.
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
