package store

import "fmt"

// Disabled 无存储实现：未配置 GH_TOKEN/GITHUB_TOKEN/GIST_ID 任一时使用。
// token 与日志功能关闭，每次都用账号密码登录；账号只读 ACCOUNTS_189 环境变量。
type Disabled struct{}

// NewDisabled 新建禁用存储。
func NewDisabled() *Disabled {
	return &Disabled{}
}

// LoadAccounts 仅从环境变量读取。
func (d *Disabled) LoadAccounts() ([]Account, error) {
	a := EnvAccounts()
	if len(a) == 0 {
		return nil, fmt.Errorf("ACCOUNTS_189 为空")
	}
	return a, nil
}

// SaveAccounts 未配置 Gist 时不支持保存。
func (d *Disabled) SaveAccounts([]Account) error {
	return fmt.Errorf("未配置 GH_TOKEN/GITHUB_TOKEN/GIST_ID，账号管理不可用")
}

// LoadTokens 始终返回空，调用方每次走密码登录。
func (d *Disabled) LoadTokens() (map[string]Token, error) {
	return map[string]Token{}, nil
}

// SaveTokens 直接丢弃，不持久化。
func (d *Disabled) SaveTokens(map[string]Token) error {
	return nil
}

// AppendLog 直接丢弃，不记日志。
func (d *Disabled) AppendLog(LogEntry) error {
	return nil
}

// LoadLogs 始终返回空。
func (d *Disabled) LoadLogs() ([]LogEntry, error) {
	return nil, nil
}
