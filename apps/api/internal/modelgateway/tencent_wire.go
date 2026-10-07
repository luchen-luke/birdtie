package modelgateway

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"time"
)

const TencentLiveWireContract = "tokenhub.chat-completions.v1"
const TencentLiveInputBoundEvidence = "PROVIDER_MAX_INPUT_BOUND"
const TencentLiveInputBoundSource = "https://cloud.tencent.com/document/product/1823/132252"

// The published selected hy3 route admits at most 192k input tokens. 192*1024
// conservatively covers decimal/binary k notation. This is a universal provider
// bound, not a tokenizer count, configured ceiling or a weight revision claim.
const TencentLiveMaxInputTokens int64 = 192 * 1024
const TencentLiveMaxOutputTokens = 768

var ErrLivePreparation = errors.New("invalid trusted live preparation")

// One formatter is shared with the existing adapter. It grants no egress.
func formatTencentTextWire(r ProviderRequest, now time.Time, maxOutput int) ([]byte, error) {
	if err := validateTencentRequest(r, now, maxOutput); err != nil {
		return nil, err
	}
	// No web_search_options/tools or arbitrary provider fields are admitted.
	wire := tencentTextRequest{Model: TencentTokenHubModel, Messages: r.Messages,
		MaxCompletionTokens: r.MaxOutputTokens, N: 1, Stream: false, ToolChoice: "none"}
	// Keep the original explicit HY3 thinking switch; no provider defaults.
	wire.Thinking.Type = "disabled"
	body, err := json.Marshal(wire)
	if err != nil || len(body) > MaxRequestBytes {
		return nil, ErrInvalid
	}
	return body, nil
}

// PreparedTencentWire is immutable server-local payload metadata, never an
// approval/receipt. There is no public constructor or caller-selected lower
// token ceiling. Bytes and provider messages are held as private copies.
type PreparedTencentWire struct {
	wire    string
	digest  string
	request ProviderRequest
}

func (PreparedTencentWire) Format(s fmt.State, _ rune) {
	_, _ = io.WriteString(s, "PreparedTencentWire{redacted}")
}
func (PreparedTencentWire) MarshalJSON() ([]byte, error) { return nil, ErrLivePreparation }
func (p *PreparedTencentWire) UnmarshalJSON([]byte) error {
	if p != nil {
		*p = PreparedTencentWire{}
	}
	return ErrLivePreparation
}
func (p PreparedTencentWire) ExactWire() []byte        { return []byte(p.wire) }
func (p PreparedTencentWire) WireDigest() string       { return p.digest }
func (PreparedTencentWire) WireContract() string       { return TencentLiveWireContract }
func (PreparedTencentWire) InputTokenBound() int64     { return TencentLiveMaxInputTokens }
func (PreparedTencentWire) InputBoundEvidence() string { return TencentLiveInputBoundEvidence }
func (PreparedTencentWire) InputBoundSource() string   { return TencentLiveInputBoundSource }
func (p PreparedTencentWire) MaxOutputTokens() int     { return p.request.MaxOutputTokens }
func (p PreparedTencentWire) DeadlineAt() time.Time    { return p.request.DeadlineAt }

func tencentWireDigest(raw []byte) string {
	h := sha256.Sum256(raw)
	return hex.EncodeToString(h[:])
}
func (p PreparedTencentWire) valid(now time.Time) bool {
	if p.request.MaxOutputTokens > TencentLiveMaxOutputTokens || len(p.wire) == 0 || p.digest != tencentWireDigest([]byte(p.wire)) {
		return false
	}
	formatted, err := formatTencentTextWire(p.request, now, TencentLiveMaxOutputTokens)
	return err == nil && string(formatted) == p.wire
}

// Matches is a data-only native projection check. It cannot confer source,
// MODEL_EGRESS or budget approval. Native dispatch must separately re-read all
// those current facts. Exact wire, deadline and non-wire contract selectors
// must match; no configured lower ceiling can replace this private proof.
func (p PreparedTencentWire) Matches(r ProviderRequest, now time.Time) bool {
	if !p.valid(now) || !r.DeadlineAt.Equal(p.request.DeadlineAt) || r.TaskKind != p.request.TaskKind || r.PromptVersion != p.request.PromptVersion || r.OutputMode != p.request.OutputMode || r.OutputSchemaVersion != p.request.OutputSchemaVersion || r.MaxOutputTokens != p.request.MaxOutputTokens || len(r.ToolAllowlist) != 0 {
		return false
	}
	wire, err := formatTencentTextWire(r, now, TencentLiveMaxOutputTokens)
	return err == nil && string(wire) == p.wire && tencentWireDigest(wire) == p.digest
}

// Prepare returns exact provider-only bytes and the fixed universal upper
// bound. It does not inspect identity, reserve funds, contact a provider or
// prove that the selected native price/budget allows this request.
func (a *TencentTokenHubAdapter) Prepare(r ProviderRequest) (PreparedTencentWire, error) {
	if a == nil || a.now == nil || a.client == nil || !a.config.valid() || r.MaxOutputTokens > TencentLiveMaxOutputTokens {
		return PreparedTencentWire{}, ErrLivePreparation
	}
	r.Messages = append([]Message(nil), r.Messages...)
	r.ToolAllowlist = append([]string(nil), r.ToolAllowlist...)
	body, err := formatTencentTextWire(r, a.now(), a.config.maxOutputTokens)
	if err != nil {
		return PreparedTencentWire{}, ErrLivePreparation
	}
	return PreparedTencentWire{wire: string(body), digest: tencentWireDigest(body), request: r}, nil
}
