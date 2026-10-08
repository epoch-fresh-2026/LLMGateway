package crypto

import (
	"errors"
	"strings"
	"testing"
)

type invalidBlock struct{}

func (invalidBlock) BlockSize() int          { return 8 }
func (invalidBlock) Encrypt(dst, src []byte) {}
func (invalidBlock) Decrypt(dst, src []byte) {}

func TestCryptoInvalidConstructionAndPassword(t *testing.T) {
	if _, err := newCipher(invalidBlock{}); !errors.Is(err, ErrInvalidKey) {
		t.Fatalf("invalid GCM block: %v", err)
	}
	if _, err := HashPassword(strings.Repeat("p", 73), MinPasswordCost); err == nil {
		t.Fatal("bcrypt must reject overlong passwords")
	}
	t.Setenv("COVERAGE_CIPHER_KEY", "")
	if _, err := NewCipherFromEnv("COVERAGE_CIPHER_KEY"); !errors.Is(err, ErrInvalidKey) {
		t.Fatal(err)
	}
	if _, err := GenerateGatewayKey(""); err != nil {
		t.Fatal(err)
	}
}
