package restyoops

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-resty/resty/v2"
)

// responseWaitDuration parses Retry-After as seconds / an HTTP-date.
// Absent / invalid values leave the wait unspecified.
//
// responseWaitDuration 解析响应头 Retry-After 中要求的等待时长
// 该头有两种写法：秒数，或 HTTP-date 时刻
// 没有该头、或内容两种写法都不符合时，把决定权交还出去
func responseWaitDuration(resp *resty.Response) (time.Duration, bool) {
	if resp == nil || resp.RawResponse == nil {
		return 0, false
	}
	return parseWaitDuration(resp.Header().Get("Retry-After"), time.Now())
}

// parseWaitDuration converts Retry-After to a duration; past dates produce zero.
//
// parseWaitDuration 把 Retry-After 的值换算成等待时长，以 now 为基准
// 已经过去的时刻换算为 0，表示对端允许立刻再来
func parseWaitDuration(value string, now time.Time) (time.Duration, bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0, false
	}

	seconds, err := strconv.Atoi(value)
	if err != nil {
		moment, err := http.ParseTime(value)
		if err != nil {
			return 0, false
		}
		return max(moment.Sub(now), 0), true
	}
	if seconds < 0 {
		return 0, false
	}
	return time.Duration(seconds) * time.Second, true
}
