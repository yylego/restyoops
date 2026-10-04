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

// hitCount counts incoming requests.
// hitCount 统计对端实际被访问了多少次
type hitCount struct {
	hits atomic.Int32
}

// serveStatus serves a fixed status code and counts requests.
// serveStatus 起一个用固定状态码应答的对端，并统计每次到达
func serveStatus(t *testing.T, statusCode int, headers map[string]string) (string, *hitCount) {
	t.Helper()
	count := &hitCount{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		count.hits.Add(1)
		for name, value := range headers {
			w.Header().Set(name, value)
		}
		w.WriteHeader(statusCode)
	}))
	t.Cleanup(server.Close)
	return server.URL, count
}

// serveCaptcha serves a captcha page with HTTP 200.
// serveCaptcha 起一个用 200 应答、但返回验证码页面而非内容的对端
func serveCaptcha(t *testing.T) (string, *hitCount) {
	t.Helper()
	count := &hitCount{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		count.hits.Add(1)
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(http.StatusOK)
		if _, err := w.Write([]byte(`<html><body>please solve the captcha</body></html>`)); err != nil {
			t.Errorf("response write: %v", err)
		}
	}))
	t.Cleanup(server.Close)
	return server.URL, count
}

// quick uses brief waits to test classification and request counts.
// quick 把退避压到很短，因为这些测试关心的是判断本身而不是等待时长
func quick(options ...restyoops.Option) []restyoops.Option {
	return append([]restyoops.Option{
		restyoops.WithAttempts(3),
		restyoops.WithWaitTime(time.Millisecond, time.Second),
	}, options...)
}

// TestSetup_RepeatsUpstreamFault checks repeats on HTTP 500.
//
// TestSetup_RepeatsUpstreamFault 会重发 500，而 resty 自己从来不会
// 不管的话 resty 对状态码一次都不重试，于是 SetRetryCount 看着配好了、实际毫无作用
func TestSetup_RepeatsUpstreamFault(t *testing.T) {
	endpoint, count := serveStatus(t, http.StatusInternalServerError, nil)

	client := restyoops.Setup(resty.New(), quick()...)
	_, cause := client.R().Get(endpoint)
	require.Error(t, cause)

	t.Logf("500 被访问 %d 次（1 次首发 + 3 次重试）", count.hits.Load())
	require.Equal(t, int32(4), count.hits.Load())
}

// TestSetup_StopsAtClientFault leaves a 404 alone, since the resource stays absent
// HTTP 404 does not warrant automatic repeats with default rules.
//
// TestSetup_StopsAtClientFault 不重发 404，因为资源不会因此出现
// resty 自带的 AddRetryAfterErrorCondition 会重发所有 4xx，毫无意义地反复敲对端
func TestSetup_StopsAtClientFault(t *testing.T) {
	endpoint, count := serveStatus(t, http.StatusNotFound, nil)

	client := restyoops.Setup(resty.New(), quick()...)
	_, cause := client.R().Get(endpoint)
	require.Error(t, cause)

	t.Logf("404 被访问 %d 次（只发一次）", count.hits.Load())
	require.Equal(t, int32(1), count.hits.Load())
}

// TestSetup_KeepsNetworkRepeat proves the built-in condition does not silence transport repeats
// Configured conditions must also handle transport faults.
//
// TestSetup_KeepsNetworkRepeat 证明内置条件不会把传输层重试关掉
// resty 每轮先假定传输故障要重试，然后让任何条件去覆盖这个假定
// 它自带的检查函数在此处回答“否”，不再触发网络重试
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
// Certificate trust needs correction, not repeats.
//
// TestSetup_StopsAtTrustFault 不重发不受信任的证书
// 这正是 resty 重试得最起劲的故障，因为开箱状态下所有传输故障都会重试
func TestSetup_StopsAtTrustFault(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {}))
	defer server.Close()

	var tries atomic.Int32
	client := restyoops.Setup(resty.New(), quick()...).
		AddRetryHook(func(_ *resty.Response, _ error) { tries.Add(1) })

	_, cause := client.R().Get(server.URL)
	require.Error(t, cause)

	t.Logf("证书不可信后重试了 %d 次（0 = 没有白费力气）", tries.Load())
	require.Equal(t, int32(0), tries.Load())
}

// TestSetup_GuardsUnsafeRepeat stops POST repeats despite HTTP 500.
//
// TestSetup_GuardsUnsafeRepeat 在 500 之后不重发 POST，因为它可能已经生效了
// 重发有下单两次的风险，而且没有任何状态码能说明它到底生效了没有
func TestSetup_GuardsUnsafeRepeat(t *testing.T) {
	endpoint, count := serveStatus(t, http.StatusInternalServerError, nil)

	client := restyoops.Setup(resty.New(), quick()...)
	_, cause := client.R().Post(endpoint)
	require.Error(t, cause)

	t.Logf("POST 遇到 500 被发送 %d 次（1 = 没有重复下单的风险）", count.hits.Load())
	require.Equal(t, int32(1), count.hits.Load())

	var oops *restyoops.Oops
	require.True(t, errors.As(cause, &oops))
	require.False(t, oops.Retryable)
}

