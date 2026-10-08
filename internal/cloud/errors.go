package cloud

import "errors"

// 会话失效标记，调用方据此决定刷新 token 后重试一次。
var (
	// errInvalidToken 对应 SDK 的 InvalidAccessToken。
	errInvalidToken = errors.New("InvalidAccessToken")
	// errInvalidSession 对应 SDK 的 InvalidSessionKey。
	errInvalidSession = errors.New("InvalidSessionKey")
)

// IsInvalid 判断是否为会话失效错误。
func IsInvalid(err error) bool {
	return err == errInvalidToken || err == errInvalidSession
}
