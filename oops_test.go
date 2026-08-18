package restyoops_test

import (
	"errors"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/yylego/restyoops"
)

// TestOops_IsAnError puts a fault where every Go caller already looks for one
// The old shape asked callers to check "oops != nil" alongside the usual "err != nil",
// which meant a fault this package found could not travel through any ordinary error path
//
// TestOops_IsAnError 把故障放在每个 Go 调用方本来就会去看的位置
// 旧的写法要求调用方在惯常的 "err != nil" 之外再查一次 "oops != nil"，
// 这意味着本包发现的故障无法走通任何一条普通的错误链路
func TestOops_IsAnError(t *testing.T) {
	var holder error = restyoops.NewOops(restyoops.KindBlock, errors.New("captcha"), restyoops.Abort())
	require.Error(t, holder)

	var oops *restyoops.Oops
	require.True(t, errors.As(holder, &oops))
	t.Logf("作为 error 传递后仍能取回：%s", oops.Kind)
}

// TestOops_UnwrapReachesCause lets errors.Is see through to what actually went wrong
// TestOops_UnwrapReachesCause 让 errors.Is 能看穿到真正出问题的那一层
func TestOops_UnwrapReachesCause(t *testing.T) {
	root := errors.New("connection reset by peer")
	oops := restyoops.NewOops(restyoops.KindNetwork, root, restyoops.Again())

	require.ErrorIs(t, oops, root)
	require.Equal(t, root, errors.Unwrap(oops))
	t.Logf("穿透到底层原因：%v", errors.Unwrap(oops))
}

// TestOops_ErrorTextLocatesIt leads with what is needed to find the request again
// TestOops_ErrorTextLocatesIt 把重新定位这个请求所需的信息放在前面
func TestOops_ErrorTextLocatesIt(t *testing.T) {
	oops := &restyoops.Oops{
		Kind:       restyoops.KindThrottle,
		StatusCode: http.StatusTooManyRequests,
		Method:     http.MethodGet,
		URL:        "https://api.example.com/items",
		Attempt:    2,
		Retryable:  true,
		WaitTime:   30 * time.Second,
		Cause:      errors.New("quota per minute reached"),
	}

	text := oops.Error()
	t.Logf("错误文本：%s", text)
	for _, mark := range []string{"THROTTLE", "GET", "https://api.example.com/items", "429", "attempt=2", "wait=30s", "quota per minute reached"} {
		require.Contains(t, text, mark)
	}
}

// TestOops_ErrorTextWithoutCause stays readable when the status code already tells the story
// The old shape invented a cause reading just "HTTP" so a non-nil check would pass, which put
// a placeholder carrying no information into every message
//
// TestOops_ErrorTextWithoutCause 在状态码已经说明问题时依然可读
// 旧的写法为了通过非空检查，造了一个内容就是 "HTTP" 的原因，
// 结果把一个毫无信息量的占位符塞进了每一条消息
func TestOops_ErrorTextWithoutCause(t *testing.T) {
	oops := &restyoops.Oops{Kind: restyoops.KindClient, StatusCode: http.StatusNotFound}
	text := oops.Error()
	t.Logf("没有底层原因时的文本：%s", text)
	require.Contains(t, text, "CLIENT")
	require.Contains(t, text, "404")
	require.Nil(t, errors.Unwrap(oops))
}

// TestOops_GivesUpWithoutRule treats a fault as final unless a rule says otherwise
// Giving up is the safe default, since repeating something unexamined can do harm
//
// TestOops_GivesUpWithoutRule 在没有规则说明时把故障当作最终结果
// 放弃才是安全的默认值，因为重复一件没有想清楚的事情可能造成伤害
func TestOops_GivesUpWithoutRule(t *testing.T) {
	oops := restyoops.NewOops(restyoops.KindBusiness, errors.New("balance too low"))
	t.Logf("不给规则时：可重试=%v", oops.Retryable)
	require.False(t, oops.Retryable)
}

// TestOops_AcceptsUnknownKind takes a kind this package never heard of, without complaint
// The old shape held a fixed list and panicked on the caller for anything outside it, which made
// the open Kind type a trap: it looked extensible while refusing every extension
//
// TestOops_AcceptsUnknownKind 接受本包从未听说过的分类，且不加抱怨
// 旧的写法持有一份固定清单，对清单之外的一切直接朝调用方 panic，
// 这让开放的 Kind 类型变成了陷阱：看着可以扩展，实际拒绝一切扩展
func TestOops_AcceptsUnknownKind(t *testing.T) {
	require.NotPanics(t, func() {
		oops := restyoops.NewOops(restyoops.Kind("PROXY-STALE"), errors.New("swap the proxy"), restyoops.Again())
		t.Logf("自定义分类被接受：%v", oops)
		require.True(t, oops.Retryable)
	})
}

// TestOops_FormatsThroughErrorVerb prints through the error verb like any other error
// TestOops_FormatsThroughErrorVerb 能像任何其它 error 一样通过 error 格式化动词打印
func TestOops_FormatsThroughErrorVerb(t *testing.T) {
	oops := restyoops.NewOops(restyoops.KindUpstream, errors.New("gateway down"), restyoops.Again())
	text := fmt.Sprintf("请求失败: %v", oops)
	t.Logf("%s", text)
	require.Contains(t, text, "gateway down")
}
