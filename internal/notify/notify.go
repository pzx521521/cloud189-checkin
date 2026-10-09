// Package notify 负责签到失败时的推送通知：读 PUSH_URL，失败才推。
// 目前仅支持企业微信群机器人 webhook（msgtype=text），为空不推、成功不推。
package notify

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"cloud189-checkin/internal/checkin"
)

var httpClient = &http.Client{Timeout: 10 * time.Second}

// PushURL 读推送地址，为空表示关闭通知。
func PushURL() string {
	return strings.TrimSpace(os.Getenv("PUSH_URL"))
}

// HasFailure 任一顶层 Error 非空即算失败（家庭签到的部分失败不算）。
func HasFailure(results []checkin.Result) bool {
	for _, r := range results {
		if r.Error != "" {
			return true
		}
	}
	return false
}

// BuildContent 组装失败明细文本，user 已在 checkin 侧脱敏。
func BuildContent(results []checkin.Result) string {
	fail := 0
	for _, r := range results {
		if r.Error != "" {
			fail++
		}
	}
	var b strings.Builder
	b.WriteString("cloud189签到失败(" + strconv.Itoa(fail) + "/" + strconv.Itoa(len(results)) + ")\n")
	b.WriteString("时间: " + time.Now().Format(time.RFC3339) + "\n")
	for _, r := range results {
		if r.Error == "" {
			continue
		}
		user := r.User
		if user == "" {
			user = "未知账号"
		}
		b.WriteString("- " + user + ": " + r.Error + "\n")
	}
	return strings.TrimSuffix(b.String(), "\n")
}

// NotifyIfFailed 有 PUSH_URL 且有失败时才推送，失败只打日志不影响主流程。
func NotifyIfFailed(results []checkin.Result) {
	url := PushURL()
	if url == "" || !HasFailure(results) {
		return
	}
	payload, _ := json.Marshal(map[string]any{
		"msgtype": "text",
		"text":    map[string]any{"content": BuildContent(results)},
	})
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		slog.Warn("推送通知构造请求失败", "err", err)
		return
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := httpClient.Do(req)
	if err != nil {
		slog.Warn("推送通知失败", "err", err)
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		slog.Warn("推送通知状态码异常", "status", resp.Status)
	}
}
