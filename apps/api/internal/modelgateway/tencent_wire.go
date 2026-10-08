package modelgateway

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"time"
	"unicode/utf8"
)

const TencentLiveWireContract = "tokenhub.chat-completions.v1"
const TencentLiveInputBoundEvidence = "PROVIDER_MAX_INPUT_BOUND"
const TencentLiveInputBoundSource = "https://cloud.tencent.com/document/product/1823/132252"

// The published selected hy3 route admits at most 192k input tokens. 192*1024
// conservatively covers decimal/binary k notation. This is a universal provider
// bound, not a tokenizer count, configured ceiling or a weight revision claim.
const TencentLiveMaxInputTokens int64 = 192 * 1024
const TencentLiveMaxOutputTokens = 768

// This is a bound for the server-selected two-message TEXT template, under
// the audited official ByteLevel/BPE tokenizer below. Using that same pinned
// tokenizer/template in TokenHub hosting is an engineering inference; this is
// neither a provider maximum-input claim nor measured hosted tokenizer parity.
const TencentDeepSeekMaxInputTokens int64 = 16384
const TencentDeepSeekInputBoundEvidence = "TOKENIZER_BYTE_BPE_UPPER_BOUND"
const TencentDeepSeekInputBoundSource = "https://huggingface.co/deepseek-ai/DeepSeek-V4-Pro-0813/resolve/72e1d3230f6c080a530b0a1d46f8eb4602340597/tokenizer.json"
const TencentDeepSeekTokenizerSHA256 = "8f9f37ca37fdc4f5fd36d5cf4d3b0e8392edb4e894fd10cc0d70b4957c8633cf"
const TencentDeepSeekTemplateSource = "https://huggingface.co/deepseek-ai/DeepSeek-V4-Pro-0813/resolve/72e1d3230f6c080a530b0a1d46f8eb4602340597/tokenizer_config.json"
const TencentDeepSeekTemplateSHA256 = "6ac8c8dc065ed118161d02dd532749ae3f52c243deac27872134fae2f50d8547"
const TencentDeepSeekEncoderSource = "https://huggingface.co/deepseek-ai/DeepSeek-V4-Pro-0813/resolve/72e1d3230f6c080a530b0a1d46f8eb4602340597/encoding/encoding_dsv4.py"
const TencentDeepSeekEncoderSHA256 = "abc0d26120250dda0ae077dc64aa28836026e61e970854aaeb792445e6a0dde6"
const TencentDeepSeekTemplateOverheadBytes = 66

// BOS + system + User + user + Assistant + </think>; content is inserted
// literally into this fixed template. Byte symbols never normalize/expand;
// splits only segment, BPE only merges, and postprocessing adds zero tokens.
const tencentDeepSeekFrame = "<｜begin▁of▁sentence｜><｜User｜><｜Assistant｜></think>"

var ErrLivePreparation = errors.New("invalid trusted live preparation")

// Profiles are package-owned exact routes, not caller-provided bounds or
// immutable weight revision claims. Each route binds its own input proof;
// DeepSeek cannot borrow HY3's provider maximum-input bound.
type tencentModelProfile struct {
	model, version                       string
	inputBound                           int64
	inputBoundEvidence, inputBoundSource string
	tokenizerSHA256, templateSHA256      string
	encoderSHA256                        string
	templateOverheadBytes                int
}

func tencentProfileForModel(model string) (tencentModelProfile, bool) {
	switch model {
	case TencentTokenHubModel:
		return tencentModelProfile{model: model, version: model, inputBound: TencentLiveMaxInputTokens, inputBoundEvidence: TencentLiveInputBoundEvidence, inputBoundSource: TencentLiveInputBoundSource}, true
	case TencentTokenHubDeepSeekModel:
		return tencentModelProfile{model: model, version: model, inputBound: TencentDeepSeekMaxInputTokens, inputBoundEvidence: TencentDeepSeekInputBoundEvidence, inputBoundSource: TencentDeepSeekInputBoundSource, tokenizerSHA256: TencentDeepSeekTokenizerSHA256, templateSHA256: TencentDeepSeekTemplateSHA256, encoderSHA256: TencentDeepSeekEncoderSHA256, templateOverheadBytes: TencentDeepSeekTemplateOverheadBytes}, true
	default:
		return tencentModelProfile{}, false
	}
}

func (p tencentModelProfile) nativeReady() bool {
	expected, ok := tencentProfileForModel(p.model)
	return ok && p == expected && p.inputBound > 0 && p.inputBoundEvidence != "" && p.inputBoundSource != ""
}

func (p tencentModelProfile) descriptor() ProviderDescriptor {
	return ProviderDescriptor{ProviderID: "tencent_tokenhub", ModelID: p.model, ModelVersion: p.version, Mode: Live}
}

// One formatter is shared with the existing adapter. It grants no egress.
func formatTencentTextWire(r ProviderRequest, now time.Time, maxOutput int) ([]byte, error) {
	return formatTencentTextWireForModel(r, now, maxOutput, TencentTokenHubModel)
}

