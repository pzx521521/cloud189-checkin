// Package main 是 Vercel Go Server 入口（对应用官方文档的 cmd/api/main.go 模式）。
// 路由：静态前端 + /api/login|accounts|logs|checkin。
// 前端鉴权：用任一 189 账号密码登录，session 为 HMAC Cookie（key=GH_TOKEN/GITHUB_TOKEN）。
package main

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"log/slog"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"cloud189-checkin/internal/checkin"
	"cloud189-checkin/internal/notify"
	"cloud189-checkin/internal/store"
	"cloud189-checkin/web"
)

var backend store.Store

func main() {
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stdout, nil)))
	// Gist 配齐则用 Gist，否则禁用 token/日志功能，每次账号密码登录。
	if g := store.NewGist(store.GitHubToken(), os.Getenv("GIST_ID")); g != nil {
		backend = g
		slog.Info("使用 Gist 存储")
	} else {
		backend = store.NewDisabled()
		slog.Info("未配置 GH_TOKEN/GITHUB_TOKEN/GIST_ID，token/日志功能关闭，每次账号密码登录")
	}

	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/login", handleLogin)
	mux.HandleFunc("GET /api/accounts", needAuth(handleAccountsGet))
	mux.HandleFunc("POST /api/accounts", needAuth(handleAccountsSave))
	mux.HandleFunc("PUT /api/accounts", needAuth(handleAccountsSave))
	mux.HandleFunc("DELETE /api/accounts", needAuth(handleAccountsDelete))
	mux.HandleFunc("GET /api/logs", needAuth(handleLogs))
	mux.HandleFunc("GET /api/checkin", handleCheckin) // cron 用 GET
	mux.HandleFunc("POST /api/checkin", needAuth(handleCheckin))
	// 静态前端
	mux.Handle("GET /", http.FileServer(http.FS(web.FS)))

	port := os.Getenv("PORT")
	if port == "" {
		port = "3000"
	}
	slog.Info("启动", "port", port)
	if err := http.ListenAndServe(":"+port, mux); err != nil {
		slog.Error("退出", "err", err)
		os.Exit(1)
	}
}

// session 签发与校验：payload=username|exp，sig=HMAC_SHA256(key=GH_TOKEN/GITHUB_TOKEN)。
func signSession(username string) string {
	exp := time.Now().Add(7 * 24 * time.Hour).Unix()
	payload := username + "|" + strconv.FormatInt(exp, 10)
	mac := hmac.New(sha256.New, []byte(store.GitHubToken()+"|session"))
	mac.Write([]byte(payload))
	sig := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	return base64.RawURLEncoding.EncodeToString([]byte(payload + "|" + sig))
}

func verifySession(cookie string) bool {
	raw, err := base64.RawURLEncoding.DecodeString(cookie)
	if err != nil {
		return false
	}
	parts := strings.Split(string(raw), "|")
	if len(parts) != 3 {
		return false
	}
	payload := parts[0] + "|" + parts[1]
	mac := hmac.New(sha256.New, []byte(store.GitHubToken()+"|session"))
	mac.Write([]byte(payload))
	expect := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	if !hmac.Equal([]byte(expect), []byte(parts[2])) {
		return false
	}
	exp, _ := strconv.ParseInt(parts[1], 10, 64)
	return time.Now().Unix() < exp
}

func needAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c, err := r.Cookie("session")
		if err != nil || !verifySession(c.Value) {
			http.Error(w, "未登录", http.StatusUnauthorized)
			return
		}
		next(w, r)
	}
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(v)
}

// handleLogin 任一账号密码匹配即登录。
func handleLogin(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "参数错误", http.StatusBadRequest)
		return
	}
	accounts, err := backend.LoadAccounts()
	if err != nil {
		http.Error(w, "读账号失败: "+err.Error(), http.StatusInternalServerError)
		return
	}
	ok := false
	for _, a := range accounts {
		if a.Username == req.Username && a.Password == req.Password {
			ok = true
			break
		}
	}
	if !ok {
		http.Error(w, "账号或密码错误", http.StatusUnauthorized)
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name: "session", Value: signSession(req.Username),
		Path: "/", HttpOnly: true, SameSite: http.SameSiteLaxMode, MaxAge: 7 * 24 * 3600,
	})
	writeJSON(w, map[string]any{"ok": true})
}

// handleAccountsGet 脱敏返回账号列表。
func handleAccountsGet(w http.ResponseWriter, r *http.Request) {
	accounts, err := backend.LoadAccounts()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	type item struct {
		Username string `json:"username"`
		HasPass  bool   `json:"hasPass"`
	}
	out := make([]item, 0, len(accounts))
	for _, a := range accounts {
		out = append(out, item{Username: a.Username, HasPass: a.Password != ""})
	}
	writeJSON(w, map[string]any{"accounts": out})
}

// handleAccountsSave 全量保存账号。
func handleAccountsSave(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Accounts []store.Account `json:"accounts"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || len(req.Accounts) == 0 {
		http.Error(w, "参数错误", http.StatusBadRequest)
		return
	}
	if err := backend.SaveAccounts(req.Accounts); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]any{"ok": true, "count": len(req.Accounts)})
}

// handleAccountsDelete 按用户名删除。
func handleAccountsDelete(w http.ResponseWriter, r *http.Request) {
	username := r.URL.Query().Get("username")
	accounts, err := backend.LoadAccounts()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	var keep []store.Account
	for _, a := range accounts {
		if a.Username != username {
			keep = append(keep, a)
		}
	}
	if err := backend.SaveAccounts(keep); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]any{"ok": true})
}

// handleLogs 分页读日志。
func handleLogs(w http.ResponseWriter, r *http.Request) {
	logs, err := backend.LoadLogs()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	skip, _ := strconv.Atoi(r.URL.Query().Get("skip"))
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	if skip < 0 {
		skip = 0
	}
	if skip > len(logs) {
		skip = len(logs)
	}
	end := skip + limit
	if end > len(logs) {
		end = len(logs)
	}
	writeJSON(w, map[string]any{"total": len(logs), "logs": logs[skip:end]})
}

// handleCheckin 执行全部账号签到并返回结果，有失败且配了 PUSH_URL 则推送。
func handleCheckin(w http.ResponseWriter, r *http.Request) {
	results := checkin.RunAll(backend)
	notify.NotifyIfFailed(results)
	writeJSON(w, map[string]any{"results": results, "time": time.Now().Format(time.RFC3339)})
}
