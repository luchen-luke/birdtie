package content

import (
	"context"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	mp "github.com/birdtie/birdtie/apps/api/internal/momentphoto"
	"time"
)

const PublicMomentPhotoSchemaVersion = "public-moment-v3"

// This distinct opt-in wire never adds image fields to the original v1/v2.
type PublicMomentWithPhoto struct {
	PublicMomentWithAuthor
	ObservedAt time.Time    `json:"observedAt"`
	ValidUntil time.Time    `json:"validUntil"`
	Photo      *mp.Metadata `json:"photo"`
}

func ValidatePublicMomentWithPhoto(v PublicMomentWithPhoto, id string) error {
	if v.SchemaVersion != PublicMomentPhotoSchemaVersion || v.ObservedAt.IsZero() || !v.ValidUntil.After(v.ObservedAt) || v.ValidUntil.Sub(v.ObservedAt) > time.Minute {
		return ErrInvalid
	}
	x := v.PublicMomentWithAuthor
	x.SchemaVersion = PublicMomentAuthorSchemaVersion
	if ValidatePublicMomentWithAuthor(x, id) != nil {
		return ErrInvalid
	}
	if v.Photo != nil && (!v.Photo.Valid(v.ObservedAt) || v.Photo.MomentID != id || v.Photo.MomentRevision != v.Revision || v.Photo.ExpiresAt.After(v.ValidUntil)) {
		return ErrInvalid
	}
	return nil
}

// Receipt is a process-sealed native snapshot, never an HTTP or model DTO.
type PublicMomentPhotoReceipt struct {
	View                        PublicMomentWithPhoto
	IncludeAuthor               bool
	BaseProof, PhotoProof, Seal string
	Frame                       identity.HumanControlFrame
}

func (PublicMomentPhotoReceipt) MarshalJSON() ([]byte, error) { return nil, ErrInvalid }

type PublicMomentWithPhotoStore interface {
	CapturePublicMomentWithPhoto(context.Context, PublicMomentAccess, string, bool) (PublicMomentPhotoReceipt, error)
	FinalizePublicMomentWithPhoto(context.Context, PublicMomentAccess, PublicMomentPhotoReceipt) (PublicMomentWithPhoto, error)
}
