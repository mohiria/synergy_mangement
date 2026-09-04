package secrets

import (
	"bytes"
	"encoding/hex"
	"errors"
	"strings"
	"testing"
)

func TestParseKey(t *testing.T) {
	raw := bytes.Repeat([]byte{0xab}, 32)
	cases := []struct {
		name string
		in   string
		want error
	}{
		{"hex 合法", hex.EncodeToString(raw), nil},
		{"base64 合法", "q6urq6urq6urq6urq6urq6urq6urq6urq6urq6urq6s=", nil},
		{"缺失", "", ErrKeyMissing},
		{"长度不对", "abcd", ErrKeyInvalid},
		{"非法字符", strings.Repeat("z", 64), ErrKeyInvalid},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := ParseKey(c.in)
			if !errors.Is(err, c.want) {
				t.Fatalf("ParseKey(%q) err = %v, want %v", c.in, err, c.want)
			}
			if err == nil && len(got) != 32 {
				t.Fatalf("key len = %d", len(got))
			}
		})
	}
}

// 加密结果每次不同（随机 nonce），能解回原文；换密钥或改密文解不开；密文不含明文。
func TestEncryptDecrypt(t *testing.T) {
	key := bytes.Repeat([]byte{1}, 32)
	other := bytes.Repeat([]byte{2}, 32)
	ct1, err := Encrypt(key, []byte("smtp-secret"))
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	ct2, _ := Encrypt(key, []byte("smtp-secret"))
	if ct1 == ct2 {
		t.Fatal("随机 nonce 下两次密文不应相同")
	}
	if strings.Contains(ct1, "smtp-secret") {
		t.Fatal("密文不应含明文")
	}
	plain, err := Decrypt(key, ct1)
	if err != nil || string(plain) != "smtp-secret" {
		t.Fatalf("decrypt = %q, %v", plain, err)
	}
	if _, err := Decrypt(other, ct1); !errors.Is(err, ErrCiphertext) {
		t.Fatalf("换密钥应失败: %v", err)
	}
	if _, err := Decrypt(key, ct1[:len(ct1)-4]+"AAAA"); !errors.Is(err, ErrCiphertext) {
		t.Fatalf("篡改密文应失败: %v", err)
	}
	if _, err := Encrypt([]byte("short"), []byte("x")); !errors.Is(err, ErrKeyInvalid) {
		t.Fatalf("短密钥应拒绝: %v", err)
	}
}
