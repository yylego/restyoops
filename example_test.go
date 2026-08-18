package restyoops_test

import (
	"bytes"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"time"

	"github.com/go-resty/resty/v2"
	"github.com/yylego/restyoops"
)

// Setup installs the policy once, leaving each call site to read like ordinary Go
// Setup 一次装好策略，让每个调用点读起来就是普通的 Go 代码
func ExampleSetup() {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()

	client := restyoops.Setup(resty.New(),
		restyoops.WithAttempts(2),
		restyoops.WithWaitTime(time.Millisecond, time.Minute),
	)

	_, cause := client.R().Get(server.URL)
	if cause != nil {
		var oops *restyoops.Oops
		if errors.As(cause, &oops) {
			fmt.Println("分类:", oops.Kind)
			fmt.Println("状态码:", oops.StatusCode)
			fmt.Println("尝试次数:", oops.Attempt)
		}
	}
	// Output:
	// 分类: UPSTREAM
	// 状态码: 503
	// 尝试次数: 3
}

// A check reads what the status code hides, since only the caller knows the peer
// 检查函数读出状态码掩盖的东西，因为只有调用方了解对端
func ExampleWithCheck() {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`<html>please solve the captcha</html>`))
	}))
	defer server.Close()

	client := restyoops.Setup(resty.New(),
		restyoops.WithAttempts(0),
		restyoops.WithCheck(func(resp *resty.Response, _ error) *restyoops.Oops {
			if resp == nil || !bytes.Contains(resp.Body(), []byte("captcha")) {
				return nil // 不是验证码页面，交还给内置检测 // not a captcha page, hand it back
			}
			return restyoops.NewOops(restyoops.KindBlock,
				errors.New("captcha page served"), restyoops.Abort())
		}),
	)

	_, cause := client.R().Get(server.URL)

	var oops *restyoops.Oops
	if errors.As(cause, &oops) {
		fmt.Println("分类:", oops.Kind)
		fmt.Println("状态码:", oops.StatusCode)
		fmt.Println("可重试:", oops.Retryable)
		fmt.Println("原因:", oops.Cause)
	}
	// Output:
	// 分类: BLOCK
	// 状态码: 200
	// 可重试: false
	// 原因: captcha page served
}

// Detect answers what a finished call ran into, without taking over the retrying
// Detect 回答一次已完成的调用碰到了什么，而不接管重试
func ExampleDetect() {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "30")
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer server.Close()

	oops := restyoops.Detect(resty.New().R().Get(server.URL))
	fmt.Println("分类:", oops.Kind)
	fmt.Println("可重试:", oops.Retryable)
	fmt.Println("对端要求等待:", oops.WaitTime)
	// Output:
	// 分类: THROTTLE
	// 可重试: true
	// 对端要求等待: 30s
}
