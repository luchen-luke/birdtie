// Package momentphoto owns explicitly approved public Moment derivatives.
// It cannot read private attachments or manage another kind of entity cover.
package momentphoto

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"strconv"
	"strings"
	"time"

	ec "github.com/birdtie/birdtie/apps/api/internal/entitycover"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	av "github.com/birdtie/birdtie/apps/api/internal/profileavatar"
)

const MaxVersion int64 = 8999999999999999
const Width, Height = ec.Width, ec.Height
const MaxInputBytes, MaxThumbnailBytes = ec.MaxInputBytes, ec.MaxThumbnailBytes

var ErrInvalid = identity.ErrControlInvalid
var ErrChanged = identity.ErrControlChanged
var ErrUnavailable = identity.ErrControlUnavailable
var ErrForbidden = errors.New("public Moment photo owner required")
var ErrNotFound = errors.New("public Moment photo not found")

type Access = av.Access
type Rights = ec.Rights
type Derivative = ec.Derivative
type PreviewDerivative = ec.PreviewDerivative

type Selector struct {
	MomentID       string
	MomentRevision int64
	PhotoID        string
	Version        int64
}

func (s Selector) Valid() bool {
	return identity.HumanControlUUID(s.MomentID) && identity.HumanControlUUID(s.PhotoID) && s.MomentRevision >= 2 && s.MomentRevision <= MaxVersion && s.Version >= 1 && s.Version <= MaxVersion
}
func Path(moment string, revision int64, photo string, version int64) string {
	return "/v1/moments/" + moment + "/public-photo/" + strconv.FormatInt(revision, 10) + "/" + photo + "/" + strconv.FormatInt(version, 10) + "/content"
}

type Metadata struct {
	MomentID       string    `json:"momentId"`
	MomentRevision int64     `json:"momentRevision"`
	PhotoID        string    `json:"photoId"`
	Version        int64     `json:"version"`
	ThumbnailPath  string    `json:"thumbnailPath"`
	Width          int       `json:"width"`
	Height         int       `json:"height"`
	ByteCount      int       `json:"byteCount"`
	SHA256         string    `json:"sha256"`
	ExpiresAt      time.Time `json:"expiresAt"`
	Attribution    string    `json:"attribution"`
}

func Hex(s string) bool {
	b, e := hex.DecodeString(s)
	return e == nil && len(b) == 32 && s == strings.ToLower(s)
}
func (m Metadata) Valid(now time.Time) bool {
	r := Rights{Basis: "OWNED", Attribution: m.Attribution, CanRetain: true, CanPublish: true, RetentionUntil: now.Add(time.Hour)}
	return (Selector{m.MomentID, m.MomentRevision, m.PhotoID, m.Version}).Valid() && m.ThumbnailPath == Path(m.MomentID, m.MomentRevision, m.PhotoID, m.Version) && m.Width == Width && m.Height == Height && m.ByteCount > 0 && m.ByteCount <= MaxThumbnailBytes && Hex(m.SHA256) && m.ExpiresAt.After(now) && !m.ExpiresAt.After(now.Add(61*time.Second)) && r.Valid(now)
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
	if c != *old {
		return false
	}
	*current = c
	return true
}

type State struct {
	MomentID       string                     `json:"momentId"`
	MomentRevision int64                      `json:"momentRevision"`
	PhotoID        *string                    `json:"photoId"`
	PhotoVersion   int64                      `json:"photoVersion"`
	Photo          *Metadata                  `json:"photo"`
	CanSave        bool                       `json:"canSave"`
	CanDelete      bool                       `json:"canDelete"`
	ObservedAt     time.Time                  `json:"observedAt"`
	ExpiresAt      time.Time                  `json:"expiresAt"`
	SourceVersion  string                     `json:"sourceVersion"`
	ResponseProof  identity.HumanControlProof `json:"-"`
}

func (s State) Valid(moment string, now time.Time) bool {
	if s.MomentID != moment || !identity.HumanControlUUID(moment) || s.MomentRevision < 1 || s.MomentRevision > MaxVersion || s.PhotoVersion < 0 || s.PhotoVersion > MaxVersion || s.ObservedAt.IsZero() || !s.ExpiresAt.After(now) || s.ExpiresAt.Sub(s.ObservedAt) > 30*time.Second || !Hex(s.SourceVersion) {
		return false
	}
	if s.PhotoVersion == 0 {
		if s.PhotoID != nil || s.Photo != nil || s.CanDelete {
			return false
		}
	} else if s.PhotoID == nil || !identity.HumanControlUUID(*s.PhotoID) {
		return false
	}
	if s.CanSave && (s.MomentRevision < 2 || s.PhotoVersion >= MaxVersion) {
		return false
	}
	return s.Photo == nil || s.Photo.Valid(now) && s.Photo.MomentID == moment && s.Photo.MomentRevision == s.MomentRevision && s.Photo.PhotoID == *s.PhotoID && s.Photo.Version == s.PhotoVersion && !s.Photo.ExpiresAt.After(s.ExpiresAt)
}

