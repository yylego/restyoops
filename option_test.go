package restyoops_test

import (
	"bytes"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/go-resty/resty/v2"
	"github.com/stretchr/testify/require"
	"github.com/yylego/restyoops"
)

// TestOption_StatusOutranksKind lets a rule pinned to one status code overrule its kind
// TestOption_StatusOutranksKind 让针对单个状态码的规则压过其所属分类
func TestOption_StatusOutranksKind(t *testing.T) {
	endpoint, count := serveStatus(t, http.StatusForbidden, nil)

	plain := restyoops.Setup(resty.New(), quick()...)
	_, cause := plain.R().Get(endpoint)
	require.Error(t, cause)
	t.Logf("默认下 403 被访问 %d 次", count.hits.Load())
	require.Equal(t, int32(1), count.hits.Load())

	// An endpoint contract can define a transient 403 response during startup.
	// 对端在预热期间返回 403 时值得再给一次机会，这跟单纯的拒绝不同
	count.hits.Store(0)
	patient := restyoops.Setup(resty.New(), quick(
		restyoops.WithStatus(http.StatusForbidden, restyoops.Again(time.Millisecond)),
	)...)
	_, cause = patient.R().Get(endpoint)
	require.Error(t, cause)
	t.Logf("改规则后 403 被访问 %d 次", count.hits.Load())
	require.Equal(t, int32(4), count.hits.Load())
}

// TestOption_KindCoversWholeGroup decides a whole kind of fault in one stroke
// TestOption_KindCoversWholeGroup 一笔决定一整类故障
func TestOption_KindCoversWholeGroup(t *testing.T) {
	endpoint, count := serveStatus(t, http.StatusBadGateway, nil)

	client := restyoops.Setup(resty.New(), quick(
		restyoops.WithKind(restyoops.KindUpstream, restyoops.Abort()),
	)...)
	_, cause := client.R().Get(endpoint)
	require.Error(t, cause)

	t.Logf("整类叫停后 502 被访问 %d 次", count.hits.Load())
	require.Equal(t, int32(1), count.hits.Load())
}

// TestOption_CheckSeesPayload detects a captcha despite HTTP 200.
//
// TestOption_CheckSeesPayload 发现状态码掩盖住的故障，这正是检查函数存在的理由
// 对端在 200 之下返回验证码页面时，任何只看状态码的判断都会当成成功
func TestOption_CheckSeesPayload(t *testing.T) {
	endpoint, count := serveCaptcha(t)

	client := restyoops.Setup(resty.New(), quick(
		restyoops.WithCheck(func(resp *resty.Response, _ error) *restyoops.Oops {
			if resp == nil || resp.RawResponse == nil {
				return nil
			}
			if !bytes.Contains(resp.Body(), []byte("captcha")) {
				return nil
			}
			return restyoops.NewOops(restyoops.KindBlock,
				errors.New("captcha page served"), restyoops.Abort())
		}),
	)...)

	resp, cause := client.R().Get(endpoint)
	require.Error(t, cause, "200 之下的验证码页面也要被认出来")

	var oops *restyoops.Oops
	require.True(t, errors.As(cause, &oops))
	t.Logf("状态码 %d 之下认出了：%v", resp.StatusCode(), cause)
	require.Equal(t, restyoops.KindBlock, oops.Kind)
	require.Equal(t, http.StatusOK, oops.StatusCode)
	require.False(t, oops.Retryable)
	require.Equal(t, int32(1), count.hits.Load(), "叫停之后不该再敲对端")
}

// TestOption_CheckCanSeeHeaders inspects response metadata in a custom check.
//
// TestOption_CheckCanSeeHeaders 证明检查函数能看到整个响应，包括头
// 旧的签名只传了内容类型和报文，把携带答案的那些头挡在了外面
func TestOption_CheckCanSeeHeaders(t *testing.T) {
	endpoint, _ := serveStatus(t, http.StatusOK, map[string]string{"X-Quota-State": "drained"})

	client := restyoops.Setup(resty.New(), quick(
		restyoops.WithCheck(func(resp *resty.Response, _ error) *restyoops.Oops {
			if resp == nil || resp.Header().Get("X-Quota-State") != "drained" {
				return nil
			}
			return restyoops.NewOops(restyoops.KindBusiness,
				errors.New("quota drained"), restyoops.Abort())
		}),
	)...)

	_, cause := client.R().Get(endpoint)
	require.Error(t, cause)
	t.Logf("从响应头认出了：%v", cause)
}

