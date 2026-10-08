// Package auth 负责天翼账号登录：密码登录、accessToken 登录、refreshToken 刷新。
// 逻辑对标 cloud189-sdk/src/CloudAuthClient.ts，仅保留签到需要的部分。
package auth

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

const (
	// WebURL 对应 SDK 的 WEB_URL。
	WebURL = "https://cloud.189.cn"
	// AuthURL 对应 SDK 的 AUTH_URL。
	AuthURL = "https://open.e.189.cn"
	// APIURL 对应 SDK 的 API_URL。
	APIURL = "https://api.cloud.189.cn"
	// AppID 对应 SDK 的 AppID。
	AppID = "8025431004"
	// ClientType 对应 SDK 的 ClientType。
	ClientType = "10020"
	// ReturnURL 对应 SDK 的 ReturnURL。
	ReturnURL = "https://m.cloud.189.cn/zhuanti/2020/loginErrorPc/index.html"
	// UserAgent 对应 SDK 的 UserAgent。
	UserAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/87.0.4280.88 Safari/537.36"
)

var httpClient = &http.Client{Timeout: 20 * time.Second}

// TokenSession 登录成功后的会话，对应 SDK 的 TokenSession（只保留签到需要的字段）。
type TokenSession struct {
	AccessToken  string `json:"accessToken"`
	RefreshToken string `json:"refreshToken"`
	SessionKey   string `json:"sessionKey"`
}

// RefreshSession 刷新 token 的返回，对应 SDK 的 RefreshTokenSession。
type RefreshSession struct {
	AccessToken  string `json:"accessToken"`
	RefreshToken string `json:"refreshToken"`
	ExpiresIn    int64  `json:"expiresIn"`
}

// encryptConf 获取 RSA 公钥的返回。
type encryptConf struct {
	Data struct {
		PubKey string `json:"pubKey"`
		Pre    string `json:"pre"`
	} `json:"data"`
}

// loginForm 登录页解析出的参数，对应 SDK 的 CacheQuery。
type loginForm struct {
	CaptchaToken string
	Lt           string
	ParamID      string
	ReqID        string
}

// rsaEncrypt 用公钥加密，对应 SDK util.ts 的 rsaEncrypt（PKCS1，hex 输出）。
func rsaEncrypt(pubKey, orig string) (string, error) {
	raw, err := base64.StdEncoding.DecodeString(pubKey)
	if err != nil {
		// 公钥可能是裸 base64（无 PEM 头），尝试按 DER 解析
		return "", fmt.Errorf("解析公钥失败: %w", err)
	}
	pub, err := x509.ParsePKIXPublicKey(raw)
	if err != nil {
		return "", fmt.Errorf("解析公钥失败: %w", err)
	}
	rsaPub, ok := pub.(*rsa.PublicKey)
	if !ok {
		return "", fmt.Errorf("不是 RSA 公钥")
	}
	enc, err := rsa.EncryptPKCS1v15(rand.Reader, rsaPub, []byte(orig))
	if err != nil {
		return "", err
	}
	return hex.EncodeToString(enc), nil
}

