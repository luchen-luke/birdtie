package devauth

import (
	"context"
	"crypto/sha256"
	"net/mail"
	"strings"
)

// EmailStore is a separate identity realm within the same protected fixed-code
// development channel. An email is never encoded as a phone identity.
type EmailStore interface {
	RequestDevEmailChallenge(context.Context, [32]byte) error
	VerifyDevEmailChallenge(context.Context, [32]byte, bool, [32]byte) error
}

func NormalizeEmail(raw string) (string, bool) {
	value := strings.ToLower(strings.TrimSpace(raw))
	if len(value) < 3 || len(value) > 254 || strings.Count(value, "@") != 1 || strings.ContainsAny(value, "<>[]():;\\\"") {
		return "", false
	}
	for i := range value {
		if value[i] < 33 || value[i] > 126 {
			return "", false
		}
	}
	a, e := mail.ParseAddress(value)
	if e != nil || a.Name != "" || a.Address != value {
		return "", false
	}
	parts := strings.Split(value, "@")
	if len(parts[0]) > 64 || !strings.Contains(parts[1], ".") {
		return "", false
	}
	return value, true
}
func EmailDigest(email string) [32]byte { return sha256.Sum256([]byte("birdtie-dev-email:" + email)) }