type Input struct {
	OperationID            string
	Action                 string
	ExpectedMomentRevision int64
	PhotoID                string
	ExpectedPhotoVersion   int64
	ContentType            string
	Content                []byte
	Rights                 *Rights
	SourceVersion          string
	Approval               string
}

func Normalize(in Input, commit bool, now time.Time) (Derivative, string, string, error) {
	if !identity.HumanControlUUID(in.OperationID) || !identity.HumanControlUUID(in.PhotoID) || in.ExpectedMomentRevision < 1 || in.ExpectedMomentRevision > MaxVersion || in.ExpectedPhotoVersion < 0 || in.ExpectedPhotoVersion > MaxVersion || (commit && (!Hex(in.SourceVersion) || len(in.Approval) == 0 || len(in.Approval) > 8192)) || (!commit && (in.SourceVersion != "" || in.Approval != "")) {
		return Derivative{}, "", "", ErrInvalid
	}
	if in.Action == "DELETE" {
		if in.ExpectedPhotoVersion == 0 || in.ContentType != "" || len(in.Content) != 0 || in.Rights != nil {
			return Derivative{}, "", "", ErrInvalid
		}
		return Derivative{}, "", "", nil
	}
	if in.Action != "SAVE" || in.ExpectedMomentRevision < 2 || in.ExpectedPhotoVersion >= MaxVersion || in.Rights == nil || !in.Rights.Valid(now) {
		return Derivative{}, "", "", ErrInvalid
	}
	d, e := ec.Prepare(in.Content, in.ContentType)
	if e != nil {
		return Derivative{}, "", "", ErrInvalid
	}
	return d, d.SHA256, in.Rights.Digest(), nil
}
func Digest(moment string, in Input, inputHash, rightsHash string) string {
	raw, _ := json.Marshal([]any{"public-moment-photo-v1", moment, in.OperationID, in.Action, in.ExpectedMomentRevision, in.PhotoID, in.ExpectedPhotoVersion, inputHash, rightsHash})
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}
func NextVersion(v int64) int64 {
	if v >= MaxVersion {
		return MaxVersion
	}
	return v + 1
}

type Preview struct {
	identity.HumanControlPreview
	MomentID               string                     `json:"momentId"`
	PhotoID                string                     `json:"photoId"`
	ExpectedMomentRevision int64                      `json:"expectedMomentRevision"`
	ExpectedPhotoVersion   int64                      `json:"expectedPhotoVersion"`
	InputSHA256            string                     `json:"inputSha256"`
	RightsSHA256           string                     `json:"rightsSha256"`
	SourceVersion          string                     `json:"sourceVersion"`
	PreviewDerivative      *PreviewDerivative         `json:"previewDerivative,omitempty"`
	ResponseProof          identity.HumanControlProof `json:"-"`
}
type Receipt struct {
	OperationID            string     `json:"operationId"`
	Status                 string     `json:"status"`
	AppliedPhotoVersion    int64      `json:"appliedPhotoVersion"`
	Action                 string     `json:"action,omitempty"`
	MomentID               string     `json:"momentId,omitempty"`
	PhotoID                string     `json:"photoId,omitempty"`
	ExpectedMomentRevision *int64     `json:"expectedMomentRevision,omitempty"`
	ExpectedPhotoVersion   *int64     `json:"expectedPhotoVersion,omitempty"`
	PayloadDigest          string     `json:"payloadDigest,omitempty"`
	CommittedAt            *time.Time `json:"committedAt,omitempty"`
}

func (r Receipt) Valid(moment, op string) bool {
	if !identity.HumanControlUUID(op) || r.OperationID != op {
		return false
	}
	if r.Status == "STOPPED" {
		return r.AppliedPhotoVersion == 0 && r.Action == "" && r.MomentID == "" && r.PhotoID == "" && r.ExpectedMomentRevision == nil && r.ExpectedPhotoVersion == nil && r.PayloadDigest == "" && r.CommittedAt == nil
	}
	if r.Action == "SAVE" && (r.ExpectedPhotoVersion == nil || *r.ExpectedPhotoVersion >= MaxVersion || r.ExpectedMomentRevision == nil || *r.ExpectedMomentRevision < 2) || r.Action == "DELETE" && (r.ExpectedPhotoVersion == nil || *r.ExpectedPhotoVersion < 1) {
		return false
	}
	return r.Status == "SUCCEEDED" && r.MomentID == moment && identity.HumanControlUUID(moment) && identity.HumanControlUUID(r.PhotoID) && (r.Action == "SAVE" || r.Action == "DELETE") && r.ExpectedMomentRevision != nil && *r.ExpectedMomentRevision >= 1 && *r.ExpectedMomentRevision <= MaxVersion && r.ExpectedPhotoVersion != nil && *r.ExpectedPhotoVersion >= 0 && *r.ExpectedPhotoVersion <= MaxVersion && r.AppliedPhotoVersion == NextVersion(*r.ExpectedPhotoVersion) && Hex(r.PayloadDigest) && r.CommittedAt != nil && !r.CommittedAt.IsZero()
}

