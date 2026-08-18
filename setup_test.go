package restyoops_test

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/go-resty/resty/v2"
	"github.com/stretchr/testify/require"
	"github.com/yylego/restyoops"
)

// hitCount counts how often the peer was actually reached
// hitCount 统计对端实际被访问了多少次
type hitCount struct {
	hits atomic.Int32
}

// serveStatus stands up a peer answering with one status code, counting every arrival
// serveStatus 起一个用固定状态码应答的对端，并统计每次到达
func serveStatus(t *testing.T, statusCode int, headers map[string]string) (string, *hitCount) {
	t.Helper()
	count := &hitCount{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count.hits.Add(1)
		for name, value := range headers {
			w.Header().Set(name, value)
		}
		w.WriteHeader(statusCode)
	}))
	t.Cleanup(server.Close)
	return server.URL, count
}

// serveCaptcha stands up a peer that answers 200 while serving a captcha page instead of content
// serveCaptcha 起一个用 200 应答、但返回验证码页面而非内容的对端
func serveCaptcha(t *testing.T) (string, *hitCount) {
	t.Helper()
	count := &hitCount{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count.hits.Add(1)
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`<html><body>please solve the captcha</body></html>`))
	}))
	t.Cleanup(server.Close)
	return server.URL, count
}

// quick keeps the backoff short, since these tests are about the decisions rather than the waits
// quick 把退避压到很短，因为这些测试关心的是判断本身而不是等待时长
func quick(options ...restyoops.Option) []restyoops.Option {
	return append([]restyoops.Option{
		restyoops.WithAttempts(3),
		restyoops.WithWaitTime(time.Millisecond, time.Second),
	}, options...)
}

// TestSetup_RepeatsUpstreamFault sends a 500 again, which resty on its own never does
// Left alone resty repeats nothing on a status code, so SetRetryCount looks set up yet does nothing
//
// TestSetup_RepeatsUpstreamFault 会重发 500，而 resty 自己从来不会
// 不管的话 resty 对状态码一次都不重试，于是 SetRetryCount 看着配好了、实际毫无作用
func TestSetup_RepeatsUpstreamFault(t *testing.T) {
	url, count := serveStatus(t, http.StatusInternalServerError, nil)

	client := restyoops.Setup(resty.New(), quick()...)
	_, cause := client.R().Get(url)
	require.Error(t, cause)

	t.Logf("500 被访问 %d 次（1 次首发 + 3 次重试）", count.hits.Load())
	require.Equal(t, int32(4), count.hits.Load())
}

// TestSetup_StopsAtClientFault leaves a 404 alone, since the resource stays absent
// resty's own AddRetryAfterErrorCondition repeats every 4xx, hammering a peer to no purpose
//
// TestSetup_StopsAtClientFault 不重发 404，因为资源不会因此出现
// resty 自带的 AddRetryAfterErrorCondition 会重发所有 4xx，毫无意义地反复敲对端
func TestSetup_StopsAtClientFault(t *testing.T) {
	url, count := serveStatus(t, http.StatusNotFound, nil)

	client := restyoops.Setup(resty.New(), quick()...)
	_, cause := client.R().Get(url)
	require.Error(t, cause)

	t.Logf("404 被访问 %d 次（只发一次）", count.hits.Load())
	require.Equal(t, int32(1), count.hits.Load())
}

// TestSetup_KeepsNetworkRepeat proves the built-in condition does not silence transport repeats
// resty starts each round assuming a transport fault repeats, then lets any condition overwrite
// that. Its own helper answers "no" there, quietly turning network retries off altogether
//
// TestSetup_KeepsNetworkRepeat 证明内置条件不会把传输层重试关掉
// resty 每轮先假定传输故障要重试，然后让任何条件去覆盖这个假定
// 它自带的 helper 在那里回答"否"，于是悄悄把网络重试整个关掉了
func TestSetup_KeepsNetworkRepeat(t *testing.T) {
	var tries atomic.Int32
	client := restyoops.Setup(resty.New(), quick()...).
		AddRetryHook(func(_ *resty.Response, _ error) { tries.Add(1) })

	_, cause := client.R().Get("http://127.0.0.1:1")
	require.Error(t, cause)

	t.Logf("连接被拒后重试了 %d 次", tries.Load())
	require.Greater(t, tries.Load(), int32(0))
}

// TestSetup_StopsAtTrustFault leaves an untrusted certificate alone
// This is the fault resty repeats hardest, since every transport fault repeats out of the box
//
// TestSetup_StopsAtTrustFault 不重发不受信任的证书
// 这正是 resty 重试得最起劲的故障，因为开箱状态下所有传输故障都会重试
func TestSetup_StopsAtTrustFault(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer server.Close()

	var tries atomic.Int32
	client := restyoops.Setup(resty.New(), quick()...).
		AddRetryHook(func(_ *resty.Response, _ error) { tries.Add(1) })

	_, cause := client.R().Get(server.URL)
	require.Error(t, cause)

	t.Logf("证书不可信后重试了 %d 次（0 = 没有白费力气）", tries.Load())
	require.Equal(t, int32(0), tries.Load())
}

