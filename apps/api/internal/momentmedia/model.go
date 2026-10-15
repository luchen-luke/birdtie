// Package momentmedia owns explicitly approved ordered static image derivatives.
// Structural validation is not antivirus, privacy detection, or publication.
package momentmedia

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"strconv"
	"time"
	"unicode/utf8"

	ec "github.com/birdtie/birdtie/apps/api/internal/entitycover"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	av "github.com/birdtie/birdtie/apps/api/internal/profileavatar"
)

const MaxImages = 9
const MaxInputBytes = 5 * 1024 * 1024
const MaxDisplayBytes = 1024 * 1024
const MaxThumbnailBytes = 256 * 1024
const MaxVersion int64 = 8999999999999999
const ValidationKind = "STATIC_IMAGE_DECODE_REENCODE_V1"
const StageTTL = 10 * time.Minute

var ErrInvalid = identity.ErrControlInvalid
var ErrChanged = identity.ErrControlChanged
var ErrUnavailable = identity.ErrControlUnavailable
var ErrForbidden = errors.New("moment media owner required")
var ErrNotFound = errors.New("moment media not found")

type Access = av.Access
type Rights = ec.Rights
type StageSelector struct {
	MediaID string `json:"mediaId"`
	Version int64  `json:"mediaVersion"`
}
type AttachmentInput struct {
	MediaID      string `json:"mediaId"`
	MediaVersion int64  `json:"mediaVersion"`
	Rights       Rights `json:"rights"`
}
type StageInput struct {
	OperationID string `json:"operationId"`
	LocalScope  string `json:"localScope"`
	ContentType string `json:"contentType"`
	Content     []byte `json:"-"`
}
type Stage struct {
	SchemaVersion    string         `json:"schemaVersion,omitempty"`
	Kind             string         `json:"kind,omitempty"`
	Purpose          string         `json:"purpose,omitempty"`
	Video            *VideoContract `json:"video,omitempty"`
	MediaID          string         `json:"mediaId"`
	Version          int64          `json:"mediaVersion"`
	LocalScope       string         `json:"localScope"`
	DisplaySHA       string         `json:"displaySha256"`
	DisplayBytes     int            `json:"displayBytes"`
	Width            int            `json:"width"`
	Height           int            `json:"height"`
	ThumbnailSHA     string         `json:"thumbnailSha256"`
	ThumbnailBytes   int            `json:"thumbnailBytes"`
	ValidationKind   string         `json:"validationKind"`
	ValidationStatus string         `json:"validationStatus"`
	AntivirusScan    string         `json:"antivirusScan"`
	PrivacyRisk      string         `json:"privacyRisk"`
	PreparedAt       time.Time      `json:"preparedAt"`
	RetainUntil      time.Time      `json:"retainUntil"`
}
type StageReceipt struct {
	OperationID   string    `json:"operationId"`
	Status        string    `json:"status"`
	PayloadDigest string    `json:"payloadDigest"`
	Material      *Stage    `json:"material"`
	CommittedAt   time.Time `json:"committedAt"`
}
type Attachment struct {
	SchemaVersion    string         `json:"schemaVersion,omitempty"`
	Kind             string         `json:"kind,omitempty"`
	Purpose          string         `json:"purpose,omitempty"`
	Video            *VideoContract `json:"video,omitempty"`
	MediaID          string         `json:"mediaId"`
	MediaVersion     int64          `json:"mediaVersion"`
	Position         int            `json:"position"`
	Width            int            `json:"width"`
	Height           int            `json:"height"`
	DisplayBytes     int            `json:"displayBytes"`
	DisplaySHA       string         `json:"displaySha256"`
	ThumbnailBytes   int            `json:"thumbnailBytes"`
	ThumbnailSHA     string         `json:"thumbnailSha256"`
	DisplayPath      string         `json:"displayPath"`
	ThumbnailPath    string         `json:"thumbnailPath"`
	RightsDigest     string         `json:"rightsDigest"`
	Attribution      string         `json:"attribution"`
	ValidationKind   string         `json:"validationKind"`
	ValidationStatus string         `json:"validationStatus"`
	AntivirusScan    string         `json:"antivirusScan"`
	PrivacyRisk      string         `json:"privacyRisk"`
	ExpiresAt        time.Time      `json:"expiresAt"`
}
type Set struct {
	SchemaVersion  string                     `json:"schemaVersion"`
	MomentID       string                     `json:"momentId"`
	MomentRevision int64                      `json:"momentRevision"`
	SetVersion     int64                      `json:"setVersion"`
	Status         string                     `json:"status"`
	Attachments    []Attachment               `json:"attachments"`
	SourceVersion  string                     `json:"sourceVersion"`
	ObservedAt     time.Time                  `json:"observedAt"`
	ValidUntil     time.Time                  `json:"validUntil"`
	CanReplace     bool                       `json:"canReplace"`
	CanDelete      bool                       `json:"canDelete"`
	ResponseProof  identity.HumanControlProof `json:"-"`
	OriginalFrame  identity.HumanControlFrame `json:"-"`
}
type Input struct {
	OperationID            string            `json:"operationId"`
	Action                 string            `json:"action"`
	ExpectedMomentRevision int64             `json:"expectedMomentRevision"`
	ExpectedSetVersion     int64             `json:"expectedSetVersion"`
	Attachments            []AttachmentInput `json:"attachments"`
	SourceVersion          string            `json:"sourceVersion,omitempty"`
	Approval               string            `json:"approval,omitempty"`
}
type PreviewImage struct {
	AttachmentInput
	Stage           Stage  `json:"material"`
	ThumbnailBase64 string `json:"thumbnailBase64"`
}
type Preview struct {
	identity.HumanControlPreview
	MomentID               string                     `json:"momentId"`
	ExpectedMomentRevision int64                      `json:"expectedMomentRevision"`
	ExpectedSetVersion     int64                      `json:"expectedSetVersion"`
	SourceVersion          string                     `json:"sourceVersion"`
	Images                 []PreviewImage             `json:"images"`
	ResponseProof          identity.HumanControlProof `json:"-"`
}
type Receipt struct {
	OperationID            string          `json:"operationId"`
	Status                 string          `json:"status"`
	MomentID               string          `json:"momentId"`
	Action                 string          `json:"action"`
	ExpectedMomentRevision int64           `json:"expectedMomentRevision"`
	ExpectedSetVersion     int64           `json:"expectedSetVersion"`
	AppliedSetVersion      int64           `json:"appliedSetVersion"`
	PayloadDigest          string          `json:"payloadDigest"`
	Media                  []StageSelector `json:"media"`
	CommittedAt            time.Time       `json:"committedAt"`
}
type ContentSelector struct {
	MomentID       string
	MomentRevision int64
	SetVersion     int64
	MediaID        string
	MediaVersion   int64
	Variant        string
}

