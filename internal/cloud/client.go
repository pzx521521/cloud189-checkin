// Package cloud 负责签到相关的 API 调用：个人签到、家庭列表、家庭签到。
// 签名与 session 逻辑对标 cloud189-sdk/src/CloudClient.ts + signature.ts，
// 仅保留签到需要的两个通道：WEB_URL（sessionKey）与 API_URL（accessToken 签名）。
package cloud

import (
	"crypto/md5"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"cloud189-checkin/internal/auth"
)

// Client 单个账号的 API 客户端，持有内存中的 sessionKey/accessToken。
type Client struct {
	mu          sync.Mutex
	sessionKey  string
	accessToken string
	http        *http.Client
}

// New 新建客户端。
func New() *Client {
	return &Client{http: &http.Client{Timeout: 20 * time.Second}}
}

// SetSession 登录成功后写入会话（内存缓存）。
func (c *Client) SetSession(sessionKey, accessToken string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.sessionKey = sessionKey
	c.accessToken = accessToken
}

// FetchAccessToken 用 sessionKey 换 API 通道用的 accessToken，
// 对应 SDK 的 getAccessTokenBySsKey（注意不是登录返回的那个 accessToken）。
func (c *Client) FetchAccessToken() error {
	body, err := c.doWeb("/api/open/oauth2/getAccessTokenBySsKey.action", nil)
	if err != nil {
		return err
	}
	var r struct {
		AccessToken string `json:"accessToken"`
		ExpiresIn   int64  `json:"expiresIn"`
		ErrorCode   string `json:"errorCode"`
	}
	if err := json.Unmarshal(body, &r); err != nil {
		return fmt.Errorf("解析 accessToken 失败: %s", string(body))
	}
	if r.AccessToken == "" {
		return fmt.Errorf("换 accessToken 失败: %s", string(body))
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.accessToken = r.AccessToken
	return nil
}

// signParams 对 API_URL 请求做签名，对应 SDK 的 signatureAccesstoken。
func signParams(params url.Values, accessToken string) (sign, ts string) {
	all := url.Values{}
	for k, vs := range params {
		for _, v := range vs {
			all.Add(k, v)
		}
	}
	ts = fmt.Sprintf("%d", time.Now().UnixMilli())
	all.Set("Timestamp", ts)
	all.Set("AccessToken", accessToken)
	var parts []string
	for k, vs := range all {
		for _, v := range vs {
			parts = append(parts, k+"="+v)
		}
	}
	sort.Strings(parts)
	sum := md5.Sum([]byte(strings.Join(parts, "&")))
	return fmt.Sprintf("%x", sum), ts
}

// openAppKey 对应 SDK 里 WEB_URL /open/* 请求用的 appkey。
const openAppKey = "600100422"

// signAppParams 对 WEB_URL /open/* 请求做 AppKey 签名，对应 SDK 的 signatureAppKey。
func signAppParams(params url.Values) (sign, ts string) {
	all := url.Values{}
	for k, vs := range params {
		for _, v := range vs {
			all.Add(k, v)
		}
	}
	ts = fmt.Sprintf("%d", time.Now().UnixMilli())
	all.Set("Timestamp", ts)
	all.Set("AppKey", openAppKey)
	var parts []string
	for k, vs := range all {
		for _, v := range vs {
			parts = append(parts, k+"="+v)
		}
	}
	sort.Strings(parts)
	sum := md5.Sum([]byte(strings.Join(parts, "&")))
	return fmt.Sprintf("%x", sum), ts
}

// doAPI 发起 API_URL 请求并自动带签名（InvalidAccessToken 时由调用方刷新）。
func (c *Client) doAPI(path string, params url.Values) ([]byte, error) {
	c.mu.Lock()
	token := c.accessToken
	c.mu.Unlock()
	sign, ts := signParams(params, token)
	full := auth.APIURL + path + "?" + params.Encode()
	req, _ := http.NewRequest(http.MethodGet, full, nil)
	req.Header.Set("User-Agent", auth.UserAgent)
	req.Header.Set("Accept", "application/json;charset=UTF-8")
	req.Header.Set("Referer", auth.WebURL+"/web/main/")
	req.Header.Set("Sign-Type", "1")
	req.Header.Set("Signature", sign)
	req.Header.Set("Timestamp", ts)
	req.Header.Set("Accesstoken", token)
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	return io.ReadAll(resp.Body)
}

// doWeb 发起 WEB_URL 请求，自动带 sessionKey 参数。
// 路径含 /open 时先按 SDK 顺序做 AppKey 签名（签原始参数），再追加 sessionKey。
func (c *Client) doWeb(path string, params url.Values) ([]byte, error) {
	c.mu.Lock()
	sk := c.sessionKey
	c.mu.Unlock()
	if params == nil {
		params = url.Values{}
	}
	full := auth.WebURL + path + "?" + params.Encode()
	req, _ := http.NewRequest(http.MethodGet, full, nil)
	req.Header.Set("User-Agent", auth.UserAgent)
	req.Header.Set("Accept", "application/json;charset=UTF-8")
	req.Header.Set("Referer", auth.WebURL+"/web/main/")
	if strings.Contains(path, "/open") {
		sign, ts := signAppParams(params)
		req.Header.Set("Sign-Type", "1")
		req.Header.Set("Signature", sign)
		req.Header.Set("Timestamp", ts)
		req.Header.Set("AppKey", openAppKey)
	}
	q := req.URL.Query()
	q.Set("sessionKey", sk)
	req.URL.RawQuery = q.Encode()
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	return io.ReadAll(resp.Body)
}

// UserSign 个人签到，对应 SDK 的 userSign。
func (c *Client) UserSign() (bool, int, error) {
	p := url.Values{}
	p.Set("rand", fmt.Sprintf("%d", time.Now().UnixMilli()))
	p.Set("clientType", "TELEANDROID")
	p.Set("version", "9.0.6")
	p.Set("model", "KB2000")
	body, err := c.doWeb("/mkt/userSign.action", p)
	if err != nil {
		return false, 0, err
	}
	var r struct {
		IsSign       bool   `json:"isSign"`
		NetdiskBonus int    `json:"netdiskBonus"`
		ErrorCode    string `json:"errorCode"`
	}
	if err := json.Unmarshal(body, &r); err != nil {
		return false, 0, fmt.Errorf("解析个人签到返回失败: %s", string(body))
	}
	if r.ErrorCode == "InvalidSessionKey" {
		return false, 0, errInvalidSession
	}
	return r.IsSign, r.NetdiskBonus, nil
}

// Family 家庭信息项。
type Family struct {
	FamilyID   string `json:"familyId"`
	RemarkName string `json:"remarkName"`
	UserRole   int    `json:"userRole"`
}

// GetFamilyList 获取家庭列表，对应 SDK 的 getFamilyList。
// 注意真实接口：familyInfoResp 是单个对象（非数组），familyId 是数字。
func (c *Client) GetFamilyList() ([]Family, error) {
	body, err := c.doAPI("/open/family/manage/getFamilyList.action", url.Values{})
	if err != nil {
		return nil, err
	}
	var r struct {
		Info      json.RawMessage `json:"familyInfoResp"`
		ErrorCode string          `json:"errorCode"`
	}
	if err := json.Unmarshal(body, &r); err != nil {
		return nil, fmt.Errorf("解析家庭列表失败: %s", string(body))
	}
	if r.ErrorCode == "InvalidAccessToken" {
		return nil, errInvalidToken
	}
	fams := parseFamilies(r.Info)
	if len(fams) == 0 {
		slog.Warn("家庭列表为空，可能无家庭或接口变更")
		return nil, nil
	}
	return fams, nil
}

// parseFamilies 兼容对象/数组两种形态的家庭列表。
func parseFamilies(raw json.RawMessage) []Family {
	if len(raw) == 0 {
		return nil
	}
	var arr []map[string]any
	if err := json.Unmarshal(raw, &arr); err == nil {
		var out []Family
		for _, m := range arr {
			out = append(out, toFamily(m))
		}
		return out
	}
	var one map[string]any
	if err := json.Unmarshal(raw, &one); err == nil && len(one) > 0 {
		return []Family{toFamily(one)}
	}
	return nil
}

func toFamily(m map[string]any) Family {
	f := Family{}
	switch v := m["familyId"].(type) {
	case string:
		f.FamilyID = v
	case float64:
		f.FamilyID = strconv.FormatInt(int64(v), 10)
	}
	if s, ok := m["remarkName"].(string); ok {
		f.RemarkName = s
	}
	if n, ok := m["userRole"].(float64); ok {
		f.UserRole = int(n)
	}
	return f
}

// FamilySign 家庭签到，对应 SDK 的 familyUserSign（官方可能下线，调用方必须容错）。
func (c *Client) FamilySign(familyID string) (int, error) {
	p := url.Values{}
	p.Set("familyId", familyID)
	body, err := c.doAPI("/open/family/manage/exeFamilyUserSign.action", p)
	if err != nil {
		return 0, err
	}
	var r struct {
		BonusSpace int    `json:"bonusSpace"`
		SignStatus int    `json:"signStatus"`
		ErrorCode  string `json:"errorCode"`
	}
	if err := json.Unmarshal(body, &r); err != nil {
		return 0, fmt.Errorf("解析家庭签到返回失败: %s", string(body))
	}
	if r.ErrorCode == "InvalidAccessToken" {
		return 0, errInvalidToken
	}
	return r.BonusSpace, nil
}
