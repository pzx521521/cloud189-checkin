// Package store 负责账号、token、日志三类数据的读写。
// Store 接口预留扩展点：现在是 Gist 实现，之后可加数据库、WebDAV 实现。
package store

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"
)

// Account 单个账号。
type Account struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

// Token 复用的登录态，对应 SDK FileTokenStore 存的内容。
type Token struct {
	AccessToken  string `json:"accessToken"`
	RefreshToken string `json:"refreshToken"`
	ExpiresIn    int64  `json:"expiresIn"` // 毫秒时间戳
}

// Valid token 是否可用（accessToken 未过期）。
func (t Token) Valid() bool {
	return t.AccessToken != "" && t.ExpiresIn > time.Now().UnixMilli()
}

// LogEntry 单次签到日志。
type LogEntry struct {
	Time     string           `json:"time"`
	User     string           `json:"user"` // 脱敏
	LoginMethod string         `json:"loginMethod,omitempty"`
	TokenRefreshed bool       `json:"tokenRefreshed,omitempty"`
	Personal map[string]any   `json:"personal"`
	Families []map[string]any `json:"families"`
	Error    string           `json:"error,omitempty"`
}

// Store 数据存储接口，之后接数据库/WebDAV 只需实现它。
type Store interface {
	// LoadAccounts 读取账号（Gist 为准，缺失时用 ACCOUNTS_189 环境变量播种）。
	LoadAccounts() ([]Account, error)
	// SaveAccounts 全量保存账号。
	SaveAccounts([]Account) error
	// LoadTokens 读取全部 token。
	LoadTokens() (map[string]Token, error)
	// SaveTokens 全量保存 token。
	SaveTokens(map[string]Token) error
	// AppendLog 追加一条日志（内部截断保留最近 200 条）。
	AppendLog(LogEntry) error
	// LoadLogs 读取日志。
	LoadLogs() ([]LogEntry, error)
}

// ParseAccounts 解析 ACCOUNTS_189 环境变量：逗号或换行分隔，username:password。
func ParseAccounts(s string) ([]Account, error) {
	s = strings.ReplaceAll(s, "\r", "\n")
	parts := strings.FieldsFunc(s, func(r rune) bool { return r == ',' || r == '\n' || r == ';' })
	var out []Account
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		idx := strings.Index(p, ":")
		if idx <= 0 {
			return nil, fmt.Errorf("账号格式错误，应为 username:password：%s", mask(p))
		}
		out = append(out, Account{Username: strings.TrimSpace(p[:idx]), Password: strings.TrimSpace(p[idx+1:])})
	}
	return out, nil
}

// EnvAccounts 从环境变量读种子账号。
func EnvAccounts() []Account {
	a, _ := ParseAccounts(os.Getenv("ACCOUNTS_189"))
	return a
}

// GitHubToken 读 GitHub PAT，GH_TOKEN 优先，兼容 GITHUB_TOKEN。
func GitHubToken() string {
	if t := os.Getenv("GH_TOKEN"); t != "" {
		return t
	}
	return os.Getenv("GITHUB_TOKEN")
}

func mask(s string) string {
	if len(s) <= 4 {
		return "***"
	}
	return s[:3] + "***"
}

// Mem 内存兜底实现（本地调试无 Gist 时用），非并发安全要求高场景够用。
type Mem struct {
	mu       sync.Mutex
	accounts []Account
	tokens   map[string]Token
	logs     []LogEntry
}

// NewMem 新建内存存储，并用环境变量播种账号。
func NewMem() *Mem {
	return &Mem{tokens: map[string]Token{}, accounts: EnvAccounts()}
}

func (m *Mem) LoadAccounts() ([]Account, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.accounts) == 0 {
		return EnvAccounts(), nil
	}
	out := append([]Account(nil), m.accounts...)
	return out, nil
}

func (m *Mem) SaveAccounts(a []Account) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.accounts = append([]Account(nil), a...)
	return nil
}

func (m *Mem) LoadTokens() (map[string]Token, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := map[string]Token{}
	for k, v := range m.tokens {
		out[k] = v
	}
	return out, nil
}

func (m *Mem) SaveTokens(t map[string]Token) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.tokens = t
	return nil
}

func (m *Mem) AppendLog(e LogEntry) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.logs = append(m.logs, e)
	if len(m.logs) > 200 {
		m.logs = m.logs[len(m.logs)-200:]
	}
	return nil
}

func (m *Mem) LoadLogs() ([]LogEntry, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := append([]LogEntry(nil), m.logs...)
	// 新的在前
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out, nil
}

var _ = json.Marshal
