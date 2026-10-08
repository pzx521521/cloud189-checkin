// Package checkin 负责单个账号的签到编排：token 复用优先，家庭签到容错。
// 优先级对标 SDK CloudClient.getSession：accessToken → refreshToken → 密码登录。
package checkin

import (
	"log/slog"
	"time"

	"cloud189-checkin/internal/auth"
	"cloud189-checkin/internal/cloud"
	"cloud189-checkin/internal/store"
)

// Result 单账号签到结果。
type Result struct {
	User     string           `json:"user"`
	Personal map[string]any   `json:"personal"`
	Families []map[string]any `json:"families"`
	Error    string           `json:"error,omitempty"`
}

// RunAll 循环所有账号签到，并回写 token 与日志。
func RunAll(st store.Store) []Result {
	accounts, err := st.LoadAccounts()
	if err != nil {
		slog.Error("读账号失败", "err", err)
		return []Result{{Error: err.Error()}}
	}
	tokens, err := st.LoadTokens()
	if err != nil {
		slog.Warn("读 token 失败，走密码登录", "err", err)
		tokens = map[string]store.Token{}
	}
	var out []Result
	dirty := false
	for _, a := range accounts {
		r := runOne(a, tokens)
		if r.Error == "" {
			dirty = true
		} else if tokens[a.Username].AccessToken != "" {
			// 即使失败也可能刷新了 token，标记保存
			dirty = true
		}
		out = append(out, r)
		// 落日志（脱敏用户名）
		_ = st.AppendLog(store.LogEntry{
			Time:     time.Now().Format(time.RFC3339),
			User:     maskUser(a.Username),
			Personal: r.Personal,
			Families: r.Families,
			Error:    r.Error,
		})
	}
	if dirty {
		if err := st.SaveTokens(tokens); err != nil {
			slog.Warn("回写 token 失败", "err", err)
		}
	}
	return out
}

// runOne 单账号签到，tokens 会被原地更新。
func runOne(a store.Account, tokens map[string]store.Token) (r Result) {
	r.User = maskUser(a.Username)
	r.Personal = map[string]any{}
	r.Families = []map[string]any{}

	// 1. 有效 accessToken 直接用
	t := tokens[a.Username]
	if t.Valid() {
		if s, err := auth.LoginByAccessToken(t.AccessToken); err == nil {
			return doSign(prepareClient(s), a)
		} else {
			slog.Warn("accessToken 登录失败，尝试刷新", "user", r.User, "err", err)
		}
	}
	// 2. refreshToken 刷新
	if t.RefreshToken != "" {
		if rs, err := auth.Refresh(t.RefreshToken); err == nil {
			if s, err := auth.LoginByAccessToken(rs.AccessToken); err == nil {
				tokens[a.Username] = store.Token{
					AccessToken:  rs.AccessToken,
					RefreshToken: firstNonEmpty(rs.RefreshToken, t.RefreshToken),
					ExpiresIn:    time.Now().Add(6 * 24 * time.Hour).UnixMilli(),
				}
				return doSign(prepareClient(s), a)
			}
		}
		slog.Warn("刷新 token 失败，走密码登录", "user", r.User)
	}
	// 3. 密码登录兜底
	s, err := auth.LoginByPassword(a.Username, a.Password)
	if err != nil {
		r.Error = "登录失败: " + err.Error()
		return r
	}
	tokens[a.Username] = store.Token{
		AccessToken:  s.AccessToken,
		RefreshToken: s.RefreshToken,
		ExpiresIn:    time.Now().Add(6 * 24 * time.Hour).UnixMilli(),
	}
	return doSign(prepareClient(s), a)
}

// prepareClient 由登录会话组装 API 客户端：sessionKey 走 WEB 通道，
// 再换取 API 通道专用的 accessToken（失败只告警，个人签到不受影响）。
func prepareClient(s *auth.TokenSession) *cloud.Client {
	c := cloud.New()
	c.SetSession(s.SessionKey, "")
	if err := c.FetchAccessToken(); err != nil {
		slog.Warn("换 API accessToken 失败，家庭签到可能跳过", "err", err)
	}
	return c
}

// doSign 已登录后执行个人+家庭签到，家庭失败只记录不中断。
func doSign(c *cloud.Client, a store.Account) (r Result) {
	r.User = maskUser(a.Username)
	r.Personal = map[string]any{}
	r.Families = []map[string]any{}
	isSign, bonus, err := c.UserSign()
	if err != nil {
		r.Error = "个人签到失败: " + err.Error()
		return r
	}
	r.Personal["isSign"] = isSign
	r.Personal["bonusMB"] = bonus

	// 家庭签到容错：无家庭/接口下线都不影响主流程
	fams, err := c.GetFamilyList()
	if err != nil {
		slog.Warn("家庭列表失败，跳过家庭签到", "user", r.User, "err", err)
		r.Families = append(r.Families, map[string]any{"skip": err.Error()})
		return r
	}
	for _, f := range fams {
		item := map[string]any{"familyId": f.FamilyID, "name": f.RemarkName}
		b, err := c.FamilySign(f.FamilyID)
		if err != nil {
			slog.Warn("家庭签到失败", "user", r.User, "family", f.FamilyID, "err", err)
			item["error"] = err.Error()
		} else {
			item["bonusMB"] = b
		}
		r.Families = append(r.Families, item)
	}
	return r
}

func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

func maskUser(s string) string {
	if len(s) <= 4 {
		return "***"
	}
	return s[:3] + "***" + s[len(s)-2:]
}
