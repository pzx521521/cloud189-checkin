package store

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"sync"
	"time"
)

// 文件名固定在同一个私密 Gist 里。
const (
	fileAccounts = "189_accounts.json"
	fileTokens   = "189_tokens.json"
	fileLogs     = "189_logs.json"
)

// Gist 基于 GitHub 私密 Gist 的 Store 实现：GET 读、PATCH 写。
type Gist struct {
	token  string
	gistID string
	http   *http.Client
	mu     sync.Mutex
}

// NewGist 新建 Gist 存储，空参数时返回 nil（调用方降级用内存）。
func NewGist(token, gistID string) *Gist {
	if token == "" || gistID == "" {
		return nil
	}
	return &Gist{token: token, gistID: gistID, http: &http.Client{Timeout: 20 * time.Second}}
}

// files 拉取 Gist 全量文件内容。
func (g *Gist) files() (map[string]string, error) {
	req, _ := http.NewRequest(http.MethodGet, "https://api.github.com/gists/"+g.gistID, nil)
	req.Header.Set("Authorization", "Bearer "+g.token)
	req.Header.Set("Accept", "application/vnd.github+json")
	resp, err := g.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("读 Gist 失败 %d: %s", resp.StatusCode, string(body))
	}
	var r struct {
		Files map[string]struct {
			Content string `json:"content"`
		} `json:"files"`
	}
	if err := json.Unmarshal(body, &r); err != nil {
		return nil, err
	}
	out := map[string]string{}
	for k, v := range r.Files {
		out[k] = v.Content
	}
	return out, nil
}

// putFile 写单个文件。
func (g *Gist) putFile(name, content string) error {
	payload, _ := json.Marshal(map[string]any{"files": map[string]any{name: map[string]any{"content": content}}})
	req, _ := http.NewRequest(http.MethodPatch, "https://api.github.com/gists/"+g.gistID, bytes.NewReader(payload))
	req.Header.Set("Authorization", "Bearer "+g.token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("Content-Type", "application/json")
	resp, err := g.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body)
	if resp.StatusCode != 200 {
		return fmt.Errorf("写 Gist 失败 %d", resp.StatusCode)
	}
	return nil
}

// LoadAccounts Gist 为准，缺失时用 ACCOUNTS_189 播种并回写。
func (g *Gist) LoadAccounts() ([]Account, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	files, err := g.files()
	if err != nil {
		return nil, err
	}
	raw := files[fileAccounts]
	if raw == "" {
		seed := EnvAccounts()
		if len(seed) == 0 {
			return nil, fmt.Errorf("Gist 无账号且 ACCOUNTS_189 为空")
		}
		data, _ := json.MarshalIndent(seed, "", "  ")
		if err := g.putFile(fileAccounts, string(data)); err != nil {
			slog.Warn("播种账号回写失败", "err", err)
		}
		return seed, nil
	}
	var out []Account
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return nil, err
	}
	if len(out) == 0 {
		return EnvAccounts(), nil
	}
	return out, nil
}

// SaveAccounts 全量保存账号。
func (g *Gist) SaveAccounts(a []Account) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	data, _ := json.MarshalIndent(a, "", "  ")
	return g.putFile(fileAccounts, string(data))
}

// LoadTokens 读取 token 表。
func (g *Gist) LoadTokens() (map[string]Token, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.loadTokensLocked()
}

func (g *Gist) loadTokensLocked() (map[string]Token, error) {
	// 注意：调用方已加锁
	files, err := g.files()
	if err != nil {
		return nil, err
	}
	out := map[string]Token{}
	if files[fileTokens] != "" {
		if err := json.Unmarshal([]byte(files[fileTokens]), &out); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// SaveTokens 全量保存 token。
func (g *Gist) SaveTokens(t map[string]Token) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	data, _ := json.MarshalIndent(t, "", "  ")
	return g.putFile(fileTokens, string(data))
}

// saveTokensLocked 加锁上下文内保存（签到循环用，避免重复加锁）。
func (g *Gist) saveTokensLocked(t map[string]Token) error {
	data, _ := json.MarshalIndent(t, "", "  ")
	return g.putFile(fileTokens, string(data))
}

// AppendLog 追加日志并截断 200 条。
func (g *Gist) AppendLog(e LogEntry) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	logs := g.loadLogsLocked()
	logs = append(logs, e)
	if len(logs) > 200 {
		logs = logs[len(logs)-200:]
	}
	data, _ := json.MarshalIndent(logs, "", "  ")
	return g.putFile(fileLogs, string(data))
}

// LoadLogs 读取日志（新的在前）。
func (g *Gist) LoadLogs() ([]LogEntry, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	logs := g.loadLogsLocked()
	for i, j := 0, len(logs)-1; i < j; i, j = i+1, j-1 {
		logs[i], logs[j] = logs[j], logs[i]
	}
	return logs, nil
}

func (g *Gist) loadLogsLocked() []LogEntry {
	files, err := g.files()
	if err != nil {
		slog.Warn("读日志失败", "err", err)
		return nil
	}
	var logs []LogEntry
	if files[fileLogs] != "" {
		_ = json.Unmarshal([]byte(files[fileLogs]), &logs)
	}
	return logs
}
