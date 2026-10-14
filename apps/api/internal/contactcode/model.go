// Package contactcode owns revocable public person selectors. A code grants no
// profile access, relationship, messaging permission or model consent.
package contactcode

import (
	"context"
	"errors"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	"regexp"
	"time"
)

const Lifetime = 24 * time.Hour

var ErrInvalid = errors.New("invalid contact code")
var ErrConflict = errors.New("contact code changed")
var ErrNotFound = errors.New("contact code unavailable")
var ErrUnavailable = errors.New("contact code service unavailable")
var idPattern = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

func ValidID(v string) bool {
	return idPattern.MatchString(v) && v != "00000000-0000-0000-0000-000000000000"
}

type View struct {
	Version   int64      `json:"version"`
	CodeID    *string    `json:"codeId"`
	ExpiresAt *time.Time `json:"expiresAt"`
}
type Input struct {
	OperationID     string `json:"operationId"`
	ExpectedVersion int64  `json:"expectedVersion"`
}
type Resolution struct {
	AccountID string `json:"accountId"`
	Proof     any    `json:"-"`
}

func ValidateInput(in Input) error {
	if !ValidID(in.OperationID) || in.ExpectedVersion < 0 || in.ExpectedVersion > 1<<52 {
		return ErrInvalid
	}
	return nil
}
func ValidView(v View) bool {
	return v.Version >= 0 && v.Version <= 1<<52 && ((v.CodeID == nil && v.ExpiresAt == nil) ||
		(v.CodeID != nil && ValidID(*v.CodeID) && v.ExpiresAt != nil && !v.ExpiresAt.IsZero()))
}

type Store interface {
	ReadOwnContactCode(context.Context, identity.ProfileAccess) (View, error)
	ChangeOwnContactCode(context.Context, identity.ProfileAccess, Input, bool) (View, error)
	ValidateOwnContactCode(context.Context, identity.ProfileAccess, View) error
	ResolveContactCode(context.Context, identity.ProfileAccess, string) (Resolution, error)
	ValidateContactResolution(context.Context, identity.ProfileAccess, string, Resolution) error
}
