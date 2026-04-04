package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"io"
	"strings"

	"golang.org/x/crypto/bcrypt"
)

var (
	// ErrInvalidKey 密钥长度无效
	ErrInvalidKey = errors.New("AES 密钥长度必须为 16、24 或 32 字节")
	// ErrInvalidCiphertext 密文无效
	ErrInvalidCiphertext = errors.New("密文格式无效")
	// ErrDecryptionFailed 解密失败
	ErrDecryptionFailed = errors.New("解密失败")
	// ErrNotInitialized 加密器未初始化
	ErrNotInitialized = errors.New("AES 加密器未初始化")
)

// AESKeyLength 密钥长度常量
const (
	AESKeyLength128 = 16 // AES-128
	AESKeyLength192 = 24 // AES-192
	AESKeyLength256 = 32 // AES-256
)

// AESCrypto AES-GCM 加密器
type AESCrypto struct {
	key []byte
}

// NewAESCrypto 创建 AES 加密器
// key 必须是 16、24 或 32 字节长度
func NewAESCrypto(key string) (*AESCrypto, error) {
	keyBytes := []byte(key)
	keyLen := len(keyBytes)
	if keyLen != AESKeyLength128 && keyLen != AESKeyLength192 && keyLen != AESKeyLength256 {
		return nil, ErrInvalidKey
	}
	return &AESCrypto{key: keyBytes}, nil
}

// Encrypt 使用 AES-GCM 加密明文
// 返回 Base64 编码的密文（包含 nonce 和 ciphertext）
func (a *AESCrypto) Encrypt(plaintext string) (string, error) {
	if plaintext == "" {
		return "", nil
	}

	block, err := aes.NewCipher(a.key)
	if err != nil {
		return "", err
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}

	// 生成随机 nonce
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}

	// 加密并附加认证标签
	ciphertext := gcm.Seal(nonce, nonce, []byte(plaintext), nil)

	// Base64 编码
	return base64.StdEncoding.EncodeToString(ciphertext), nil
}

// Decrypt 使用 AES-GCM 解密密文
// 输入为 Base64 编码的密文（包含 nonce 和 ciphertext）
func (a *AESCrypto) Decrypt(ciphertext string) (string, error) {
	if ciphertext == "" {
		return "", nil
	}

	// Base64 解码
	data, err := base64.StdEncoding.DecodeString(ciphertext)
	if err != nil {
		return "", ErrInvalidCiphertext
	}

	block, err := aes.NewCipher(a.key)
	if err != nil {
		return "", err
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}

	nonceSize := gcm.NonceSize()
	if len(data) < nonceSize {
		return "", ErrInvalidCiphertext
	}

	// 提取 nonce 和密文
	nonce, encryptedData := data[:nonceSize], data[nonceSize:]

	// 解密并验证
	plaintext, err := gcm.Open(nil, nonce, encryptedData, nil)
	if err != nil {
		return "", ErrDecryptionFailed
	}

	return string(plaintext), nil
}

// globalCrypto 全局 AES 加密器实例
var globalCrypto *AESCrypto

// InitAES 初始化全局 AES 加密器
func InitAES(key string) error {
	crypto, err := NewAESCrypto(key)
	if err != nil {
		return err
	}
	globalCrypto = crypto
	return nil
}

// Encrypt 使用全局加密器加密
func Encrypt(plaintext string) (string, error) {
	if globalCrypto == nil {
		return "", ErrNotInitialized
	}
	return globalCrypto.Encrypt(plaintext)
}

// Decrypt 使用全局加密器解密
func Decrypt(ciphertext string) (string, error) {
	if globalCrypto == nil {
		return "", ErrNotInitialized
	}
	return globalCrypto.Decrypt(ciphertext)
}

// IsEncrypted 检查字符串是否为 AES 加密格式
// AES 加密后的字符串是 Base64 编码，长度至少为 nonce(12) + tag(16) = 28 字节
func IsEncrypted(s string) bool {
	if len(s) < 28 {
		return false
	}
	// 尝试 Base64 解码
	_, err := base64.StdEncoding.DecodeString(s)
	return err == nil
}

// IsBcryptHash 检查字符串是否为 bcrypt 哈希格式
func IsBcryptHash(s string) bool {
	return strings.HasPrefix(s, "$2a$") || strings.HasPrefix(s, "$2b$") || strings.HasPrefix(s, "$2y$")
}

// VerifyDevicePassword 验证设备密码并识别是否为历史 bcrypt 存储格式。
// 返回值:
//   - match: 密码是否匹配
//   - legacy: 是否命中历史 bcrypt 存储格式
func VerifyDevicePassword(stored, plainInput string) (match bool, legacy bool, err error) {
	if stored == "" {
		return false, false, nil
	}

	// 先尝试 AES 解密（新格式）
	decrypted, decErr := Decrypt(stored)
	if decErr == nil {
		return decrypted == plainInput, false, nil
	}

	// 兼容历史 bcrypt（旧格式）
	if IsBcryptHash(stored) {
		err = bcrypt.CompareHashAndPassword([]byte(stored), []byte(plainInput))
		if err == nil {
			return true, true, nil
		}
		if errors.Is(err, bcrypt.ErrMismatchedHashAndPassword) {
			return false, true, nil
		}
		return false, true, err
	}

	// 既不是可解密 AES，也不是 bcrypt
	return false, false, decErr
}

// DecodeDevicePassword 解码设备密码。
// 返回 legacy=true 表示命中历史 bcrypt 存储格式（不可逆，需要重置或迁移）。
func DecodeDevicePassword(stored string) (plain string, legacy bool, err error) {
	if stored == "" {
		return "", false, nil
	}
	decrypted, decErr := Decrypt(stored)
	if decErr == nil {
		return decrypted, false, nil
	}
	if IsBcryptHash(stored) {
		return "", true, nil
	}
	return "", false, decErr
}