func formatTencentTextWireForModel(r ProviderRequest, now time.Time, maxOutput int, model string) ([]byte, error) {
	profile, ok := tencentProfileForModel(model)
	if !ok {
		return nil, ErrInvalid
	}
	if err := validateTencentRequest(r, now, maxOutput); err != nil {
		return nil, err
	}
	if model == TencentTokenHubDeepSeekModel && !validTencentDeepSeekTextInput(r, profile) {
		return nil, ErrInvalid
	}
	// No web_search_options/tools or arbitrary provider fields are admitted.
	wire := tencentTextRequest{Model: model, Messages: r.Messages,
		MaxCompletionTokens: r.MaxOutputTokens, N: 1, Stream: false, ToolChoice: "none"}
	// Both approved text routes explicitly disable thinking; no provider defaults.
	wire.Thinking.Type = "disabled"
	body, err := json.Marshal(wire)
	if err != nil || len(body) > MaxRequestBytes {
		return nil, ErrInvalid
	}
	return body, nil
}

// This helper bounds the literal rendered UTF-8 bytes before JSON escaping.
// The original validMessage 4096-byte cap remains independently enforced by
// validateTencentRequest. No history, context role, tools or configurable chat
// template can enter this two-role engineering proof.
func validTencentDeepSeekTextInput(r ProviderRequest, profile tencentModelProfile) bool {
	if !profile.nativeReady() || profile.model != TencentTokenHubDeepSeekModel || profile.templateOverheadBytes != len(tencentDeepSeekFrame) || len(r.Messages) != 2 || r.Messages[0].Role != "system" || r.Messages[1].Role != "user" || r.OutputMode != Text || len(r.ToolAllowlist) != 0 || r.MaxOutputTokens < 1 || r.MaxOutputTokens > TencentLiveMaxOutputTokens {
		return false
	}
	remaining := profile.inputBound - int64(profile.templateOverheadBytes)
	for _, message := range r.Messages {
		if !utf8.ValidString(message.Content) || int64(len(message.Content)) > remaining {
			return false
		}
		remaining -= int64(len(message.Content))
	}
	return true
}

// PreparedTencentWire is immutable server-local payload metadata, never an
// approval/receipt. There is no public constructor or caller-selected lower
// token ceiling. Bytes and provider messages are held as private copies.
type PreparedTencentWire struct {
	wire    string
	digest  string
	request ProviderRequest
	profile tencentModelProfile
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
func (p PreparedTencentWire) ExactWire() []byte          { return []byte(p.wire) }
func (p PreparedTencentWire) WireDigest() string         { return p.digest }
func (PreparedTencentWire) WireContract() string         { return TencentLiveWireContract }
func (p PreparedTencentWire) InputTokenBound() int64     { return p.profile.inputBound }
func (p PreparedTencentWire) InputBoundEvidence() string { return p.profile.inputBoundEvidence }
func (p PreparedTencentWire) InputBoundSource() string   { return p.profile.inputBoundSource }

// Descriptor identifies the private prepared route. It is data, not a grant.
func (p PreparedTencentWire) Descriptor() ProviderDescriptor { return p.profile.descriptor() }
func (p PreparedTencentWire) MaxOutputTokens() int           { return p.request.MaxOutputTokens }
func (p PreparedTencentWire) DeadlineAt() time.Time          { return p.request.DeadlineAt }

func tencentWireDigest(raw []byte) string {
	h := sha256.Sum256(raw)
	return hex.EncodeToString(h[:])
}
func (p PreparedTencentWire) valid(now time.Time) bool {
	if !p.profile.nativeReady() || p.request.MaxOutputTokens > TencentLiveMaxOutputTokens || len(p.wire) == 0 || p.digest != tencentWireDigest([]byte(p.wire)) {
		return false
	}
	formatted, err := formatTencentTextWireForModel(p.request, now, TencentLiveMaxOutputTokens, p.profile.model)
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
	wire, err := formatTencentTextWireForModel(r, now, TencentLiveMaxOutputTokens, p.profile.model)
	return err == nil && string(wire) == p.wire && tencentWireDigest(wire) == p.digest
}

// Prepare returns exact provider-only bytes and the selected route's fixed
// input proof. It does not inspect identity, reserve funds, contact a provider or
// prove that the selected native price/budget allows this request.
func (a *TencentTokenHubAdapter) Prepare(r ProviderRequest) (PreparedTencentWire, error) {
	if a == nil || a.now == nil || a.client == nil || !a.config.valid() || r.MaxOutputTokens > TencentLiveMaxOutputTokens {
		return PreparedTencentWire{}, ErrLivePreparation
	}
	profile, ok := tencentProfileForModel(a.config.model)
	if !ok || !profile.nativeReady() {
		return PreparedTencentWire{}, ErrLivePreparation
	}
	r.Messages = append([]Message(nil), r.Messages...)
	r.ToolAllowlist = append([]string(nil), r.ToolAllowlist...)
	body, err := formatTencentTextWireForModel(r, a.now(), a.config.maxOutputTokens, profile.model)
	if err != nil {
		return PreparedTencentWire{}, ErrLivePreparation
	}
	return PreparedTencentWire{wire: string(body), digest: tencentWireDigest(body), request: r, profile: profile}, nil
}