// TestOption_CustomKind accepts endpoint-specific kinds.
//
// TestOption_CustomKind 接受本包从未听说过的分类
// 旧的写法对自己清单之外的任何分类都直接朝调用方 panic
func TestOption_CustomKind(t *testing.T) {
	const kindCustom = restyoops.Kind("CUSTOM")

	endpoint, count := serveStatus(t, http.StatusOK, nil)
	client := restyoops.Setup(resty.New(), quick(
		restyoops.WithCheck(func(_ *resty.Response, _ error) *restyoops.Oops {
			return restyoops.NewOops(kindCustom, errors.New("resource not available"),
				restyoops.Again(time.Millisecond))
		}),
	)...)

	_, cause := client.R().Get(endpoint)
	require.Error(t, cause)

	var oops *restyoops.Oops
	require.True(t, errors.As(cause, &oops))
	t.Logf("自定义分类 %s 正常工作，重发 %d 次", oops.Kind, count.hits.Load()-1)
	require.Equal(t, kindCustom, oops.Kind)
	require.Equal(t, int32(4), count.hits.Load())
}

// TestOption_StatedZeroSurvives distinguishes zero from an unspecified wait.
//
// TestOption_StatedZeroSurvives 把"故意写 0"和"没写"区分开
// 旧的写法把明确写下的 0 读作"没设置"，然后悄悄换成了自己的默认值，
// 于是想要最短等待的调用方根本没有办法把这件事表达出来
func TestOption_StatedZeroSurvives(t *testing.T) {
	resp := respWith(t, http.StatusInternalServerError, nil)

	stated := restyoops.NewDetective(
		restyoops.WithStatus(http.StatusInternalServerError, restyoops.Again(0)),
	).Detect(resp, nil)
	waitTime, given := restyoops.WaitTimeOf(stated)
	t.Logf("明确写 0：waitTime=%v 明确给出=%v", waitTime, given)
	require.True(t, given, "明确写下的 0 必须被保留成'已给出'")
	require.Equal(t, time.Duration(0), waitTime)

	absent := restyoops.NewDetective(
		restyoops.WithStatus(http.StatusInternalServerError, restyoops.Again()),
	).Detect(resp, nil)
	waitTime, given = restyoops.WaitTimeOf(absent)
	t.Logf("没写时长：waitTime=%v 明确给出=%v", waitTime, given)
	require.False(t, given, "没写时长应当交给退避算法")
	require.Equal(t, time.Duration(0), waitTime)
}

// TestOption_StatedWaitStaysFlat checks fixed waits across attempts.
// TestOption_StatedWaitStaysFlat 让给定的等待时长保持不变，而不是每次尝试都增长
func TestOption_StatedWaitStaysFlat(t *testing.T) {
	endpoint, count := serveStatus(t, http.StatusInternalServerError, nil)

	client := restyoops.Setup(resty.New(),
		restyoops.WithAttempts(3),
		restyoops.WithWaitTime(10*time.Millisecond, time.Minute),
		restyoops.WithStatus(http.StatusInternalServerError, restyoops.Again(20*time.Millisecond)),
	)

	since := time.Now()
	_, cause := client.R().Get(endpoint)
	elapsed := time.Since(since)
	require.Error(t, cause)

	t.Logf("固定等待 20ms 重试 3 次，共耗时 %v，访问 %d 次", elapsed.Round(time.Millisecond), count.hits.Load())
	require.Equal(t, int32(4), count.hits.Load())
	require.GreaterOrEqual(t, elapsed, 60*time.Millisecond, "三次等待都该真的等到")
	require.Less(t, elapsed, 500*time.Millisecond, "给定时长不该随尝试次数增长")
}

// TestOption_NoAttempts sends the request once and lets its verdict stand
// TestOption_NoAttempts 只发一次请求，结果就是最终结果
func TestOption_NoAttempts(t *testing.T) {
	endpoint, count := serveStatus(t, http.StatusInternalServerError, nil)

	client := restyoops.Setup(resty.New(), restyoops.WithAttempts(0))
	_, cause := client.R().Get(endpoint)
	require.Error(t, cause)

	t.Logf("不重试时被访问 %d 次", count.hits.Load())
	require.Equal(t, int32(1), count.hits.Load())
}
