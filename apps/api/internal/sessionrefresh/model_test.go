package sessionrefresh

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/identity"
)

const (
	testOperation = "11b69051-2c73-494a-9a7c-3cc01aca6e2a"
	testFamily    = "f42b806e-0f48-4158-8d67-3351f128e31e"
)

func TestTokenCanonicalAndDigest(t *testing.T) {
	token, digest, err := NewToken()
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := ParseToken(token)
	if err != nil || parsed != digest || digest != sha256.Sum256([]byte(token)) {
		t.Fatal("generated token did not match its canonical digest")
	}
	second, _, err := NewToken()
	if err != nil || second == token {
		t.Fatal("tokens did not have independent randomness")
	}
	// The final base64 character has two unused bits, which must remain zero.
	canonicalZero := tokenPrefix + base64.RawURLEncoding.EncodeToString(make([]byte, 32))
	noncanonical := canonicalZero[:len(canonicalZero)-1] + "B"
	for _, invalid := range []string{"", " " + token, token + "\n", token + "=", strings.Replace(token, tokenPrefix, "bts1_", 1), token[:len(token)-1], noncanonical} {
		if got, err := ParseToken(invalid); !errors.Is(err, identity.ErrUnauthorized) || got != ([32]byte{}) {
			t.Error("malformed token was not rejected with an empty digest")
		}
	}
}

func TestOperationIDCanonicalAndNonzero(t *testing.T) {
	for _, valid := range []string{testOperation, "10000000-0000-0000-0000-000000000000", "ffffffff-ffff-ffff-ffff-ffffffffffff"} {
		if got, err := NormalizeOperationID(valid); err != nil || got != valid {
			t.Errorf("canonical UUID was rejected: %q", valid)
		}
	}
	for _, invalid := range []string{"", strings.ToUpper(testOperation), " " + testOperation, "{" + testOperation + "}", strings.ReplaceAll(testOperation, "-", ""), "00000000-0000-0000-0000-000000000000", "g1b69051-2c73-494a-9a7c-3cc01aca6e2a"} {
		if got, err := NormalizeOperationID(invalid); !errors.Is(err, ErrInvalidRequest) || got != "" {
			t.Errorf("noncanonical UUID was accepted: %q", invalid)
		}
	}
}

func TestReceiptRoundTripAuthenticationAndNonce(t *testing.T) {
	old, _, err := NewToken()
	if err != nil {
		t.Fatal(err)
	}
	next, _, err := NewToken()
	if err != nil {
		t.Fatal(err)
	}
	want := Response{AccessToken: "access-secret", TokenType: TokenType, ExpiresInSeconds: ExpiresInSeconds, IdleTimeoutSeconds: IdleTimeoutSeconds, RefreshToken: next, RefreshExpiresAt: time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC), AccountID: testFamily, AuthMethod: AuthMethod}
	sealed, err := SealReceipt(old, testOperation, testFamily, want)
	if err != nil {
		t.Fatal(err)
	}
	got, err := OpenReceipt(old, testOperation, testFamily, sealed)
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatal("authenticated receipt did not round trip")
	}
	second, err := SealReceipt(old, testOperation, testFamily, want)
	if err != nil || bytes.Equal(sealed, second) || bytes.Equal(sealed[len(receiptPrefix):len(receiptPrefix)+12], second[len(receiptPrefix):len(receiptPrefix)+12]) {
		t.Fatal("receipt nonce was reused")
	}
	if bytes.Contains(sealed, []byte(next)) || bytes.Contains(sealed, []byte(want.AccessToken)) {
		t.Fatal("receipt exposed plaintext credentials")
	}
	tampered := bytes.Clone(sealed)
	tampered[len(tampered)-1] ^= 1
	for _, tc := range []struct {
		name, token, operation, family string
		data                           []byte
	}{
		{"wrong token", next, testOperation, testFamily, sealed},
		{"wrong operation", old, testFamily, testFamily, sealed},
		{"wrong family", old, testOperation, testOperation, sealed},
		{"malformed operation", old, "bad", testFamily, sealed},
		{"malformed family", old, testOperation, "bad", sealed},
		{"tampered", old, testOperation, testFamily, tampered},
		{"truncated", old, testOperation, testFamily, sealed[:len(sealed)-1]},
		{"short", old, testOperation, testFamily, []byte(receiptPrefix)},
		{"oversized", old, testOperation, testFamily, bytes.Repeat([]byte{'a'}, MaxReceiptBytes+1)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got, err := OpenReceipt(tc.token, tc.operation, tc.family, tc.data); !errors.Is(err, ErrUnauthorized) || got != (Response{}) {
				t.Fatal("unauthenticated receipt was accepted")
			}
		})
	}
	want.AccessToken = strings.Repeat("x", MaxReceiptBytes)
	if _, err := SealReceipt(old, testOperation, testFamily, want); !errors.Is(err, ErrInvalidRequest) {
		t.Fatal("oversized receipt was accepted")
	}
}

func TestResponseWireNames(t *testing.T) {
	raw, err := json.Marshal(Response{})
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"accessToken", "tokenType", "expiresInSeconds", "idleTimeoutSeconds", "refreshToken", "refreshExpiresAt", "accountId", "authMethod"} {
		if _, exists := fields[name]; !exists {
			t.Errorf("missing response field %q", name)
		}
	}
	if len(fields) != 8 {
		t.Fatal("unexpected response wire fields")
	}
}
