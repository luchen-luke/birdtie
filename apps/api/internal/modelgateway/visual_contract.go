package modelgateway

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"time"
	"unicode/utf8"
)

const TencentVisionModel = "hy-vision-2.0-instruct"
const TencentVisionWireContract = "tokenhub.visual.chat-completions.v1"
const TencentVisionMaxInputTokens int64 = 24 * 1024
const TencentVisionMaxOutputTokens = 768
const TencentVisionMaxPixelBytes = 10 * 1024 * 1024
const TencentVisionInputBoundSource = "https://cloud.tencent.com/document/product/1823/130051"

type VisualRequest struct {
	UserText        string
	Pixels          []byte
	MIME, SHA256    string
	Width, Height   int
	DeadlineAt      time.Time
	MaxOutputTokens int
}
type VisualConfidence struct {
	Kind  string `json:"kind"`
	Level string `json:"level"`
}
type VisualObservation struct {
	Kind       string           `json:"kind"`
	Value      string           `json:"value"`
	Confidence VisualConfidence `json:"confidence"`
}
type VisualReport struct {
	Summary      string              `json:"summary"`
	Observations []VisualObservation `json:"observations"`
	Unknowns     []string            `json:"unknowns"`
}
type VisualResult struct {
	Status            Status
	Report            *VisualReport
	Usage             Usage
	ProviderRequestID string
}

// Immutable package-owned bytes. Public getters return copies, never an
// authorization, identity selector or text route's preparation receipt.
type PreparedVisualWire struct {
	body     []byte
	digest   string
	output   int
	deadline time.Time
}

func (PreparedVisualWire) String() string   { return "PreparedVisualWire{redacted}" }
func (PreparedVisualWire) GoString() string { return "PreparedVisualWire{redacted}" }
func (PreparedVisualWire) Format(s fmt.State, _ rune) {
	_, _ = io.WriteString(s, "PreparedVisualWire{redacted}")
}
func (PreparedVisualWire) MarshalJSON() ([]byte, error) { return nil, ErrInvalid }
func (p *PreparedVisualWire) UnmarshalJSON([]byte) error {
	*p = PreparedVisualWire{}
	return ErrInvalid
}
func (p PreparedVisualWire) ExactWire() []byte      { return append([]byte(nil), p.body...) }
func (p PreparedVisualWire) WireDigest() string     { return p.digest }
func (p PreparedVisualWire) InputTokenBound() int64 { return TencentVisionMaxInputTokens }
func (p PreparedVisualWire) Descriptor() ProviderDescriptor {
	return ProviderDescriptor{ProviderID: "tencent_tokenhub", ModelID: TencentVisionModel, ModelVersion: TencentVisionModel, Mode: Live}
}
func (p PreparedVisualWire) MaxOutputTokens() int  { return p.output }
func (p PreparedVisualWire) DeadlineAt() time.Time { return p.deadline }
func (p PreparedVisualWire) valid(now time.Time) bool {
	h := sha256.Sum256(p.body)
	return len(p.body) > 0 && len(p.body) < 15*1024*1024 && p.digest == hex.EncodeToString(h[:]) &&
		p.output > 0 && p.output <= TencentVisionMaxOutputTokens && p.deadline.After(now) && !p.deadline.After(now.Add(MaxDeadline))
}
func validateVisualRequest(r VisualRequest, now time.Time) error {
	if !utf8.ValidString(r.UserText) || len(r.UserText) > 240 || bytes.IndexByte([]byte(r.UserText), 0) >= 0 ||
		len(r.Pixels) < 1 || len(r.Pixels) > TencentVisionMaxPixelBytes || r.Width < 1 || r.Height < 1 || r.Width > 4096 || r.Height > 4096 ||
		int64(r.Width)*int64(r.Height) > 4096*4096 || r.MaxOutputTokens < 1 || r.MaxOutputTokens > TencentVisionMaxOutputTokens ||
		!r.DeadlineAt.After(now) || r.DeadlineAt.After(now.Add(MaxDeadline)) {
		return ErrInvalid
	}
	h := sha256.Sum256(r.Pixels)
	if r.SHA256 != hex.EncodeToString(h[:]) {
		return ErrInvalid
	}
	c, kind, e := image.DecodeConfig(bytes.NewReader(r.Pixels))
	if e != nil || c.Width != r.Width || c.Height != r.Height || !((r.MIME == "image/png" && kind == "png") || (r.MIME == "image/jpeg" && kind == "jpeg")) {
		return ErrInvalid
	}
	return nil
}

// Only these model-owned observation fields are accepted. Native code adds
// approved-pixel provenance and observation time after its final source check.
func parseVisualReport(raw []byte) (*VisualReport, error) {
	o, e := strictObject(raw, []string{"summary", "observations", "unknowns"}, nil)
	if e != nil {
		return nil, ErrOutputSchema
	}
	summary, e := stringValue(o["summary"], 4096)
	if e != nil {
		return nil, ErrOutputSchema
	}
	var observations, unknowns []json.RawMessage
	if json.Unmarshal(o["observations"], &observations) != nil || observations == nil || len(observations) > 32 ||
		json.Unmarshal(o["unknowns"], &unknowns) != nil || unknowns == nil || len(unknowns) > 16 {
		return nil, ErrOutputSchema
	}
	out := &VisualReport{Summary: summary, Observations: []VisualObservation{}, Unknowns: []string{}}
	for _, raw := range observations {
		item, e := strictObject(raw, []string{"kind", "value", "confidence"}, nil)
		if e != nil {
			return nil, ErrOutputSchema
		}
		kind, e1 := stringValue(item["kind"], 40)
		value, e2 := stringValue(item["value"], 2048)
		confidence, e3 := strictObject(item["confidence"], []string{"kind", "level"}, nil)
		if e1 != nil || e2 != nil || e3 != nil {
			return nil, ErrOutputSchema
		}
		ck, e4 := stringValue(confidence["kind"], 40)
		level, e5 := stringValue(confidence["level"], 40)
		if e4 != nil || e5 != nil || ck != "QUALITATIVE" || !(kind == "VISIBLE_OBJECT" || kind == "VISIBLE_TEXT" || kind == "VISIBLE_SCENE") ||
			!(level == "LOW" || level == "MEDIUM" || level == "HIGH" || level == "UNKNOWN") {
			return nil, ErrOutputSchema
		}
		out.Observations = append(out.Observations, VisualObservation{kind, value, VisualConfidence{ck, level}})
	}
	for _, raw := range unknowns {
		v, e := stringValue(raw, 2048)
		if e != nil {
			return nil, ErrOutputSchema
		}
		out.Unknowns = append(out.Unknowns, v)
	}
	return out, nil
}
