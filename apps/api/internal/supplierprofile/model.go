// Package supplierprofile separates ordinary public disclosure from merchant
// management verification. None of these permissions authorizes Agent use.
package supplierprofile

import (
	"context"
	"encoding/json"
	"regexp"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/businessconsole"
	"github.com/birdtie/birdtie/apps/api/internal/foundation"
)

type Business struct {
	ID                 string                `json:"id"`
	Name               string                `json:"name"`
	VerificationStatus string                `json:"verificationStatus"`
	ProfileStatus      string                `json:"profileStatus"`
	Description        string                `json:"description"`
	OfficialLinks      []string              `json:"officialLinks"`
	ProfileVersion     int64                 `json:"profileVersion"`
	ReviewedAt         *time.Time            `json:"reviewedAt"`
	ValidUntil         *time.Time            `json:"validUntil"`
	UpcomingActivities []foundation.Activity `json:"upcomingActivities"`
}

// Permission is an owner-only management result, never embedded in Business.
type Permission struct {
	Version        int64      `json:"version"`
	ProfileVersion int64      `json:"profileVersion"`
	State          string     `json:"state"`
	ValidUntil     *time.Time `json:"validUntil"`
}
type PermissionInput struct {
	ExpectedVersion        int64  `json:"expectedVersion"`
	ExpectedProfileVersion int64  `json:"expectedProfileVersion"`
	Action                 string `json:"action"`
	ValidUntil             string `json:"validUntil"`
	SourceSnapshot         string `json:"sourceSnapshot"`
}

type PublicationPreview struct {
	ProfileVersion int64     `json:"profileVersion"`
	Name           string    `json:"name"`
	Description    string    `json:"description"`
	OfficialLinks  []string  `json:"officialLinks"`
	ReviewedAt     time.Time `json:"reviewedAt"`
	ValidUntil     time.Time `json:"validUntil"`
	SourceSnapshot string    `json:"sourceSnapshot"`
}
type PermissionView struct {
	Permission       Permission          `json:"permission"`
	Preview          *PublicationPreview `json:"preview"`
	DisclosureStatus string              `json:"disclosureStatus"`
}

var snapshotPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)

func DecodePermission(raw []byte) (PermissionInput, error) {
	var in PermissionInput
	normalized, e := businessconsole.StrictObject(raw, "expectedVersion", "expectedProfileVersion", "action", "validUntil", "sourceSnapshot")
	if e != nil || json.Unmarshal(normalized, &in) != nil {
		return in, businessconsole.ErrInvalid
	}
	return in, nil
}
func ValidatePermission(in PermissionInput, now time.Time) (*time.Time, error) {
	if in.ExpectedVersion < 0 || in.ExpectedVersion == int64(^uint64(0)>>1) {
		return nil, businessconsole.ErrInvalid
	}
	switch in.Action {
	case "publish":
		until, e := time.Parse(time.RFC3339Nano, in.ValidUntil)
		until = until.UTC().Truncate(time.Microsecond)
		if e != nil || !snapshotPattern.MatchString(in.SourceSnapshot) || in.ExpectedProfileVersion < 1 || until.Year() < 1 || until.Year() > 9999 || !until.After(now) || until.After(now.AddDate(1, 0, 0)) {
			return nil, businessconsole.ErrInvalid
		}
		return &until, nil
	case "revoke":
		if in.ExpectedVersion < 1 || in.ExpectedProfileVersion != 0 || in.ValidUntil != "" || in.SourceSnapshot != "" {
			return nil, businessconsole.ErrInvalid
		}
		return nil, nil
	default:
		return nil, businessconsole.ErrInvalid
	}
}

type Store interface {
	ReadPublicBusiness(context.Context, string, string) (Business, error)
	ReadBusinessPublicPermission(context.Context, businessconsole.Access) (PermissionView, error)
	ChangeBusinessPublicPermission(context.Context, businessconsole.Access, PermissionInput) (Permission, error)
}
