package crypto

import (
	"strings"
	"testing"
)

// testEnvChannelKey mirrors config.EnvChannelKey without creating a crypto ->
// config dependency.
const testEnvChannelKey = "CHANNEL_KEY_ENCRYPTION_KEY"

func testKey() []byte {
	return []byte("0123456789abcdef0123456789abcdef")
}

func TestEncryptDecryptRoundTrip(t *testing.T) {
	c, err := NewCipher(testKey())
	if err != nil {
		t.Fatalf("NewCipher: %v", err)
	}

	for _, plaintext := range []string{"sk-secret-value", "", "多字节-密钥-🔐"} {
		ciphertext, err := c.Encrypt(plaintext)
		if err != nil {
			t.Fatalf("Encrypt(%q): %v", plaintext, err)
		}
		if plaintext != "" && strings.Contains(ciphertext, plaintext) {
			t.Fatalf("ciphertext leaked plaintext: %q", ciphertext)
		}

		got, err := c.Decrypt(ciphertext)
		if err != nil {
			t.Fatalf("Decrypt: %v", err)
		}
		if got != plaintext {
			t.Fatalf("round trip = %q, want %q", got, plaintext)
		}
	}
}

func TestEncryptIsNonDeterministic(t *testing.T) {
	c, _ := NewCipher(testKey())
	first, err := c.Encrypt("sk-secret-value")
	if err != nil {
		t.Fatal(err)
	}
	second, err := c.Encrypt("sk-secret-value")
	if err != nil {
		t.Fatal(err)
	}
	if first == second {
		t.Fatal("ciphertext should differ across encryptions due to random nonce")
	}
}

func TestDecryptWithWrongKeyFails(t *testing.T) {
	c, _ := NewCipher(testKey())
	ciphertext, err := c.Encrypt("sk-secret-value")
	if err != nil {
		t.Fatal(err)
	}

	other, err := NewCipher([]byte("fedcba9876543210fedcba9876543210"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := other.Decrypt(ciphertext); err == nil {
		t.Fatal("decrypt with wrong key should fail")
	}
}

func TestDecryptInvalidInputFails(t *testing.T) {
	c, _ := NewCipher(testKey())
	for _, bad := range []string{"", "not-base64!!!", "c2hvcnQ="} {
		if _, err := c.Decrypt(bad); err == nil {
			t.Fatalf("Decrypt(%q) should fail", bad)
		}
	}
}

func TestNewCipherRejectsInvalidKeyLength(t *testing.T) {
	for _, key := range [][]byte{nil, []byte("short"), []byte("0123456789012345678901234567890123456789")} {
		if _, err := NewCipher(key); err == nil {
			t.Fatalf("NewCipher(len=%d) should fail", len(key))
		}
	}
}

func TestNewCipherFromEnv(t *testing.T) {
	t.Setenv(testEnvChannelKey, "0123456789abcdef0123456789abcdef")
	if _, err := NewCipherFromEnv(testEnvChannelKey); err != nil {
		t.Fatalf("NewCipherFromEnv: %v", err)
	}

	t.Setenv(testEnvChannelKey, "")
	if _, err := NewCipherFromEnv(testEnvChannelKey); err == nil {
		t.Fatal("missing env key should fail, not fall back to plaintext")
	}

	t.Setenv(testEnvChannelKey, "too-short")
	if _, err := NewCipherFromEnv(testEnvChannelKey); err == nil {
		t.Fatal("invalid env key length should fail")
	}
}

func TestGenerateGatewayKeyUniqueAndHashed(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 64; i++ {
		plain, err := GenerateGatewayKey("sk-")
		if err != nil {
			t.Fatalf("GenerateGatewayKey: %v", err)
		}
		if !strings.HasPrefix(plain, "sk-") {
			t.Fatalf("key %q missing prefix", plain)
		}
		if len(plain) <= len("sk-") {
			t.Fatalf("key %q has no random part", plain)
		}
		if seen[plain] {
			t.Fatalf("duplicate gateway key generated: %q", plain)
		}
		seen[plain] = true

		hash := HashKey(plain)
		if hash == plain || strings.Contains(hash, plain) {
			t.Fatal("hash must not contain the plaintext key")
		}
		if len(hash) != 64 {
			t.Fatalf("hash length = %d, want 64 hex chars", len(hash))
		}
		if !EqualHashes(hash, HashKey(plain)) {
			t.Fatal("hash should be stable for the same key")
		}
	}
}

func TestHashKeyNormalizesWhitespace(t *testing.T) {
	if HashKey("sk-abc") != HashKey("  sk-abc  ") {
		t.Fatal("HashKey should trim surrounding whitespace")
	}
}

func TestEqualHashes(t *testing.T) {
	h := HashKey("sk-abc")
	if !EqualHashes(h, h) {
		t.Fatal("identical hashes should match")
	}
	if EqualHashes(h, HashKey("sk-other")) {
		t.Fatal("different hashes should not match")
	}
}

func TestHashPasswordVerifyRoundTrip(t *testing.T) {
	hash, err := HashPassword("correct horse battery staple", MinPasswordCost)
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	if strings.Contains(hash, "correct horse battery staple") {
		t.Fatal("hash must not contain the plaintext password")
	}
	if !VerifyPassword(hash, "correct horse battery staple") {
		t.Fatal("correct password should verify")
	}
	if VerifyPassword(hash, "wrong password") {
		t.Fatal("wrong password must not verify")
	}
	if VerifyPassword("", "correct horse battery staple") {
		t.Fatal("empty hash must not verify")
	}
	if VerifyPassword("not-a-bcrypt-hash", "correct horse battery staple") {
		t.Fatal("malformed hash must not verify")
	}
}

func TestHashPasswordIsSalted(t *testing.T) {
	first, err := HashPassword("same-password", MinPasswordCost)
	if err != nil {
		t.Fatal(err)
	}
	second, err := HashPassword("same-password", MinPasswordCost)
	if err != nil {
		t.Fatal(err)
	}
	if first == second {
		t.Fatal("bcrypt must salt, producing different hashes for equal passwords")
	}
}

func TestHashPasswordCostDefaultsAndBounds(t *testing.T) {
	hash, err := HashPassword("password", 0)
	if err != nil {
		t.Fatalf("cost 0 should use default: %v", err)
	}
	if !VerifyPassword(hash, "password") {
		t.Fatal("default-cost hash should verify")
	}

	if _, err := HashPassword("password", MaxPasswordCost+1); err == nil {
		t.Fatal("cost above MaxPasswordCost should fail")
	}
	if _, err := HashPassword("password", MinPasswordCost-1); err == nil {
		t.Fatal("positive cost below MinPasswordCost should fail")
	}
}

func TestGenerateSessionTokenUniqueAndHashed(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 64; i++ {
		token, err := GenerateSessionToken()
		if err != nil {
			t.Fatalf("GenerateSessionToken: %v", err)
		}
		if len(token) != 64 {
			t.Fatalf("token length = %d, want 64 hex chars", len(token))
		}
		if seen[token] {
			t.Fatalf("duplicate session token generated: %q", token)
		}
		seen[token] = true

		hash := HashSessionToken(token)
		if hash == token || strings.Contains(hash, token) {
			t.Fatal("session token hash must not contain the plaintext token")
		}
		if len(hash) != 64 {
			t.Fatalf("session token hash length = %d, want 64 hex chars", len(hash))
		}
		if hash != HashSessionToken(token) {
			t.Fatal("session token hash should be stable for the same token")
		}
	}
}
