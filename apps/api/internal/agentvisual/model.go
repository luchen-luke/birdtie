// Package agentvisual defines one explicit, private, approved-pixel analysis.
// It does not grant Memory writes, place facts, or human attachment forwarding.
package agentvisual

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"github.com/birdtie/birdtie/apps/api/internal/actorref"
	"strings"
	"time"
	"unicode/utf8"
)

const Purpose = "HUMAN_AGENT_VISUAL_ANALYSIS"
const EgressPurpose = "MODEL_VISUAL_PIXEL_EGRESS"
const Scope = "SELF_VISUAL_PIXEL_ANALYSIS"
const MaxInputBytes = 10 * 1024 * 1024
const MaxTextBytes = 240

var ErrInvalid = errors.New("invalid visual analysis")
var ErrConflict = errors.New("visual preview changed")
var ErrDenied = errors.New("visual source unavailable")
var ErrUnavailable = errors.New("visual adapter unavailable")
var ErrBudget = errors.New("visual budget unavailable")

type Access struct {
	AccountID, AccountType string
	SessionDigest          [32]byte
}
type ImageInput struct {
	MIME          string `json:"mimeType"`
	ContentBase64 string `json:"contentBase64"`
	InputSHA256   string `json:"inputSha256"`
	PixelRisk     string `json:"pixelRisk"`
}
type PreviewInput struct {
	OperationID           string     `json:"operationId"`
	ClientDraftID         string     `json:"clientDraftId"`
	ClientDraftRevision   int64      `json:"clientDraftRevision"`
	SourceContextType     string     `json:"sourceContextType"`
	SourceContextID       string     `json:"sourceContextId"`
	UserText              string     `json:"userText"`
	Purpose               string     `json:"purpose"`
	Image                 ImageInput `json:"image"`
	TaskID                string     `json:"taskId,omitempty"`
	ExpectedTaskUpdatedAt *time.Time `json:"expectedTaskUpdatedAt,omitempty"`
}
type ApproveInput struct {
	OperationID     string `json:"operationId"`
	ExpectedVersion int64  `json:"expectedVersion"`
	PayloadDigest   string `json:"payloadDigest"`
	PixelSHA256     string `json:"pixelSha256"`
	Purpose         string `json:"purpose"`
	Confirmed       bool   `json:"confirmed"`
}
type Pixel struct {
	ID                 string `json:"id"`
	Revision           int64  `json:"revision"`
	SHA256             string `json:"sha256"`
	MIME               string `json:"mimeType"`
	Width              int    `json:"width"`
	Height             int    `json:"height"`
	ByteSize           int    `json:"byteSize"`
	PreviewContentPath string `json:"previewContentPath"`
}
type Destination struct {
	Provider     string `json:"provider"`
	Model        string `json:"model"`
	WireContract string `json:"wireContract"`
	Region       string `json:"region"`
	Retention    string `json:"retention"`
}
type Budget struct {
	Currency         string    `json:"currency"`
	PriceVersion     string    `json:"priceVersion"`
	InputTokenUpper  int64     `json:"inputTokenUpper"`
	OutputTokenUpper int64     `json:"outputTokenUpper"`
	CostMicrosUpper  int64     `json:"costMicrosUpper"`
	PriceExpiresAt   time.Time `json:"priceExpiresAt"`
}
type Preview struct {
	SchemaVersion         int         `json:"schemaVersion"`
	PreviewID             string      `json:"previewId"`
	Version               int64       `json:"version"`
	Status                string      `json:"status"`
	ClientDraftID         string      `json:"clientDraftId"`
	ClientDraftRevision   int64       `json:"clientDraftRevision"`
	UserText              string      `json:"userText"`
	Purpose               string      `json:"purpose"`
	EgressPurpose         string      `json:"egressPurpose"`
	PayloadDigest         string      `json:"payloadDigest"`
	MemoryWriteAllowed    bool        `json:"memoryWriteAllowed"`
	PlaceFactWriteAllowed bool        `json:"placeFactWriteAllowed"`
	Pixel                 Pixel       `json:"pixel"`
	Destination           Destination `json:"destination"`
	Budget                Budget      `json:"budget"`
	CreatedAt             time.Time   `json:"createdAt"`
	ExpiresAt             time.Time   `json:"expiresAt"`
}
type Confidence struct {
	Kind  string `json:"kind"`
	Level string `json:"level"`
}
type Source struct {
	Type     string `json:"type"`
	ID       string `json:"id"`
	Revision int64  `json:"revision"`
	SHA256   string `json:"sha256"`
}
type Observation struct {
	Kind       string     `json:"kind"`
	Value      string     `json:"value"`
	Confidence Confidence `json:"confidence"`
	Source     Source     `json:"source"`
	ObservedAt time.Time  `json:"observedAt"`
}
type Report struct {
	Summary      string        `json:"summary"`
	Observations []Observation `json:"observations"`
	Unknowns     []string      `json:"unknowns"`
}
type Operation struct {
	SchemaVersion int     `json:"schemaVersion"`
	OperationID   string  `json:"operationId"`
	PreviewID     string  `json:"previewId"`
	Version       int64   `json:"version"`
	Status        string  `json:"status"`
	Report        *Report `json:"report,omitempty"`
}
type Gateway interface {
	Preview(context.Context, Access, PreviewInput) (Preview, error)
	Pixels(context.Context, Access, string) (Pixel, []byte, error)
	Analyze(context.Context, Access, string, ApproveInput) (Operation, error)
	Operation(context.Context, Access, string) (Operation, error)
}

func UUID(v string) bool {
	r, e := actorref.Parse("PERSON", v)
	return e == nil && r.ID == v && v != "00000000-0000-0000-0000-000000000000"
}
func Hash(v string) bool {
	if len(v) != 64 || v == strings.Repeat("0", 64) {
		return false
	}
	for _, c := range v {
		if !(c >= 'a' && c <= 'f' || c >= '0' && c <= '9') {
			return false
		}
	}
	return true
}
func Digest(b []byte) string { h := sha256.Sum256(b); return hex.EncodeToString(h[:]) }
func Text(v string, max int, empty bool) bool {
	return utf8.ValidString(v) && len(v) <= max && !strings.ContainsRune(v, 0) && (empty || strings.TrimSpace(v) != "")
}
func ValidateInput(in PreviewInput) error {
	if !UUID(in.OperationID) || !UUID(in.ClientDraftID) || in.ClientDraftRevision < 1 || in.Purpose != Purpose || !Text(in.UserText, MaxTextBytes, true) ||
		!(in.SourceContextType == "CITY" || in.SourceContextType == "ONLINE") || !Text(in.SourceContextID, 120, false) ||
		in.SourceContextType == "ONLINE" && !UUID(in.SourceContextID) || !Hash(in.Image.InputSHA256) ||
		!(in.Image.MIME == "image/png" || in.Image.MIME == "image/jpeg") || !(in.Image.PixelRisk == "UNKNOWN" || in.Image.PixelRisk == "USER_MASKED") ||
		(in.TaskID != "" && (!UUID(in.TaskID) || in.ExpectedTaskUpdatedAt == nil)) || (in.TaskID == "" && in.ExpectedTaskUpdatedAt != nil) {
		return ErrInvalid
	}
	return nil
}
func ValidateApproval(in ApproveInput) error {
	if !UUID(in.OperationID) || in.ExpectedVersion < 1 || !Hash(in.PayloadDigest) || !Hash(in.PixelSHA256) || in.Purpose != Purpose || !in.Confirmed {
		return ErrInvalid
	}
	return nil
}
