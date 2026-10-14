// Package placephoto owns independently reviewed retained Place photos.
// A human thumbnail is not a license for model or Agent image processing.
package placephoto

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	ec "github.com/birdtie/birdtie/apps/api/internal/entitycover"
	av "github.com/birdtie/birdtie/apps/api/internal/profileavatar"
)

const MaxVersion = ec.MaxVersion

var ErrInvalid = errors.New("invalid place photo")
var ErrChanged = errors.New("place photo changed")
var ErrForbidden = errors.New("place photo forbidden")
var ErrNotFound = errors.New("place photo not found")
var ErrUnavailable = errors.New("place photo unavailable")

type Access = av.Access
type Rights = ec.Rights
type Derivative = ec.PreviewDerivative

func ValidID(id string) bool { return av.ValidID(id) }
func Path(id string, v int64) string {
	return "/v1/places/" + id + "/photos/" + strconv.FormatInt(v, 10) + "/content"
}

type Metadata struct {
	EntityType    string    `json:"entityType"`
	EntityID      string    `json:"entityId"`
	Version       int64     `json:"version"`
	ThumbnailPath string    `json:"thumbnailPath"`
	Width         int       `json:"width"`
	Height        int       `json:"height"`
	ByteCount     int       `json:"byteCount"`
	SHA256        string    `json:"sha256"`
	ExpiresAt     time.Time `json:"expiresAt"`
	Attribution   string    `json:"attribution"`
	ResponseProof string    `json:"-"`
}

func (m Metadata) Valid(now time.Time) bool {
	h, e := hex.DecodeString(m.SHA256)
	if e != nil || len(h) != 32 || m.SHA256 != strings.ToLower(m.SHA256) || !utf8.ValidString(m.Attribution) || m.Attribution != strings.TrimSpace(m.Attribution) || len(m.Attribution) > 320 {
		return false
	}
	for _, r := range m.Attribution {
		if unicode.IsControl(r) {
			return false
		}
	}
	return m.EntityType == "place" && ValidID(m.EntityID) && m.Version > 0 && m.Version <= MaxVersion && m.ThumbnailPath == Path(m.EntityID, m.Version) && m.Width == ec.Width && m.Height == ec.Height && m.ByteCount > 0 && m.ByteCount <= ec.MaxThumbnailBytes && m.ExpiresAt.After(now) && !m.ExpiresAt.After(now.Add(61*time.Second))
}
func PreserveLease(old, current *Metadata, now time.Time) bool {
	if old == nil || current == nil {
		return old == nil && current == nil
	}
	if !old.Valid(now) || !current.Valid(now) || current.ExpiresAt.Before(old.ExpiresAt) {
		return false
	}
	c := *current
	c.ExpiresAt = old.ExpiresAt
	c.ResponseProof = old.ResponseProof
	if c != *old {
		return false
	}
	*current = c
	return true
}

type State struct {
	Version     int64     `json:"version"`
	PlacePhoto  *Metadata `json:"placePhoto"`
	SourceProof string    `json:"-"`
}
type Candidate struct {
	ID              string     `json:"id"`
	Version         int64      `json:"version"`
	Status          string     `json:"status"`
	ExpectedVersion int64      `json:"expectedVersion"`
	Rights          Rights     `json:"rights"`
	Thumbnail       *Thumbnail `json:"thumbnail"`
	CanApprove      bool       `json:"canApprove"`
	CanReject       bool       `json:"canReject"`
	CanWithdraw     bool       `json:"canWithdraw"`
	SubmittedBy     string     `json:"-"`
	PlaceID         string     `json:"-"`
	Bytes           []byte     `json:"-"`
	SHA256          string     `json:"-"`
	SourceProof     string     `json:"-"`
}
type Thumbnail struct {
	ContentType string `json:"contentType"`
	Width       int    `json:"width"`
	Height      int    `json:"height"`
	ByteCount   int    `json:"byteCount"`
	SHA256      string `json:"sha256"`
}
type Editorial struct {
	PlaceID       string      `json:"placeId"`
	CityID        string      `json:"cityId"`
	Version       int64       `json:"version"`
	CanContribute bool        `json:"canContribute"`
	CanReview     bool        `json:"canReview"`
	Candidates    []Candidate `json:"candidates"`
	ObservedAt    time.Time   `json:"observedAt"`
	ExpiresAt     time.Time   `json:"expiresAt"`
	SourceProof   string      `json:"-"`
}
type Input struct {
	OperationID      string
	ExpectedVersion  int64
	Action           string
	ContentType      string
	Content          []byte
	Rights           *Rights
	CandidateID      string
	CandidateVersion int64
	RightsConfirmed  bool
	PhotoConfirmed   bool
	Approval         string
}