func ID(v string) bool { return identity.HumanControlUUID(v) }
func Hash(v string) bool {
	raw, e := hex.DecodeString(v)
	return e == nil && len(raw) == 32 && v == hex.EncodeToString(raw)
}
func Digest(v any) string {
	b, _ := json.Marshal(v)
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}
func DigestBytes(b []byte) string { h := sha256.Sum256(b); return hex.EncodeToString(h[:]) }
func InputDigest(moment string, in Input) string {
	values := []any{"public-moment-media-v1", moment, in.OperationID, in.Action, in.ExpectedMomentRevision, in.ExpectedSetVersion}
	for _, a := range in.Attachments {
		values = append(values, []any{a.MediaID, a.MediaVersion, a.Rights.Digest()})
	}
	return Digest(values)
}
func StageDigest(in StageInput) string {
	h := sha256.Sum256(in.Content)
	return Digest([]any{"moment-media-stage-v1", in.OperationID, in.LocalScope, in.ContentType, hex.EncodeToString(h[:])})
}
func Path(moment string, rev, set int64, id string, version int64, variant string) string {
	return "/v1/moments/" + moment + "/public-media/" + strconv.FormatInt(rev, 10) + "/" + strconv.FormatInt(set, 10) + "/" + id + "/" + strconv.FormatInt(version, 10) + "/" + variant
}
func StageValid(s Stage) bool {
	common := ID(s.MediaID) && ID(s.LocalScope) && s.Version >= 1 && s.Version <= MaxVersion && Hash(s.DisplaySHA) && Hash(s.ThumbnailSHA) && s.DisplayBytes > 0 && s.DisplayBytes <= MaxDisplayBytes && s.ThumbnailBytes > 0 && s.ThumbnailBytes <= MaxThumbnailBytes && s.Width > 0 && s.Height > 0 && s.Width <= 1920 && s.Height <= 1920 && s.ValidationStatus == "VALIDATED" && s.AntivirusScan == "NOT_RUN" && s.PrivacyRisk == "UNKNOWN" && !s.PreparedAt.IsZero() && s.RetainUntil.After(s.PreparedAt) && s.RetainUntil.Sub(s.PreparedAt) <= StageTTL
	if !common {
		return false
	}
	if s.Kind == "" {
		return s.SchemaVersion == "" && s.Purpose == "" && s.Video == nil && s.ValidationKind == ValidationKind
	}
	return s.Kind == "VIDEO" && s.SchemaVersion == "public-moment-media-stage-v2" && s.Purpose == VideoPurpose && s.Video != nil && s.Video.Valid() && s.Video.Path == VideoStagePath(s.MediaID, s.Version, "video") && max(s.Width, s.Height) <= 1280 && min(s.Width, s.Height) <= 720 && s.ValidationKind == VideoValidation
}
func ValidateInput(in Input, commit bool, now time.Time) error {
	if !ID(in.OperationID) || in.ExpectedMomentRevision < 2 || in.ExpectedMomentRevision > MaxVersion || in.ExpectedSetVersion < 0 || in.ExpectedSetVersion >= MaxVersion {
		return ErrInvalid
	}
	if in.Action == "REPLACE" {
		if len(in.Attachments) < 1 || len(in.Attachments) > MaxImages {
			return ErrInvalid
		}
		seen := map[string]bool{}
		for _, a := range in.Attachments {
			if !ID(a.MediaID) || seen[a.MediaID] || a.MediaVersion < 1 || a.MediaVersion > MaxVersion || !a.Rights.Valid(now) {
				return ErrInvalid
			}
			seen[a.MediaID] = true
		}
	} else if in.Action != "DELETE" || len(in.Attachments) != 0 || in.ExpectedSetVersion < 1 {
		return ErrInvalid
	}
	if commit {
		if !Hash(in.SourceVersion) || len(in.Approval) < 1 || len(in.Approval) > 8192 {
			return ErrInvalid
		}
	} else if in.SourceVersion != "" || in.Approval != "" {
		return ErrInvalid
	}
	return nil
}
func (v Set) Valid(id string, now time.Time) bool {
	if (v.SchemaVersion != "public-moment-media-set-v1" && v.SchemaVersion != "public-moment-media-set-v2") || v.MomentID != id || !ID(id) || v.MomentRevision < 2 || v.MomentRevision > MaxVersion || v.SetVersion < 0 || v.SetVersion > MaxVersion || !Hash(v.SourceVersion) || v.ObservedAt.IsZero() || !v.ValidUntil.After(now) || v.ValidUntil.Sub(v.ObservedAt) > time.Minute || v.Attachments == nil || len(v.Attachments) > MaxImages {
		return false
	}
	if v.Status == "READY" {
		if v.SetVersion < 1 || len(v.Attachments) < 1 {
			return false
		}
	} else if (v.Status != "EMPTY" && v.Status != "DELETED") || len(v.Attachments) != 0 {
		return false
	}
	seen := map[string]bool{}
	for i, a := range v.Attachments {
		if !ID(a.MediaID) || seen[a.MediaID] || a.MediaVersion < 1 || a.MediaVersion > MaxVersion || a.Position != i || a.DisplayBytes < 1 || a.DisplayBytes > MaxDisplayBytes || a.ThumbnailBytes < 1 || a.ThumbnailBytes > MaxThumbnailBytes || !Hash(a.DisplaySHA) || !Hash(a.ThumbnailSHA) || !Hash(a.RightsDigest) || a.Width < 1 || a.Height < 1 || a.Width > 1920 || a.Height > 1920 || a.ValidationStatus != "VALIDATED" || a.AntivirusScan != "NOT_RUN" || a.PrivacyRisk != "UNKNOWN" || a.DisplayPath != Path(id, v.MomentRevision, v.SetVersion, a.MediaID, a.MediaVersion, "display") || a.ThumbnailPath != Path(id, v.MomentRevision, v.SetVersion, a.MediaID, a.MediaVersion, "thumbnail") || !a.ExpiresAt.Equal(v.ValidUntil) {
			return false
		}
		if a.Kind == "" {
			if v.SchemaVersion != "public-moment-media-set-v1" || a.SchemaVersion != "" || a.Video != nil || a.Purpose != "" || a.ValidationKind != ValidationKind {
				return false
			}
		} else if a.Kind != "VIDEO" || len(v.Attachments) != 1 || v.SchemaVersion != "public-moment-media-set-v2" || a.SchemaVersion != "public-moment-media-attachment-v2" || a.Purpose != VideoPurpose || a.Video == nil || !a.Video.Valid() || a.Video.Path != Path(id, v.MomentRevision, v.SetVersion, a.MediaID, a.MediaVersion, "video") || a.ValidationKind != VideoValidation || max(a.Width, a.Height) > 1280 || min(a.Width, a.Height) > 720 {
			return false
		}
		seen[a.MediaID] = true
	}
	return true
}
func (r Receipt) Valid(moment, op string) bool {
	if r.OperationID != op || !ID(op) || r.Media == nil || r.CommittedAt.IsZero() {
		return false
	}
	if r.Status == "STOPPED" {
		return r.MomentID == moment && r.Action == "" && r.PayloadDigest == "" && r.AppliedSetVersion == 0 && len(r.Media) == 0
	}
	if r.Status != "SUCCEEDED" || r.MomentID != moment || !ID(moment) || !Hash(r.PayloadDigest) || r.ExpectedMomentRevision < 2 || r.ExpectedSetVersion < 0 || r.AppliedSetVersion != r.ExpectedSetVersion+1 {
		return false
	}
	if r.Action == "DELETE" {
		return len(r.Media) == 0
	}
	if r.Action != "REPLACE" || len(r.Media) < 1 || len(r.Media) > MaxImages {
		return false
	}
	seen := map[string]bool{}
	for _, m := range r.Media {
		if !ID(m.MediaID) || seen[m.MediaID] || m.Version < 1 {
			return false
		}
		seen[m.MediaID] = true
	}
	return true
}
func StrictJSON(raw []byte, out any) error {
	if !utf8.Valid(raw) {
		return ErrInvalid
	}
	duplicateCheck := json.NewDecoder(bytes.NewReader(raw))
	if uniqueJSON(duplicateCheck) != nil {
		return ErrInvalid
	}
	if _, e := duplicateCheck.Token(); e != io.EOF {
		return ErrInvalid
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if dec.Decode(out) != nil {
		return ErrInvalid
	}
	var extra any
	if dec.Decode(&extra) != io.EOF {
		return ErrInvalid
	}
	return nil
}
func uniqueJSON(dec *json.Decoder) error {
	token, e := dec.Token()
	if e != nil {
		return ErrInvalid
	}
	delimiter, structured := token.(json.Delim)
	if !structured {
		return nil
	}
	switch delimiter {
	case '{':
		seen := map[string]bool{}
		for dec.More() {
			key, e := dec.Token()
			name, ok := key.(string)
			if e != nil || !ok || seen[name] {
				return ErrInvalid
			}
			seen[name] = true
			if uniqueJSON(dec) != nil {
				return ErrInvalid
			}
		}
		end, e := dec.Token()
		if e != nil || end != json.Delim('}') {
			return ErrInvalid
		}
	case '[':
		for dec.More() {
			if uniqueJSON(dec) != nil {
				return ErrInvalid
			}
		}
		end, e := dec.Token()
		if e != nil || end != json.Delim(']') {
			return ErrInvalid
		}
	default:
		return ErrInvalid
	}
	return nil
}

type Store interface {
	PrepareMomentMedia(context.Context, Access, StageInput) (StageReceipt, error)
	ReadMomentMediaPreparation(context.Context, Access, string, bool) (StageReceipt, error)
	ReadOwnMomentMedia(context.Context, Access, string) (Set, error)
	PreviewMomentMedia(context.Context, Access, string, Input) (Preview, error)
	CommitMomentMedia(context.Context, Access, string, Input) (Receipt, error)
	ReadMomentMediaOperation(context.Context, Access, string, string, bool) (Receipt, error)
	ReadPublicMomentMedia(context.Context, Access, string) (Set, error)
	FinalizePublicMomentMedia(context.Context, Access, Set) (Set, error)
	ReadPublicMomentMediaContent(context.Context, Access, ContentSelector, func(Attachment, []byte) ([]byte, error)) ([]byte, error)
}