// getEncrypt 获取加密公钥。
func getEncrypt() (string, string, error) {
	req, _ := http.NewRequest(http.MethodPost, AuthURL+"/api/logbox/config/encryptConf.do", nil)
	req.Header.Set("User-Agent", UserAgent)
	req.Header.Set("Accept", "application/json;charset=UTF-8")
	resp, err := httpClient.Do(req)
	if err != nil {
		return "", "", err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	var c encryptConf
	if err := json.Unmarshal(body, &c); err != nil {
		return "", "", err
	}
	return c.Data.PubKey, c.Data.Pre, nil
}

// getLoginForm 拉登录页并解析参数。
func getLoginForm() (*loginForm, error) {
	u := WebURL + "/api/portal/unifyLoginForPC.action?appId=" + AppID +
		"&clientType=" + ClientType + "&returnURL=" + url.QueryEscape(ReturnURL) +
		fmt.Sprintf("&timeStamp=%d", time.Now().UnixMilli())
	req, _ := http.NewRequest(http.MethodGet, u, nil)
	req.Header.Set("User-Agent", UserAgent)
	req.Header.Set("Accept", "application/json;charset=UTF-8")
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	html := string(body)
	m := func(re string) string {
		found := regexp.MustCompile(re).FindStringSubmatch(html)
		if len(found) > 1 {
			return found[1]
		}
		return ""
	}
	f := &loginForm{
		CaptchaToken: m(`'captchaToken' value='(.+?)'`),
		Lt:           m(`lt = "(.+?)"`),
		ParamID:      m(`paramId = "(.+?)"`),
		ReqID:        m(`reqId = "(.+?)"`),
	}
	if f.Lt == "" || f.ReqID == "" {
		return nil, fmt.Errorf("解析登录页失败")
	}
	return f, nil
}

// GetSessionForPC 用 redirectURL 或 accessToken 换会话，对应 SDK 的 getSessionForPC。
func GetSessionForPC(redirectURL, accessToken string) (*TokenSession, error) {
	q := url.Values{}
	q.Set("appId", AppID)
	q.Set("clientType", "TELEPC")
	q.Set("version", "6.2")
	q.Set("channelId", "web_cloud.189.cn")
	q.Set("rand", fmt.Sprintf("%d", time.Now().UnixMilli()))
	if redirectURL != "" {
		q.Set("redirectURL", redirectURL)
	}
	if accessToken != "" {
		q.Set("accessToken", accessToken)
	}
	req, _ := http.NewRequest(http.MethodPost, APIURL+"/getSessionForPC.action?"+q.Encode(), nil)
	req.Header.Set("User-Agent", UserAgent)
	req.Header.Set("Accept", "application/json;charset=UTF-8")
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	var s TokenSession
	if err := json.Unmarshal(body, &s); err != nil {
		return nil, err
	}
	if s.SessionKey == "" {
		return nil, fmt.Errorf("换会话失败: %s", string(body))
	}
	return &s, nil
}

// LoginByPassword 用户名密码登录，对应 SDK 的 loginByPassword。
func LoginByPassword(username, password string) (*TokenSession, error) {
	slog.Info("密码登录", "user", mask(username))
	pubKey, pre, err := getEncrypt()
	if err != nil {
		return nil, err
	}
	form, err := getLoginForm()
	if err != nil {
		return nil, err
	}
	userEnc, err := rsaEncrypt(pubKey, username)
	if err != nil {
		return nil, err
	}
	passEnc, err := rsaEncrypt(pubKey, password)
	if err != nil {
		return nil, err
	}
	v := url.Values{}
	v.Set("appKey", AppID)
	v.Set("accountType", "02")
	v.Set("validateCode", "")
	v.Set("captchaToken", form.CaptchaToken)
	v.Set("dynamicCheck", "FALSE")
	v.Set("clientType", "1")
	v.Set("cb_SaveName", "3")
	v.Set("isOauth2", "false")
	v.Set("returnUrl", ReturnURL)
	v.Set("paramId", form.ParamID)
	v.Set("userName", pre+userEnc)
	v.Set("password", pre+passEnc)
	req, _ := http.NewRequest(http.MethodPost, AuthURL+"/api/logbox/oauth2/loginSubmit.do",
		strings.NewReader(v.Encode()))
	req.Header.Set("User-Agent", UserAgent)
	req.Header.Set("Accept", "application/json;charset=UTF-8")
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Referer", AuthURL)
	req.Header.Set("lt", form.Lt)
	req.Header.Set("REQID", form.ReqID)
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	var lr struct {
		Result int    `json:"result"`
		Msg    string `json:"msg"`
		ToURL  string `json:"toUrl"`
	}
	if err := json.Unmarshal(body, &lr); err != nil {
		return nil, err
	}
	if lr.ToURL == "" {
		return nil, fmt.Errorf("登录失败 result=%d msg=%s", lr.Result, lr.Msg)
	}
	return GetSessionForPC(lr.ToURL, "")
}

// LoginByAccessToken 用 accessToken 换会话，对应 SDK 的 loginByAccessToken。
func LoginByAccessToken(accessToken string) (*TokenSession, error) {
	return GetSessionForPC("", accessToken)
}

// Refresh 用 refreshToken 换新 token，对应 SDK 的 refreshToken。
func Refresh(refreshToken string) (*RefreshSession, error) {
	v := url.Values{}
	v.Set("clientId", AppID)
	v.Set("refreshToken", refreshToken)
	v.Set("grantType", "refresh_token")
	v.Set("format", "json")
	req, _ := http.NewRequest(http.MethodPost, AuthURL+"/api/oauth2/refreshToken.do",
		strings.NewReader(v.Encode()))
	req.Header.Set("User-Agent", UserAgent)
	req.Header.Set("Accept", "application/json;charset=UTF-8")
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	var s RefreshSession
	if err := json.Unmarshal(body, &s); err != nil {
		return nil, err
	}
	if s.AccessToken == "" {
		return nil, fmt.Errorf("刷新 token 失败: %s", string(body))
	}
	return &s, nil
}

// mask 用户名脱敏。
func mask(s string) string {
	if len(s) <= 4 {
		return "***"
	}
	return s[:3] + "***" + s[len(s)-2:]
}