type Encode func(Metadata, []byte) ([]byte, error)
type Store interface {
	ReadMomentPhotoState(context.Context, Access, string) (State, error)
	PreviewMomentPhoto(context.Context, Access, string, Input) (Preview, error)
	CommitMomentPhoto(context.Context, Access, string, Input) (Receipt, error)
	ReadMomentPhotoOperation(context.Context, Access, string, string) (Receipt, error)
	StopMomentPhotoOperation(context.Context, Access, string, string) (Receipt, error)
	ReadMomentPhotoContent(context.Context, Access, Selector, Encode) ([]byte, error)
}

func object(raw []byte) (map[string]json.RawMessage, error) {
	d := json.NewDecoder(bytes.NewReader(raw))
	t, e := d.Token()
	if e != nil || t != json.Delim('{') {
		return nil, ErrInvalid
	}
	m := map[string]json.RawMessage{}
	for d.More() {
		k, e := d.Token()
		s, ok := k.(string)
		if e != nil || !ok {
			return nil, ErrInvalid
		}
		if _, ok = m[s]; ok {
			return nil, ErrInvalid
		}
		var v json.RawMessage
		if d.Decode(&v) != nil {
			return nil, ErrInvalid
		}
		m[s] = v
	}
	if _, e = d.Token(); e != nil {
		return nil, ErrInvalid
	}
	if _, e = d.Token(); e != io.EOF {
		return nil, ErrInvalid
	}
	return m, nil
}
func Decode(raw []byte, commit bool) (Input, error) {
	var in Input
	m, e := object(raw)
	if e != nil {
		return in, e
	}
	for k := range m {
		switch k {
		case "operationId", "action", "expectedMomentRevision", "photoId", "expectedPhotoVersion", "contentType", "contentBase64", "rights":
		case "sourceVersion", "approval":
			if !commit {
				return in, ErrInvalid
			}
		default:
			return in, ErrInvalid
		}
	}
	for k, p := range map[string]any{"operationId": &in.OperationID, "action": &in.Action, "photoId": &in.PhotoID, "expectedMomentRevision": &in.ExpectedMomentRevision, "expectedPhotoVersion": &in.ExpectedPhotoVersion} {
		if b, ok := m[k]; !ok || bytes.Equal(b, []byte("null")) || json.Unmarshal(b, p) != nil {
			return in, ErrInvalid
		}
	}
	if commit {
		for k, p := range map[string]*string{"sourceVersion": &in.SourceVersion, "approval": &in.Approval} {
			if b, ok := m[k]; !ok || json.Unmarshal(b, p) != nil {
				return in, ErrInvalid
			}
		}
	}
	if in.Action == "SAVE" {
		var encoded string
		if json.Unmarshal(m["contentType"], &in.ContentType) != nil || json.Unmarshal(m["contentBase64"], &encoded) != nil || len(encoded) > (MaxInputBytes+2)/3*4 {
			return in, ErrInvalid
		}
		in.Content, e = base64.StdEncoding.Strict().DecodeString(encoded)
		if e != nil || base64.StdEncoding.EncodeToString(in.Content) != encoded {
			return in, ErrInvalid
		}
		r, e := object(m["rights"])
		if e != nil || len(r) != 6 {
			return in, ErrInvalid
		}
		for _, k := range []string{"basis", "attribution", "licenseReference", "retentionUntil", "canRetain", "canPublish"} {
			if v, ok := r[k]; !ok || bytes.Equal(v, []byte("null")) {
				return in, ErrInvalid
			}
		}
		in.Rights = &Rights{}
		if json.Unmarshal(m["rights"], in.Rights) != nil {
			return in, ErrInvalid
		}
	} else {
		for _, k := range []string{"contentType", "contentBase64", "rights"} {
			if _, ok := m[k]; ok {
				return in, ErrInvalid
			}
		}
	}
	if _, _, _, e = Normalize(in, commit, time.Now().UTC()); e != nil {
		return in, e
	}
	return in, nil
}
