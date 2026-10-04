package restyoops_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-resty/resty/v2"
	"github.com/stretchr/testify/require"
	"github.com/yylego/restyoops"
)

// causeOf obtains a transport fault from a request.
// causeOf 发出一个预期在完整响应到达前就失败的请求
func causeOf(t *testing.T, client *resty.Client, endpoint string) error {
	t.Helper()
	_, cause := client.R().Get(endpoint)
	require.Error(t, cause)
	t.Logf("底层错误：%v", cause)
	return cause
}

// TestDetect_TrustFault keeps an untrusted certificate from being sent again
// Repetition cannot establish trust in a certificate.
//
// TestDetect_TrustFault 确保不受信任的证书不会被重发
// 证书不会因为重复就变得可信，重复只会推迟失败的到来
func TestDetect_TrustFault(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {}))
	defer server.Close()

	oops := restyoops.Detect(nil, causeOf(t, resty.New(), server.URL))
	require.NotNil(t, oops)
	t.Logf("分类=%s 可重试=%v", oops.Kind, oops.Retryable)
	require.Equal(t, restyoops.KindTLS, oops.Kind)
	require.False(t, oops.Retryable)
}

// TestDetect_SchemeFault keeps an unsupported scheme from being sent again
// TestDetect_SchemeFault 确保不支持的协议不会被重发
func TestDetect_SchemeFault(t *testing.T) {
	oops := restyoops.Detect(nil, causeOf(t, resty.New(), "xyz://example.com"))
	require.NotNil(t, oops)
	t.Logf("分类=%s 可重试=%v", oops.Kind, oops.Retryable)
	require.Equal(t, restyoops.KindRequest, oops.Kind)
	require.False(t, oops.Retryable)
}

// TestDetect_MissingHost keeps a name that does not exist from being sent again
// TestDetect_MissingHost 确保不存在的域名不会被重发
func TestDetect_MissingHost(t *testing.T) {
	oops := restyoops.Detect(nil, causeOf(t, resty.New(), "http://host-that-does-not-exist-8f2a.invalid"))
	require.NotNil(t, oops)
	t.Logf("分类=%s 可重试=%v", oops.Kind, oops.Retryable)
	require.Equal(t, restyoops.KindRequest, oops.Kind)
	require.False(t, oops.Retryable)
}

// TestDetect_RefusedConnection permits repeats on refused connections.
// TestDetect_RefusedConnection 会重发被拒绝的连接，因为对端可能会恢复
func TestDetect_RefusedConnection(t *testing.T) {
	oops := restyoops.Detect(nil, causeOf(t, resty.New(), "http://127.0.0.1:1"))
	require.NotNil(t, oops)
	t.Logf("分类=%s 可重试=%v", oops.Kind, oops.Retryable)
	require.Equal(t, restyoops.KindNetwork, oops.Kind)
	require.True(t, oops.Retryable)
}

// TestDetect_Canceled stops on context cancellation.
// TestDetect_Canceled 在请求 context 取消后停止重试。
func TestDetect_Canceled(t *testing.T) {
	oops := restyoops.Detect(nil, context.Canceled)
	require.NotNil(t, oops)
	t.Logf("分类=%s 可重试=%v", oops.Kind, oops.Retryable)
	require.Equal(t, restyoops.KindCanceled, oops.Kind)
	require.False(t, oops.Retryable)
}

func TestDetect_ContextStopsRules(t *testing.T) {
	ctx, stop := context.WithCancel(context.Background())
	stop()
	resp := &resty.Response{Request: resty.New().R().SetContext(ctx)}
	detective := restyoops.NewDetective(
		restyoops.WithKind(restyoops.KindCanceled, restyoops.Again()),
		restyoops.WithCheck(func(*resty.Response, error) *restyoops.Oops {
			return restyoops.NewOops(restyoops.KindBusiness, nil, restyoops.Again())
		}),
	)
	for _, item := range []struct {
		resp  *resty.Response
		cause error
	}{
		{nil, context.Canceled},
		{resp, restyoops.NewOops(restyoops.KindNetwork, context.DeadlineExceeded, restyoops.Again())},
	} {
		oops := detective.Detect(item.resp, item.cause)
		require.Equal(t, restyoops.KindCanceled, oops.Kind)
		require.False(t, oops.Retryable)
		require.ErrorIs(t, oops, context.Canceled)
	}
	ctx, stop = context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer stop()
	resp.Request.SetContext(ctx)
	oops := detective.Detect(resp, context.DeadlineExceeded)
	require.False(t, oops.Retryable)
	require.ErrorIs(t, oops, context.DeadlineExceeded)
}

func TestDetect_RespectsEndpointWait(t *testing.T) {
	resp := respWith(t, http.StatusTooManyRequests, map[string]string{"Retry-After": "120"})
	for _, wait := range []time.Duration{time.Second, 3 * time.Minute} {
		oops := restyoops.NewDetective(restyoops.WithStatus(http.StatusTooManyRequests, restyoops.Again(wait))).Detect(resp, nil)
		require.Equal(t, max(wait, 2*time.Minute), oops.WaitTime)
	}
}

