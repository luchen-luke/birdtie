package devauth

import (
	"context"
	"crypto/sha256"
	"errors"
	"strings"
)

var (
	ErrInvalid     = errors.New("invalid development code or challenge")
	ErrRateLimited = errors.New("development code rate limited")
	ErrDisabled    = errors.New("development phone auth disabled")
)

const Code = "123456"

type Store interface {
	RequestDevPhoneChallenge(context.Context, [32]byte) error
	VerifyDevPhoneChallenge(context.Context, [32]byte, bool, [32]byte) error
}

// NormalizePhone accepts a strict E.164 number or a mainland mobile shorthand.
// The development identity never becomes a verified production phone identity.
func NormalizePhone(raw string) (string, bool) {
	phone := strings.ReplaceAll(strings.ReplaceAll(strings.TrimSpace(raw), " ", ""), "-", "")
	if len(phone) == 11 && phone[0] == '1' && digits(phone) {
		return "+86" + phone, true
	}
	if len(phone) == 13 && strings.HasPrefix(phone, "86") && digits(phone) {
		return "+" + phone, true
	}
	if !strings.HasPrefix(phone, "+") || len(phone) < 9 || len(phone) > 16 ||
		phone[1] == '0' || !digits(phone[1:]) {
		return "", false
	}
	return phone, true
}

func digits(value string) bool {
	for i := 0; i < len(value); i++ {
		if value[i] < '0' || value[i] > '9' {
			return false
		}
	}
	return value != ""
}

func PhoneDigest(phone string) [32]byte {
	return sha256.Sum256([]byte("birdtie-dev-phone:" + phone))
}
