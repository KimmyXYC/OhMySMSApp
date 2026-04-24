// password.go —— bcrypt 密码校验封装。
package auth

import (
	"errors"

	"golang.org/x/crypto/bcrypt"
)

// comparePassword 比较 bcrypt hash 与明文密码。
func comparePassword(hash, plain string) error {
	if hash == "" {
		return errors.New("no password configured")
	}
	if err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(plain)); err != nil {
		return errors.New("invalid credentials")
	}
	return nil
}

// HashPassword 生成 bcrypt hash（cost=12），供 CLI 子命令使用。
func HashPassword(plain string) (string, error) {
	b, err := bcrypt.GenerateFromPassword([]byte(plain), 12)
	if err != nil {
		return "", err
	}
	return string(b), nil
}
