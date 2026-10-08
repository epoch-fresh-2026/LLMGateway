// Package crypto holds the secret-handling primitives: symmetric encryption for
// upstream channel api keys, hashing for gateway API keys, bcrypt password
// hashing and session token generation/hashing.
//
// It is a leaf package: it must not depend on internal/store or internal/httpapi.
package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"strings"

	"golang.org/x/crypto/bcrypt"
)

// Password hashing cost bounds mirror bcrypt so callers (and config parsing)
// can validate a configured cost without importing bcrypt directly.
const (
	MinPasswordCost     = bcrypt.MinCost
	MaxPasswordCost     = bcrypt.MaxCost
	DefaultPasswordCost = bcrypt.DefaultCost
)

// ErrInvalidKey is returned when an encryption key is missing or malformed.
var ErrInvalidKey = errors.New("invalid encryption key")

// defaultGatewayKeyPrefix matches the gateway key format used by the dashboard.
const defaultGatewayKeyPrefix = "sk-"

// Cipher encrypts and decrypts upstream channel api keys with AES-GCM.
type Cipher struct {
	aead cipher.AEAD
}

// NewCipher builds a Cipher from a raw key of 16, 24 or 32 bytes.
func NewCipher(key []byte) (*Cipher, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("%w: length must be 16, 24 or 32 bytes", ErrInvalidKey)
	}
	return newCipher(block)
}

func newCipher(block cipher.Block) (*Cipher, error) {
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidKey, err)
	}
	return &Cipher{aead: aead}, nil
}

// NewCipherFromEnv builds a Cipher from the named environment variable. A
// missing or malformed value is an error; callers must not fall back to
// plaintext storage.
func NewCipherFromEnv(name string) (*Cipher, error) {
	value := os.Getenv(name)
	if value == "" {
		return nil, fmt.Errorf("%w: environment variable %s is not set", ErrInvalidKey, name)
	}
	return NewCipher([]byte(value))
}

// Encrypt returns a base64 string containing the random nonce and ciphertext.
func (c *Cipher) Encrypt(plaintext string) (string, error) {
	nonce := make([]byte, c.aead.NonceSize())
	// Go 1.24 and later terminate on entropy failure; Read always returns nil.
	_, _ = rand.Read(nonce)
	sealed := c.aead.Seal(nonce, nonce, []byte(plaintext), nil)
	return base64.StdEncoding.EncodeToString(sealed), nil
}

// Decrypt reverses Encrypt. A wrong key or tampered input returns an error.
func (c *Cipher) Decrypt(encoded string) (string, error) {
	raw, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return "", err
	}

	nonceSize := c.aead.NonceSize()
	if len(raw) < nonceSize {
		return "", errors.New("crypto: ciphertext too short")
	}

	plaintext, err := c.aead.Open(nil, raw[:nonceSize], raw[nonceSize:], nil)
	if err != nil {
		return "", err
	}
	return string(plaintext), nil
}

// GenerateGatewayKey returns a new random gateway key. Only the hash should be
// persisted; the plaintext is returned to the caller once.
func GenerateGatewayKey(prefix string) (string, error) {
	if prefix == "" {
		prefix = defaultGatewayKeyPrefix
	}

	buf := make([]byte, 24)
	_, _ = rand.Read(buf)
	return prefix + hex.EncodeToString(buf), nil
}

// HashKey returns the SHA-256 hex digest of a gateway key. Surrounding
// whitespace is trimmed so equivalent inputs hash identically.
func HashKey(key string) string {
	return sha256Hex(key)
}

// HashSessionToken returns the SHA-256 hex digest of a session token. Session
// tokens are high-entropy random values, so a plain digest is sufficient;
// unlike passwords they need no salt or slow KDF. Only this hash is persisted.
func HashSessionToken(token string) string {
	return sha256Hex(token)
}

// EqualHashes compares two hex digests in constant time.
func EqualHashes(a, b string) bool {
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}

// HashPassword returns a bcrypt hash of password. A cost <= 0 selects
// DefaultPasswordCost; any other cost must be within [MinPasswordCost,
// MaxPasswordCost] and otherwise returns an error.
func HashPassword(password string, cost int) (string, error) {
	if cost <= 0 {
		cost = DefaultPasswordCost
	}
	if cost < MinPasswordCost || cost > MaxPasswordCost {
		return "", fmt.Errorf("crypto: password cost %d outside allowed range (%d,%d)", cost, MinPasswordCost, MaxPasswordCost)
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), cost)
	if err != nil {
		return "", err
	}
	return string(hash), nil
}

// VerifyPassword reports whether password matches a bcrypt hash. A malformed
// hash and a mismatched password both return false so callers cannot leak which
// case occurred.
func VerifyPassword(hash, password string) bool {
	if hash == "" {
		return false
	}
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) == nil
}

// GenerateSessionToken returns a new random session token. Only its hash
// (HashSessionToken) should be persisted; the plaintext is returned to the
// caller once and delivered to the browser as an HttpOnly cookie.
func GenerateSessionToken() (string, error) {
	buf := make([]byte, 32)
	_, _ = rand.Read(buf)
	return hex.EncodeToString(buf), nil
}

// sha256Hex is the shared digest behind gateway key and session token hashing.
// Inputs are whitespace-trimmed so equivalent values hash identically.
func sha256Hex(value string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(value)))
	return hex.EncodeToString(sum[:])
}