// TestSetup_GuardsThrottledPost requires consent to repeat a POST, including on 429.
// TestSetup_GuardsThrottledPost 即使收到 429，也需要明确允许才能重发 POST。
func TestSetup_GuardsThrottledPost(t *testing.T) {
	endpoint, count := serveStatus(t, http.StatusTooManyRequests, nil)

	client := restyoops.Setup(resty.New(), quick()...)
	_, cause := client.R().Post(endpoint)
	require.Error(t, cause)

	require.Equal(t, int32(1), count.hits.Load())
}

func TestSetup_StopsAtWaitLimit(t *testing.T) {
	endpoint, count := serveStatus(t, http.StatusTooManyRequests, map[string]string{"Retry-After": "120"})
	client := restyoops.Setup(resty.New(), quick(restyoops.WithWaitTime(time.Millisecond, 5*time.Millisecond))...)
	resp, cause := client.R().Get(endpoint)
	require.Error(t, cause)
	require.Equal(t, int32(1), count.hits.Load(), "等待超出预算时停止，不能截短后提前请求")
	oops := restyoops.Detect(resp, cause)
	require.True(t, oops.Retryable, "分类建议与自动重试预算分别处理")
	require.Equal(t, 2*time.Minute, oops.WaitTime)
}

func TestSetup_GuardsMixedConditions(t *testing.T) {
	for _, placement := range []string{"before", "following", "request"} {
		t.Run(placement, func(t *testing.T) {
			endpoint, count := serveStatus(t, http.StatusTooManyRequests, nil)
			always := func(*resty.Response, error) bool { return true }
			client := resty.New()
			if placement == "before" {
				client.AddRetryCondition(always)
			}
			restyoops.Setup(client, quick()...)
			if placement == "following" {
				client.AddRetryCondition(always)
			}
			req := client.R()
			if placement == "request" {
				req.AddRetryCondition(always)
			}
			_, cause := req.Post(endpoint)
			require.Error(t, cause)
			require.Equal(t, int32(1), count.hits.Load(), "其他条件不能放行未经允许的 POST 重发")
		})
	}
}

// TestSetup_AllowsStatedRepeat checks an explicit method allowance.
// TestSetup_AllowsStatedRepeat 允许调用方自行承担重发非安全方法的责任
func TestSetup_AllowsStatedRepeat(t *testing.T) {
	endpoint, count := serveStatus(t, http.StatusInternalServerError, nil)

	client := restyoops.Setup(resty.New(), quick(restyoops.WithRepeatMethods("GET", "POST"))...)
	_, cause := client.R().Post(endpoint)
	require.Error(t, cause)

	t.Logf("明确允许后，POST 遇到 500 被发送 %d 次", count.hits.Load())
	require.Equal(t, int32(4), count.hits.Load())
}

// TestSetup_ReportsFaultAsOops checks status faults through errors.As.
//
// TestSetup_ReportsFaultAsOops 验证状态码故障可以通过 errors.As 获取。
// 没有这一步，500 会伴随 nil 的 error 返回，在所有惯常的检查里都读作成功
func TestSetup_ReportsFaultAsOops(t *testing.T) {
	endpoint, _ := serveStatus(t, http.StatusServiceUnavailable, nil)

	client := restyoops.Setup(resty.New(), quick()...)
	resp, cause := client.R().Get(endpoint)
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
	endpoint, _ := serveStatus(t, http.StatusInternalServerError, nil)

	client := restyoops.Setup(resty.New(), quick(restyoops.WithFaultAsOops(false))...)
	resp, cause := client.R().Get(endpoint)
	require.NoError(t, cause, "关掉之后 500 仍然伴随 nil 的 error")

	oops := restyoops.Detect(resp, cause)
	require.NotNil(t, oops, "分类仍然拿得到")
	t.Logf("保持 resty 形态时，仍可主动分类：%v", oops)
}

// TestSetup_DefaultWaitLimit checks the default wait limit.
//
// TestSetup_DefaultWaitLimit 检查默认等待上限。
func TestSetup_DefaultWaitLimit(t *testing.T) {
	client := restyoops.Setup(resty.New())
	t.Logf("等待上限 %v（resty 默认只有 2s）", client.RetryMaxWaitTime)
	require.Greater(t, client.RetryMaxWaitTime, 2*time.Second)
}

// TestSetup_WaitsAsAsked respects the endpoint's requested wait.
// TestSetup_WaitsAsAsked 按对端说明的时长停够了再重发
func TestSetup_WaitsAsAsked(t *testing.T) {
	endpoint, count := serveStatus(t, http.StatusTooManyRequests, map[string]string{"Retry-After": "1"})

	client := restyoops.Setup(resty.New(),
		restyoops.WithAttempts(1),
		restyoops.WithWaitTime(time.Millisecond, time.Minute),
	)

	since := time.Now()
	_, cause := client.R().Get(endpoint)
	elapsed := time.Since(since)
	require.Error(t, cause)

	t.Logf("对端要求等 1s，实际间隔 %v，共访问 %d 次", elapsed.Round(time.Millisecond), count.hits.Load())
	require.Equal(t, int32(2), count.hits.Load())
	require.GreaterOrEqual(t, elapsed, time.Second)
}