// TestSetup_GuardsUnsafeRepeat leaves a POST alone after a 500, since it may already have landed
// Repeating it risks placing the same order twice, and no status code can tell whether it landed
//
// TestSetup_GuardsUnsafeRepeat 在 500 之后不重发 POST，因为它可能已经生效了
// 重发有下单两次的风险，而且没有任何状态码能说明它到底生效了没有
func TestSetup_GuardsUnsafeRepeat(t *testing.T) {
	url, count := serveStatus(t, http.StatusInternalServerError, nil)

	client := restyoops.Setup(resty.New(), quick()...)
	_, cause := client.R().Post(url)
	require.Error(t, cause)

	t.Logf("POST 遇到 500 被发送 %d 次（1 = 没有重复下单的风险）", count.hits.Load())
	require.Equal(t, int32(1), count.hits.Load())

	var oops *restyoops.Oops
	require.True(t, errors.As(cause, &oops))
	require.False(t, oops.Retryable)
}

// TestSetup_RepeatsThrottledPost sends a throttled POST again, since it was turned away unhandled
// TestSetup_RepeatsThrottledPost 会重发被限流的 POST，因为它是被挡回来的、根本没被处理
func TestSetup_RepeatsThrottledPost(t *testing.T) {
	url, count := serveStatus(t, http.StatusTooManyRequests, nil)

	client := restyoops.Setup(resty.New(), quick()...)
	_, cause := client.R().Post(url)
	require.Error(t, cause)

	t.Logf("POST 遇到 429 被发送 %d 次（限流是挡回来的，重发安全）", count.hits.Load())
	require.Equal(t, int32(4), count.hits.Load())
}

// TestSetup_AllowsStatedRepeat lets a caller take responsibility for repeating an unsafe method
// TestSetup_AllowsStatedRepeat 允许调用方自行承担重发非安全方法的责任
func TestSetup_AllowsStatedRepeat(t *testing.T) {
	url, count := serveStatus(t, http.StatusInternalServerError, nil)

	client := restyoops.Setup(resty.New(), quick(restyoops.WithRepeatMethods("GET", "POST"))...)
	_, cause := client.R().Post(url)
	require.Error(t, cause)

	t.Logf("明确允许后，POST 遇到 500 被发送 %d 次", count.hits.Load())
	require.Equal(t, int32(4), count.hits.Load())
}

// TestSetup_ReportsFaultAsError puts a status-code fault where the language expects a fault
// Without this a 500 arrives with a nil error, reading as success to every habitual check
//
// TestSetup_ReportsFaultAsError 把状态码故障放到这门语言期待故障出现的位置
// 没有这一步，500 会伴随 nil 的 error 返回，在所有惯常的检查里都读作成功
func TestSetup_ReportsFaultAsError(t *testing.T) {
	url, _ := serveStatus(t, http.StatusServiceUnavailable, nil)

	client := restyoops.Setup(resty.New(), quick()...)
	resp, cause := client.R().Get(url)
	require.Error(t, cause)

	var oops *restyoops.Oops
	require.True(t, errors.As(cause, &oops), "故障应当能被 errors.As 取出")
	t.Logf("拿到的 error：%v", cause)
	require.Equal(t, restyoops.KindUpstream, oops.Kind)
	require.Equal(t, http.StatusServiceUnavailable, oops.StatusCode)
	require.Equal(t, http.MethodGet, oops.Method)
	require.Equal(t, 4, oops.Attempt, "尝试次数应当记在故障上")
	require.NotNil(t, resp, "响应仍然要交回调用方")
}

// TestSetup_KeepsRestyShape leaves resty's own shape untouched when asked to
// TestSetup_KeepsRestyShape 在被要求时保持 resty 原本的形态
func TestSetup_KeepsRestyShape(t *testing.T) {
	url, _ := serveStatus(t, http.StatusInternalServerError, nil)

	client := restyoops.Setup(resty.New(), quick(restyoops.WithErrorOnFault(false))...)
	resp, cause := client.R().Get(url)
	require.NoError(t, cause, "关掉之后 500 仍然伴随 nil 的 error")

	oops := restyoops.Detect(resp, cause)
	require.NotNil(t, oops, "分类仍然拿得到")
	t.Logf("保持 resty 形态时，仍可主动分类：%v", oops)
}

// TestSetup_LeavesRoomForStatedWait keeps the ceiling clear of the wait a peer asks for
// resty caps the wait at two seconds out of the box, which would cut a two minute request to two
// seconds and make reading the peer's request pointless
//
// TestSetup_LeavesRoomForStatedWait 让上限不会挡住对端要求的等待时长
// resty 开箱时把等待截断在两秒，那会把"请等两分钟"砍成两秒，让读取对端要求这件事失去意义
func TestSetup_LeavesRoomForStatedWait(t *testing.T) {
	client := restyoops.Setup(resty.New())
	t.Logf("等待上限 %v（resty 默认只有 2s）", client.RetryMaxWaitTime)
	require.Greater(t, client.RetryMaxWaitTime, 2*time.Second)
}

// TestSetup_WaitsAsAsked holds off as long as the peer states before going again
// TestSetup_WaitsAsAsked 按对端说明的时长停够了再重发
func TestSetup_WaitsAsAsked(t *testing.T) {
	url, count := serveStatus(t, http.StatusTooManyRequests, map[string]string{"Retry-After": "1"})

	client := restyoops.Setup(resty.New(),
		restyoops.WithAttempts(1),
		restyoops.WithWaitTime(time.Millisecond, time.Minute),
	)

	since := time.Now()
	_, cause := client.R().Get(url)
	elapsed := time.Since(since)
	require.Error(t, cause)

	t.Logf("对端要求等 1s，实际间隔 %v，共访问 %d 次", elapsed.Round(time.Millisecond), count.hits.Load())
	require.Equal(t, int32(2), count.hits.Load())
	require.GreaterOrEqual(t, elapsed, time.Second)
}
