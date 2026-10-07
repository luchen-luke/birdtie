package venue

import (
	"context"
	"errors"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	"time"
)

var (
	ErrForbidden   = errors.New("venue editor access denied")
	ErrConflict    = errors.New("venue review conflict")
	ErrNotFound    = errors.New("venue not found")
	ErrChanged     = errors.New("venue source changed")
	ErrUnavailable = errors.New("venue current source unavailable")
)

type Facts struct {
	Capacity               *int     `json:"capacity,omitempty"`
	ReservationSupport     string   `json:"reservationSupport"`
	ReservationURL         *string  `json:"reservationUrl,omitempty"`
	Suitability            []string `json:"suitability"`
	Amenities              []string `json:"amenities"`
	OperatorOrganizationID *string  `json:"operatorOrganizationId,omitempty"`
}

type SubmitInput struct {
	Facts
	SourceURL  string    `json:"sourceUrl"`
	RightsNote string    `json:"rightsNote"`
	ExpiresAt  time.Time `json:"expiresAt"`
}

type Candidate struct {
	ID          string `json:"id"`
	PlaceID     string `json:"placeId"`
	CityID      string `json:"cityId"`
	SubmittedBy string `json:"submittedBy"`
	Facts
	SourceURL  string     `json:"sourceUrl"`
	RightsNote string     `json:"rightsNote"`
	ExpiresAt  time.Time  `json:"expiresAt"`
	Status     string     `json:"status"`
	ReviewedBy *string    `json:"reviewedBy,omitempty"`
	ReviewedAt *time.Time `json:"reviewedAt,omitempty"`
	ReviewNote *string    `json:"reviewNote,omitempty"`
	CreatedAt  time.Time  `json:"createdAt"`
}

type ReviewInput struct {
	Decision string `json:"decision"`
	Note     string `json:"note"`
}

type Public struct {
	PlaceID string `json:"placeId"`
	CityID  string `json:"cityId"`
	Facts
	OperatorOrganizationName string            `json:"operatorOrganizationName,omitempty"`
	Businesses               []BusinessSummary `json:"businesses"`
	SourceURL                string            `json:"sourceUrl"`
	ReviewedAt               time.Time         `json:"reviewedAt"`
	ExpiresAt                time.Time         `json:"expiresAt"`
	BookingSourceVersion     string            `json:"bookingSourceVersion,omitempty"`
	BookingSourceRevision    string            `json:"bookingSourceRevision,omitempty"`
	BookingObservedAt        time.Time         `json:"bookingObservedAt,omitempty"`
	BookingValidUntil        time.Time         `json:"bookingValidUntil,omitempty"`
}

// An absent actor is a genuinely anonymous public read. A supplied invalid
// session is never downgraded to anonymous. This is not a machine grant.
type PublicAccess struct {
	Actor         identity.Actor
	SessionDigest [32]byte
}
type CurrentReceipt struct {
	View  Public
	Proof string
	Seal  string
}
type CurrentStore interface {
	ReadCurrentPublicVenue(context.Context, PublicAccess, string) (CurrentReceipt, error)
	RevalidateCurrentPublicVenue(context.Context, PublicAccess, string, CurrentReceipt) error
}

type BusinessSummary struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type Store interface {
	SubmitVenueCandidate(context.Context, string, string, string, SubmitInput) (Candidate, error)
	ListVenueCandidates(context.Context, string, string) ([]Candidate, error)
	ReviewVenueCandidate(context.Context, string, string, ReviewInput) (Candidate, error)
	GetPublicVenue(context.Context, string) (Public, error)
}
