// Package secrets 用环境变量提供的应用密钥对落库的敏感配置（如 SMTP 密码）做 AES-GCM 加解密（ADR 0003）。
package secrets

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
)

// EnvKey 应用密钥环境变量名：32 字节，hex（64 位）或 base64 编码。缺失时进程拒绝启动（#212）。
const EnvKey = "APP_SECRET_KEY"

var (
	ErrKeyMissing = errors.New(EnvKey + " 未配置：需要 32 字节密钥（hex 或 base64），见 .env.example")
	ErrKeyInvalid = errors.New(EnvKey + " 不合法：须为 32 字节的 hex 或 base64")
	ErrCiphertext = errors.New("密文不合法或密钥不匹配")
)

// ParseKey 解析环境变量值：64 位 hex 或 base64（标准／URL 安全，可无填充）；必须是 32 字节。
func ParseKey(v string) ([]byte, error) {
	v = strings.TrimSpace(v)
	if v == "" {
		return nil, ErrKeyMissing
	}
	if b, err := hex.DecodeString(v); err == nil && len(b) == 32 {
		return b, nil
	}
	for _, enc := range []*base64.Encoding{base64.StdEncoding, base64.RawStdEncoding, base64.URLEncoding, base64.RawURLEncoding} {
		if b, err := enc.DecodeString(v); err == nil && len(b) == 32 {
			return b, nil
		}
	}
	return nil, ErrKeyInvalid
}

// KeyFromEnv 从环境变量读取并解析应用密钥。
func KeyFromEnv() ([]byte, error) {
	return ParseKey(os.Getenv(EnvKey))
}

// Encrypt AES-256-GCM 加密，输出 base64(nonce ‖ ciphertext)。空明文加密后仍是非空密文。
func Encrypt(key, plaintext []byte) (string, error) {
	gcm, err := newGCM(key)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}
	sealed := gcm.Seal(nonce, nonce, plaintext, nil)
	return base64.StdEncoding.EncodeToString(sealed), nil
}

// Decrypt 解 Encrypt 的输出；密钥不匹配或密文被改一律 ErrCiphertext。
func Decrypt(key []byte, encoded string) ([]byte, error) {
	gcm, err := newGCM(key)
	if err != nil {
		return nil, err
	}
	raw, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil || len(raw) < gcm.NonceSize() {
		return nil, ErrCiphertext
	}
	nonce, ct := raw[:gcm.NonceSize()], raw[gcm.NonceSize():]
	plain, err := gcm.Open(nil, nonce, ct, nil)
	if err != nil {
		return nil, ErrCiphertext
	}
	return plain, nil
}

func newGCM(key []byte) (cipher.AEAD, error) {
	if len(key) != 32 {
		return nil, fmt.Errorf("%w（长度 %d）", ErrKeyInvalid, len(key))
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}
