package protocol

import (
	"crypto/ed25519"
	"crypto/rand"
	"regexp"
	"strings"
	"testing"
	"time"
)

func TestSignAndKeyEncoding(t *testing.T) {
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	encodedPublic := EncodePublicKey(publicKey)
	decodedPublic, err := DecodePublicKey(encodedPublic)
	if err != nil {
		t.Fatal(err)
	}
	encodedPrivate := EncodePrivateKey(privateKey)
	decodedPrivate, err := DecodePrivateKey(encodedPrivate)
	if err != nil {
		t.Fatal(err)
	}
	canonical := Canonical("post", "/api/pages", 1234, "nonce", []byte("hello"))
	signature := ed25519.Sign(decodedPrivate, canonical)
	if !ed25519.Verify(decodedPublic, canonical, signature) {
		t.Fatal("signature did not verify")
	}
	if KeyID(publicKey) != KeyID(decodedPublic) {
		t.Fatal("key id changed after encoding")
	}
}

func TestNewUUIDv7(t *testing.T) {
	now := time.Date(2026, time.July, 14, 12, 30, 0, 0, time.UTC)
	id, err := NewUUIDv7(now)
	if err != nil {
		t.Fatal(err)
	}
	pattern := regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)
	if !pattern.MatchString(id) {
		t.Fatalf("not a UUIDv7: %s", id)
	}
	if !IsUUIDv7(id) {
		t.Fatalf("IsUUIDv7 rejected %s", id)
	}
	for _, invalid := range []string{"", strings.ToUpper(id), "019f620a-226d-6981-88d3-83da3b460b6c", id + "/extra"} {
		if IsUUIDv7(invalid) {
			t.Fatalf("IsUUIDv7 accepted %q", invalid)
		}
	}
}

func TestCanonicalBindsEveryField(t *testing.T) {
	base := string(Canonical("POST", "/api/pages", 1234, "nonce", []byte("hello")))
	cases := [][]byte{
		Canonical("PUT", "/api/pages", 1234, "nonce", []byte("hello")),
		Canonical("POST", "/api/keys", 1234, "nonce", []byte("hello")),
		Canonical("POST", "/api/pages", 1235, "nonce", []byte("hello")),
		Canonical("POST", "/api/pages", 1234, "other", []byte("hello")),
		Canonical("POST", "/api/pages", 1234, "nonce", []byte("changed")),
	}
	for _, candidate := range cases {
		if string(candidate) == base {
			t.Fatal("canonical request did not bind a field")
		}
	}
}
