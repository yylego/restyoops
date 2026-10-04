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

// TestOops_ErrorInterface checks error assignment and errors.As.
//
// TestOops_ErrorInterface 验证 error 接口赋值和 errors.As。
// 旧的写法要求调用方在惯常的 "err != nil" 之外再查一次 "oops != nil"，
// 这意味着本包发现的故障无法走通任何一条普通的错误链路
func TestOops_ErrorInterface(t *testing.T) {
	var cause error = restyoops.NewOops(restyoops.KindBlock, errors.New("captcha"), restyoops.Abort())
	require.Error(t, cause)

	var oops *restyoops.Oops
	require.True(t, errors.As(cause, &oops))
	t.Logf("作为 error 传递后仍能取回：%s", oops.Kind)
}

// TestOops_UnwrapReachesCause checks errors.Is and errors.Unwrap.
// TestOops_UnwrapReachesCause 让 errors.Is 能看穿到真正出问题的那一层
func TestOops_UnwrapReachesCause(t *testing.T) {
	root := errors.New("connection reset")
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
		Cause:      errors.New("quota exhausted"),
	}

	text := oops.Error()
	t.Logf("错误文本：%s", text)
	for _, mark := range []string{"THROTTLE", "GET", "https://api.example.com/items", "429", "attempt=2", "wait=30s", "quota exhausted"} {
		require.Contains(t, text, mark)
	}
}

// TestOops_ErrorTextWithoutCause includes status details without inventing a cause.
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

// TestOops_GivesUpWithoutRule stops repeats when no rule is supplied.
// Giving up is the safe default, since repeating something unexamined can do harm
//
// TestOops_GivesUpWithoutRule 在没有规则说明时把故障当作最终结果
// 放弃才是安全的默认值，因为重复一件没有想清楚的事情可能造成伤害
func TestOops_GivesUpWithoutRule(t *testing.T) {
	oops := restyoops.NewOops(restyoops.KindBusiness, errors.New("balance too low"))
	t.Logf("不给规则时：可重试=%v", oops.Retryable)
	require.False(t, oops.Retryable)
}

// TestOops_AcceptsUnknownKind accepts custom kind values.
//
// TestOops_AcceptsUnknownKind 接受本包从未听说过的分类，且不加抱怨
// 旧的写法持有一份固定清单，对清单之外的一切直接朝调用方 panic，
// 这让开放的 Kind 类型变成了陷阱：看着可以扩展，实际拒绝一切扩展
func TestOops_AcceptsUnknownKind(t *testing.T) {
	require.NotPanics(t, func() {
		oops := restyoops.NewOops(restyoops.Kind("CUSTOM"), errors.New("custom fault"), restyoops.Again())
		t.Logf("自定义分类被接受：%v", oops)
		require.True(t, oops.Retryable)
	})
}

// TestOops_FormatsThroughErrorVerb checks formatting through the error interface.
// TestOops_FormatsThroughErrorVerb 能像任何其它 error 一样通过 error 格式化动词打印
func TestOops_FormatsThroughErrorVerb(t *testing.T) {
	oops := restyoops.NewOops(restyoops.KindUpstream, errors.New("upstream down"), restyoops.Again())
	text := fmt.Sprintf("请求失败: %v", oops)
	t.Logf("%s", text)
	require.Contains(t, text, "upstream down")
}
