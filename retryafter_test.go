package restyoops

import (
	"testing"
	"time"
)

// TestParseRetryAfter covers both shapes the header comes in, plus the contents to ignore
// A peer stating how long to hold off is the one authoritative answer available, so reading it
// wrong in either direction hurts: too short keeps hammering, too long stalls the caller
//
// TestParseRetryAfter 覆盖该头的两种写法，以及应当忽略的内容
// 对端说明该停多久是唯一权威的答案，因此两个方向读错都有害：
// 读短了会继续猛敲对端，读长了会让调用方空等
func TestParseRetryAfter(t *testing.T) {
	now := time.Date(2026, 8, 19, 12, 0, 0, 0, time.UTC)

	cases := []struct {
		value    string
		waitTime time.Duration
		stated   bool
		reason   string
	}{
		{"120", 2 * time.Minute, true, "秒数写法"},
		{"0", 0, true, "秒数写法，允许立刻再来"},
		{" 45 ", 45 * time.Second, true, "两侧空白应当忽略"},
		{"Wed, 19 Aug 2026 12:02:00 GMT", 2 * time.Minute, true, "时刻写法"},
		{"Wed, 19 Aug 2026 11:58:00 GMT", 0, true, "已经过去的时刻，表示可以立刻再来"},
		{"", 0, false, "没有该头，决定权交还退避"},
		{"soon", 0, false, "两种写法都不符合，不猜"},
		{"-5", 0, false, "负秒数没有意义，不采信"},
	}

	for _, c := range cases {
		waitTime, stated := readRetryAfter(c.value, now)
		if stated != c.stated || waitTime != c.waitTime {
			t.Fatalf("值 %q 解析为 (%v, %v)，期望 (%v, %v)", c.value, waitTime, stated, c.waitTime, c.stated)
		}
		t.Logf("%-32q -> 等待=%-8v 明确给出=%-5v（%s）", c.value, waitTime, stated, c.reason)
	}
}
