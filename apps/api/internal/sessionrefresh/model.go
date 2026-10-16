// Package sessionrefresh defines persistent sessions and their bounded,
// authenticated refresh retry receipts. Storage enforces expiry and revocation.
package sessionrefresh

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hkdf"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/identity"
)

const (
	TokenType          = "Bearer"
	ExpiresInSeconds   = 28800
	IdleTimeoutSeconds = 1800
	AuthMethod         = "dev_phone"
	MaxReceiptBytes    = 4096

	tokenPrefix   = "btr1_"
	receiptPrefix = "btrr1"
	receiptDomain = "birdtie:session-refresh:receipt:v1"
)

var (
	ErrUnauthorized   = identity.ErrUnauthorized
	ErrInvalidRequest = errors.New("invalid session refresh request")
	ErrConflict       = errors.New("session refresh conflict")
)

type Response struct {
	AccessToken        string    `json:"accessToken"`
	TokenType          string    `json:"tokenType"`
	ExpiresInSeconds   int       `json:"expiresInSeconds"`
	IdleTimeoutSeconds int       `json:"idleTimeoutSeconds"`
	RefreshToken       string    `json:"refreshToken"`
	RefreshExpiresAt   time.Time `json:"refreshExpiresAt"`
	AccountID          string    `json:"accountId"`
	AuthMethod         string    `json:"authMethod"`
}

type Store interface {
	RefreshSession(ctx context.Context, oldRefresh string, operationID string) (Response, error)
	RevokeRefreshFamily(ctx context.Context, refresh string) error
}

// DevPhoneStore supplements the existing development phone challenge store.
// Callers generate the two credentials only after validating the request.
type DevPhoneStore interface {
	VerifyDevPhoneChallengePersistent(ctx context.Context, phoneDigest [32]byte, codeValid bool, sessionToken string, refreshToken string) (Response, error)
}

// NewToken returns a canonical 256-bit refresh credential and its storage digest.
func NewToken() (string, [32]byte, error) {
	var raw [32]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", [32]byte{}, err
	}
	token := tokenPrefix + base64.RawURLEncoding.EncodeToString(raw[:])
	return token, sha256.Sum256([]byte(token)), nil
}

// ParseToken rejects whitespace, padding and noncanonical encodings. Only the
// digest, never the bearer credential, belongs in persistent lookup columns.
func ParseToken(token string) ([32]byte, error) {
	if len(token) != len(tokenPrefix)+43 || !strings.HasPrefix(token, tokenPrefix) {
		return [32]byte{}, ErrUnauthorized
	}
	encoded := token[len(tokenPrefix):]
	raw, err := base64.RawURLEncoding.Strict().DecodeString(encoded)
	if err != nil || len(raw) != 32 || base64.RawURLEncoding.EncodeToString(raw) != encoded {
		return [32]byte{}, ErrUnauthorized
	}
	return sha256.Sum256([]byte(token)), nil
}

// NormalizeOperationID accepts only a nonzero, lowercase canonical UUID. It
// intentionally does not impose a UUID version or rewrite client input.
func NormalizeOperationID(value string) (string, error) {
	if len(value) != 36 {
		return "", ErrInvalidRequest
	}
	nonzero := false
	for i := range len(value) {
		if i == 8 || i == 13 || i == 18 || i == 23 {
			if value[i] != '-' {
				return "", ErrInvalidRequest
			}
			continue
		}
		c := value[i]
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
			return "", ErrInvalidRequest
		}
		nonzero = nonzero || c != '0'
	}
	if !nonzero {
		return "", ErrInvalidRequest
	}
	return value, nil
}

func receiptCipher(oldRefresh, operationID, familyID string) (cipher.AEAD, []byte, error) {
	digest, err := ParseToken(oldRefresh)
	if err != nil {
		return nil, nil, err
	}
	if _, err = NormalizeOperationID(operationID); err != nil {
		return nil, nil, err
	}
	if _, err = NormalizeOperationID(familyID); err != nil {
		return nil, nil, err
	}
	key, err := hkdf.Key(sha256.New, []byte(oldRefresh), []byte(receiptDomain), receiptDomain+":aes-256-gcm", 32)
	if err != nil {
		return nil, nil, err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, nil, err
	}
	aad := []byte(receiptDomain + "\x00" + operationID + "\x00" + familyID + "\x00" + hex.EncodeToString(digest[:]))
	return aead, aad, nil
}

// SealReceipt binds a retry response to the previous credential, operation and
// family. The caller must separately enforce its short retry window in storage.
func SealReceipt(oldRefresh, operationID, familyID string, response Response) ([]byte, error) {
	aead, aad, err := receiptCipher(oldRefresh, operationID, familyID)
	if err != nil {
		return nil, err
	}
	plain, err := json.Marshal(response)
	if err != nil {
		return nil, ErrInvalidRequest
	}
	if len(receiptPrefix)+aead.NonceSize()+len(plain)+aead.Overhead() > MaxReceiptBytes {
		return nil, ErrInvalidRequest
	}
	envelope := make([]byte, len(receiptPrefix)+aead.NonceSize())
	copy(envelope, receiptPrefix)
	nonce := envelope[len(receiptPrefix):]
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	return aead.Seal(envelope, nonce, plain, aad), nil
}

// OpenReceipt returns one generic authorization error for malformed envelopes
// and authentication failures, without exposing which binding did not match.
func OpenReceipt(oldRefresh, operationID, familyID string, sealed []byte) (Response, error) {
	var response Response
	if len(sealed) > MaxReceiptBytes || !strings.HasPrefix(string(sealed), receiptPrefix) {
		return response, ErrUnauthorized
	}
	aead, aad, err := receiptCipher(oldRefresh, operationID, familyID)
	if err != nil || len(sealed) < len(receiptPrefix)+aead.NonceSize()+aead.Overhead() {
		return response, ErrUnauthorized
	}
	start := len(receiptPrefix)
	nonce := sealed[start : start+aead.NonceSize()]
	plain, err := aead.Open(nil, nonce, sealed[start+aead.NonceSize():], aad)
	if err != nil || json.Unmarshal(plain, &response) != nil {
		return Response{}, ErrUnauthorized
	}
	return response, nil
}
