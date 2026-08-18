package restyoops

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-resty/resty/v2"
)

// retryAfterOf reads the wait the peer asks through the Retry-After header
// The header comes in two shapes: a count of seconds, or an HTTP-date
// Missing header, or contents matching neither shape, leaves the decision open
//
// retryAfterOf 读出对端通过 Retry-After 头要求的等待时长
// 该头有两种写法：秒数，或 HTTP-date 时刻
// 没有该头、或内容两种写法都不符合时，把决定权交还出去
func retryAfterOf(resp *resty.Response) (time.Duration, bool) {
	if resp == nil || resp.RawResponse == nil {
		return 0, false
	}
	return readRetryAfter(resp.Header().Get("Retry-After"), time.Now())
}

// readRetryAfter turns a Retry-After value into a wait, measured against now
// A moment already past becomes a zero wait, meaning the peer allows going again
//
// readRetryAfter 把 Retry-After 的值换算成等待时长，以 now 为基准
// 已经过去的时刻换算为 0，表示对端允许立刻再来
func readRetryAfter(value string, now time.Time) (time.Duration, bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0, false
	}

	if seconds, err := strconv.Atoi(value); err == nil {
		if seconds < 0 {
			return 0, false
		}
		return time.Duration(seconds) * time.Second, true
	}

	if moment, err := http.ParseTime(value); err == nil {
		return max(moment.Sub(now), 0), true
	}

	return 0, false
}