func Normalize(in Input, now time.Time) (ec.Derivative, error) {
	if !ValidID(in.OperationID) || in.ExpectedVersion < 0 || in.ExpectedVersion > MaxVersion || (in.Action == "SUBMIT" || in.Action == "APPROVE") && in.ExpectedVersion >= MaxVersion {
		return ec.Derivative{}, ErrInvalid
	}
	if in.Action == "SUBMIT" {
		if in.CandidateID != "" || in.CandidateVersion != 0 || in.RightsConfirmed || in.PhotoConfirmed || in.Rights == nil || !in.Rights.Valid(now) {
			return ec.Derivative{}, ErrInvalid
		}
		d, e := ec.Prepare(in.Content, in.ContentType)
		if e != nil {
			return d, ErrInvalid
		}
		return d, nil
	}
	if in.Action != "APPROVE" && in.Action != "REJECT" && in.Action != "WITHDRAW" {
		return ec.Derivative{}, ErrInvalid
	}
	if !ValidID(in.CandidateID) || in.CandidateVersion < 1 || in.CandidateVersion > MaxVersion || in.Action == "APPROVE" && in.CandidateVersion >= MaxVersion || in.ContentType != "" || len(in.Content) != 0 || in.Rights != nil || (in.Action == "APPROVE" && (!in.RightsConfirmed || !in.PhotoConfirmed)) || (in.Action != "APPROVE" && (in.RightsConfirmed || in.PhotoConfirmed)) {
		return ec.Derivative{}, ErrInvalid
	}
	return ec.Derivative{}, nil
}
func Digest(id string, in Input, image, rights string) string {
	raw, _ := json.Marshal([]any{"PLACE_PHOTO_138", id, in.OperationID, in.ExpectedVersion, in.Action, in.CandidateID, in.CandidateVersion, in.RightsConfirmed, in.PhotoConfirmed, image, rights})
	h := sha256.Sum256(raw)
	return hex.EncodeToString(h[:])
}

type Approval struct {
	PlaceID           string      `json:"placeId"`
	OperationID       string      `json:"operationId"`
	ExpectedVersion   int64       `json:"expectedVersion"`
	Action            string      `json:"action"`
	CandidateID       string      `json:"candidateId"`
	CandidateVersion  int64       `json:"candidateVersion"`
	InputSHA256       string      `json:"inputSha256"`
	RightsSHA256      string      `json:"rightsSha256"`
	ExpiresAt         time.Time   `json:"expiresAt"`
	Token             string      `json:"token"`
	PreviewDerivative *Derivative `json:"previewDerivative,omitempty"`
}
type Receipt struct {
	PlaceID          string `json:"placeId"`
	OperationID      string `json:"operationId"`
	Action           string `json:"action"`
	CandidateID      string `json:"candidateId,omitempty"`
	CandidateVersion int64  `json:"candidateVersion"`
	AppliedVersion   int64  `json:"appliedVersion"`
	Status           string `json:"status"`
	ResponseProof    string `json:"-"`
}

func (r Receipt) Valid(id, op string) bool {
	if !ValidID(id) || !ValidID(op) || r.PlaceID != id || r.OperationID != op || r.AppliedVersion < 0 || r.AppliedVersion > MaxVersion {
		return false
	}
	if r.Status == "STOPPED" {
		return r.Action == "STOP" && r.AppliedVersion == 0 && r.CandidateVersion == 0 && r.CandidateID == ""
	}
	return r.Status == "SUCCEEDED" && ValidID(r.CandidateID) && r.CandidateVersion > 0 && r.CandidateVersion <= MaxVersion && (r.Action == "SUBMIT" || r.Action == "APPROVE" || r.Action == "REJECT" || r.Action == "WITHDRAW") && (r.Action != "APPROVE" || r.AppliedVersion > 0)
}

type Store interface {
	ReadPlacePhoto(context.Context, Access, string) (State, error)
	ReadPlacePhotoEditorial(context.Context, Access, string) (Editorial, error)
	PreviewPlacePhoto(context.Context, Access, string, Input) (Approval, error)
	ChangePlacePhoto(context.Context, Access, string, Input) (Receipt, error)
	ReadPlacePhotoOperation(context.Context, Access, string, string) (Receipt, error)
	StopPlacePhotoOperation(context.Context, Access, string, string, int64) (Receipt, error)
	ReadPlacePhotoContent(context.Context, Access, string, int64) (Metadata, []byte, error)
	ValidatePlacePhotoProjection(context.Context, Access, string, *Metadata, string) error
	ValidatePlacePhotoPreview(context.Context, Access, string, Input, Approval) error
	ValidatePlacePhotoEditorial(context.Context, Access, string, Editorial) error
}
type SessionStore interface {
	CapturePlacePhotoSession(context.Context, Access) (string, error)
	ValidatePlacePhotoSession(context.Context, Access, string) error
}
