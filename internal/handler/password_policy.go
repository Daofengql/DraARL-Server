package handler

import (
	"fmt"
	"unicode/utf8"
)

const (
	minUserPasswordRunes = 6
	maxUserPasswordBytes = 72
)

// validateNewUserPassword applies policy only when a password is created or
// replaced. Login remains compatible with hashes created under older rules.
func validateNewUserPassword(password string) error {
	if !utf8.ValidString(password) {
		return fmt.Errorf("密码必须是有效文本")
	}
	if utf8.RuneCountInString(password) < minUserPasswordRunes {
		return fmt.Errorf("密码长度不能少于%d个字符", minUserPasswordRunes)
	}
	if len(password) > maxUserPasswordBytes {
		return fmt.Errorf("密码长度不能超过%d字节", maxUserPasswordBytes)
	}
	return nil
}