// TestDetect_DeadlineExceeded sends a timeout again, since time running out often clears up
// TestDetect_DeadlineExceeded 会重发超时，因为时间用完这种情况常常会好转
func TestDetect_DeadlineExceeded(t *testing.T) {
	oops := restyoops.Detect(nil, context.DeadlineExceeded)
	require.NotNil(t, oops)
	t.Logf("分类=%s 可重试=%v", oops.Kind, oops.Retryable)
	require.Equal(t, restyoops.KindNetwork, oops.Kind)
	require.True(t, oops.Retryable)
}

// TestDetect_UnknownCause gives up on a shape it cannot recognize
// TestDetect_UnknownCause 对认不出的形态选择放弃
func TestDetect_UnknownCause(t *testing.T) {
	oops := restyoops.Detect(nil, errors.New("unrecognized fault"))
	require.NotNil(t, oops)
	t.Logf("分类=%s 可重试=%v", oops.Kind, oops.Retryable)
	require.Equal(t, restyoops.KindUnknown, oops.Kind)
	require.False(t, oops.Retryable)
}

// respWith serves one status code and returns the response resty received
// respWith 提供一个状态码，并返回 resty 收到的响应
func respWith(t *testing.T, statusCode int, headers map[string]string) *resty.Response {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		for name, value := range headers {
			w.Header().Set(name, value)
		}
		w.WriteHeader(statusCode)
	}))
	t.Cleanup(server.Close)

	resp, cause := resty.New().R().Get(server.URL)
	require.NoError(t, cause)
	return resp
}

// TestDetect_StatusKinds checks status-based classifications.
// TestDetect_StatusKinds 检查每个状态码都落在与其补救方式相符的分类上
func TestDetect_StatusKinds(t *testing.T) {
	cases := []struct {
		statusCode int
		kind       restyoops.Kind
		retryable  bool
		reason     string
	}{
		{http.StatusOK, "", false, "成功不是故障"},
		{http.StatusMovedPermanently, "", false, "重定向由 resty 处理，不算故障"},
		{http.StatusBadRequest, restyoops.KindClient, false, "请求本身不对，重发还是不对"},
		{http.StatusUnauthorized, restyoops.KindClient, false, "没有凭据，重发也没有"},
		{http.StatusNotFound, restyoops.KindClient, false, "资源不在，重发也不会出现"},
		{http.StatusRequestTimeout, restyoops.KindClient, true, "对端等烦了，再来一次即可"},
		{http.StatusTooEarly, restyoops.KindClient, true, "对端说来早了，稍后再来"},
		{http.StatusTooManyRequests, restyoops.KindThrottle, true, "限流，等够了再来"},
		{http.StatusInternalServerError, restyoops.KindUpstream, true, "服务端出错，可能是一时的"},
		{http.StatusNotImplemented, restyoops.KindUpstream, false, "对端根本没实现，重发无用"},
		{http.StatusBadGateway, restyoops.KindUpstream, true, "网关故障，通常是一时的"},
		{http.StatusServiceUnavailable, restyoops.KindUpstream, true, "服务不可用，通常是一时的"},
	}

	for _, c := range cases {
		oops := restyoops.Detect(respWith(t, c.statusCode, nil), nil)
		if c.kind == "" {
			require.Nil(t, oops, "状态码 %d 不该被当成故障", c.statusCode)
			t.Logf("%3d -> 无故障           （%s）", c.statusCode, c.reason)
			continue
		}
		require.NotNil(t, oops, "状态码 %d 该被当成故障", c.statusCode)
		require.Equal(t, c.kind, oops.Kind, "状态码 %d 分类不符", c.statusCode)
		require.Equal(t, c.retryable, oops.Retryable, "状态码 %d 重试判断不符", c.statusCode)
		require.Equal(t, c.statusCode, oops.StatusCode)
		t.Logf("%3d -> %-8s 可重试=%-5v（%s）", c.statusCode, oops.Kind, oops.Retryable, c.reason)
	}
}

// TestDetect_ExplicitWait preserves the endpoint's stated wait.
// TestDetect_ExplicitWait 采用对端明确说明的等待时长。
func TestDetect_ExplicitWait(t *testing.T) {
	resp := respWith(t, http.StatusTooManyRequests, map[string]string{"Retry-After": "120"})

	oops := restyoops.Detect(resp, nil)
	require.NotNil(t, oops)
	waitTime, stated := restyoops.WaitTimeOf(oops)
	t.Logf("对端要求等待 %v（明确给出=%v）", waitTime, stated)
	require.True(t, stated)
	require.Equal(t, 120*time.Second, waitTime)
}

// TestDetect_KeepsVerdict reuses an existing classification.
//
// TestDetect_KeepsVerdict 沿用交给它的结论，而不是再造一个
// 这让 Detect 能放在流程的任何位置，而不必担心同一个故障被分类两次
func TestDetect_KeepsVerdict(t *testing.T) {
	first := restyoops.Detect(respWith(t, http.StatusInternalServerError, nil), nil)
	require.NotNil(t, first)

	again := restyoops.Detect(nil, first)
	require.Same(t, first, again)
	t.Logf("反复分类拿到的是同一个结论：%v", again)
}
