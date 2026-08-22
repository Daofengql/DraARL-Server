package handler

import (
	"crypto/subtle"
	"errors"
	"strings"

	"golang.org/x/crypto/bcrypt"
)

var errPrivateGroupPasswordRequired = errors.New("private group password is required")

func validateGroupPasswordRequirement(groupType int, password string) error {
	if groupType == groupTypePrivate && password == "" {
		return errPrivateGroupPasswordRequired
	}
	return nil
}

// hashGroupPassword 对群组密码做 bcrypt 哈希；空密码保持为空（公开群组）。
// 【H9 安全修复】群组密码不再明文落库。
func hashGroupPassword(password string) (string, error) {
	if password == "" {
		return "", nil
	}
	hashed, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return "", err
	}
	return string(hashed), nil
}

// verifyGroupPassword 校验群组密码。
// 新数据为 bcrypt 哈希（恒定时间比较）；兼容历史明文存储使用
// subtle.ConstantTimeCompare 进行恒定时间比较，避免时序侧信道。
func verifyGroupPassword(stored, plain string) bool {
	if stored == "" {
		return false
	}
	if isBcryptGroupPassword(stored) {
		return bcrypt.CompareHashAndPassword([]byte(stored), []byte(plain)) == nil
	}
	return subtle.ConstantTimeCompare([]byte(stored), []byte(plain)) == 1
}

func isBcryptGroupPassword(value string) bool {
	if len(value) != 60 || (!strings.HasPrefix(value, "$2a$") && !strings.HasPrefix(value, "$2b$") && !strings.HasPrefix(value, "$2y$")) {
		return false
	}
	for _, ch := range value[7:] {
		if !isBcryptBase64Char(ch) {
			return false
		}
	}
	_, err := bcrypt.Cost([]byte(value))
	return err == nil
}

func isBcryptBase64Char(ch rune) bool {
	return ch == '.' || ch == '/' || ch >= 'A' && ch <= 'Z' || ch >= 'a' && ch <= 'z' || ch >= '0' && ch <= '9'
}
