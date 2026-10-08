package modelgateway

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
)

// These diagnostics contain fixed local classifications only. They are not
// provider text, a response receipt, permission, or authority to retry.
type modelFailureDiagnostic struct {
	stage  modelFailureStage
	kind   modelFailureKind
	reason modelFailureReason
	status int
	http   tencentHTTPMetadata
	// A getter keeps private cause data out of fmt's reflective fallback for
	// invalid verbs (for example Sprintf's unsupported %w). Unwrap preserves
	// the same error identity without a printable raw error field.
	cause func() error
}

type modelFailureStage uint8
type modelFailureKind uint8
type modelFailureReason uint8

// Only reviewed TokenHub codes and wire parameter names can enter diagnostics.
// No response text, request ID, upstream code or arbitrary field is retained.
const maxTencentHTTPDiagnosticBytes = 8 * 1024

type tencentHTTPCode uint8
type tencentHTTPType uint8
type tencentHTTPSource uint8
type tencentHTTPParam uint8

type tencentHTTPMetadata struct {
	code           tencentHTTPCode
	typ            tencentHTTPType
	source         tencentHTTPSource
	param          tencentHTTPParam
	upstreamStatus int
}

var tencentHTTPCodes = [...]string{
	"unknown", "400001", "400002", "400003", "400004", "400005", "400006", "401006",
	"401001", "401002", "401003", "401004", "401005", "401007", "401008",
	"403001", "403002", "403003", "403004", "403005", "403006", "410001", "413001",
	"429001", "429002", "429003", "429004", "429005", "429006", "451001", "499001",
	"500001", "502001", "503001", "504001",
}
var tencentHTTPTypes = [...]string{"unknown", "gateway_error", "invalid_request_error", "upstream_error"}
var tencentHTTPSources = [...]string{"unknown", "client", "gateway", "upstream"}
var tencentHTTPParams = [...]string{"unknown", "model", "messages", "max_completion_tokens", "n", "stream", "thinking", "tool_choice"}

func (m tencentHTTPMetadata) normalized() tencentHTTPMetadata {
	code := tencentHTTPCode(modelDiagnosticIndex(modelDiagnosticName(uint8(m.code), tencentHTTPCodes[:]), tencentHTTPCodes[:]))
	if code == 0 {
		return tencentHTTPMetadata{}
	}
	return tencentHTTPMetadata{code: code,
		typ:            tencentHTTPType(modelDiagnosticIndex(modelDiagnosticName(uint8(m.typ), tencentHTTPTypes[:]), tencentHTTPTypes[:])),
		source:         tencentHTTPSource(modelDiagnosticIndex(modelDiagnosticName(uint8(m.source), tencentHTTPSources[:]), tencentHTTPSources[:])),
		param:          tencentHTTPParam(modelDiagnosticIndex(modelDiagnosticName(uint8(m.param), tencentHTTPParams[:]), tencentHTTPParams[:])),
		upstreamStatus: normalizedModelHTTPStatus(m.upstreamStatus)}
}

func readTencentHTTPMetadata(body io.Reader) tencentHTTPMetadata {
	if body == nil {
		return tencentHTTPMetadata{}
	}
	raw, err := io.ReadAll(io.LimitReader(body, maxTencentHTTPDiagnosticBytes+1))
	defer clear(raw)
	if err != nil || len(raw) > maxTencentHTTPDiagnosticBytes {
		return tencentHTTPMetadata{}
	}
	// Unselected response members are skipped by decoding, not stored in the
	// error. In particular, message/message_zh are never inspected for clues.
	var envelope struct {
		Error struct {
			Code           json.RawMessage `json:"code"`
			Type           string          `json:"type"`
			Source         string          `json:"source"`
			Param          string          `json:"param"`
			UpstreamStatus int             `json:"upstream_status"`
		} `json:"error"`
	}
	if json.Unmarshal(raw, &envelope) != nil {
		return tencentHTTPMetadata{}
	}
	defer clear(envelope.Error.Code)
	var code string
	if json.Unmarshal(envelope.Error.Code, &code) != nil {
		var number int
		if json.Unmarshal(bytes.TrimSpace(envelope.Error.Code), &number) != nil {
			return tencentHTTPMetadata{}
		}
		code = strconv.Itoa(number)
	}
	metadata := tencentHTTPMetadata{
		code:           tencentHTTPCode(modelDiagnosticIndex(code, tencentHTTPCodes[:])),
		typ:            tencentHTTPType(modelDiagnosticIndex(envelope.Error.Type, tencentHTTPTypes[:])),
		source:         tencentHTTPSource(modelDiagnosticIndex(envelope.Error.Source, tencentHTTPSources[:])),
		param:          tencentHTTPParam(modelDiagnosticIndex(envelope.Error.Param, tencentHTTPParams[:])),
		upstreamStatus: normalizedModelHTTPStatus(envelope.Error.UpstreamStatus),
	}
	return metadata.normalized()
}

var modelFailureStages = [...]string{
	"MODEL_GATEWAY", "TENCENT_REQUEST", "TENCENT_TRANSPORT", "TENCENT_HTTP", "TENCENT_RESPONSE",
	"TENCENT_ENVELOPE", "TENCENT_SEARCH_METADATA", "TENCENT_ID_MODEL", "TENCENT_OBJECT",
	"TENCENT_CHOICES", "TENCENT_CHOICE", "TENCENT_MESSAGE", "TENCENT_TOOLS",
	"TENCENT_FINISH", "TENCENT_REFUSAL", "TENCENT_CONTENT", "TENCENT_USAGE", "TENCENT_ENCODE",
	"MODEL_PREPARE", "MODEL_CURRENT", "MODEL_RELEASE_SETTLEMENT", "MODEL_WIRE", "MODEL_NORMALIZE",
}

var modelFailureKinds = [...]string{
	"UNKNOWN", "REQUEST", "HTTP", "TRANSPORT", "CONTENT_TYPE", "RESPONSE_SHAPE",
	"NATIVE_CURRENT", "NATIVE_RELEASE_SETTLEMENT", "NATIVE_WIRE",
}

var modelFailureReasons = [...]string{
	"unknown", "gateway_unavailable", "invalid_context", "cancelled", "invalid_request", "inference_disabled",
	"live_preparation_unavailable", "live_current_authority_unavailable", "live_release_or_settlement_unknown",
	"live_wire_unavailable", "invalid_live_text_response",
	"provider_RATE_LIMIT", "provider_TEMPORARY", "provider_REFUSED", "provider_AUTHENTICATION",
	"provider_INVALID_REQUEST", "provider_DEADLINE", "provider_CANCELLED", "provider_UNKNOWN",
}

func modelDiagnosticIndex(value string, allowed []string) uint8 {
	for index, known := range allowed {
		if value == known {
			return uint8(index)
		}
	}
	return 0
}

func modelDiagnosticName(index uint8, allowed []string) string {
	if int(index) >= len(allowed) {
		return allowed[0]
	}
	return allowed[index]
}

func normalizedModelFailureStage(stage string) modelFailureStage {
	return modelFailureStage(modelDiagnosticIndex(stage, modelFailureStages[:]))
}
func normalizedModelFailureKind(kind string) modelFailureKind {
	return modelFailureKind(modelDiagnosticIndex(kind, modelFailureKinds[:]))
}
func normalizedModelFailureReason(reason string) modelFailureReason {
	return modelFailureReason(modelDiagnosticIndex(reason, modelFailureReasons[:]))
}
func (e *modelFailureDiagnostic) stageName() string {
	return modelDiagnosticName(uint8(e.stage), modelFailureStages[:])
}
func (e *modelFailureDiagnostic) kindName() string {
	return modelDiagnosticName(uint8(e.kind), modelFailureKinds[:])
}
func (e *modelFailureDiagnostic) reasonName() string {
	return modelDiagnosticName(uint8(e.reason), modelFailureReasons[:])
}

func normalizedModelHTTPStatus(status int) int {
	if status >= 100 && status <= 599 {
		return status
	}
	return 0
}

func nativeModelTransportDiagnostic(cause error) error {
	var diagnostic *modelFailureDiagnostic
	if errors.As(cause, &diagnostic) && diagnostic != nil &&
		(diagnostic.kindName() == "NATIVE_CURRENT" || diagnostic.kindName() == "NATIVE_WIRE") {
		return diagnostic
	}
	return nil
}

func modelFailure(stage, kind string, status int, cause error) error {
	if cause == nil {
		return nil
	}
	return &modelFailureDiagnostic{
		stage: normalizedModelFailureStage(stage), kind: normalizedModelFailureKind(kind),
		status: normalizedModelHTTPStatus(status), cause: func() error { return cause },
	}
}

func tencentHTTPFailure(status int, cause error, metadata tencentHTTPMetadata) error {
	err := modelFailure("TENCENT_HTTP", "HTTP", status, cause)
	if diagnostic, ok := err.(*modelFailureDiagnostic); ok {
		diagnostic.http = metadata.normalized()
	}
	return err
}

func (e *modelFailureDiagnostic) Error() string {
	if e == nil {
		return "MODEL stage=MODEL_GATEWAY kind=UNKNOWN status=0 reason=unknown"
	}
	summary := fmt.Sprintf("MODEL stage=%s kind=%s status=%d reason=%s",
		e.stageName(), e.kindName(), normalizedModelHTTPStatus(e.status), e.reasonName())
	if metadata := e.http.normalized(); metadata.code != 0 {
		summary += fmt.Sprintf(" business=%s type=%s source=%s param=%s upstream_status=%d",
			modelDiagnosticName(uint8(metadata.code), tencentHTTPCodes[:]), modelDiagnosticName(uint8(metadata.typ), tencentHTTPTypes[:]),
			modelDiagnosticName(uint8(metadata.source), tencentHTTPSources[:]), modelDiagnosticName(uint8(metadata.param), tencentHTTPParams[:]), metadata.upstreamStatus)
	}
	return summary
}

func (e *modelFailureDiagnostic) Format(s fmt.State, _ rune) { _, _ = io.WriteString(s, e.Error()) }
func (e *modelFailureDiagnostic) Unwrap() error {
	if e == nil || e.cause == nil {
		return nil
	}
	return e.cause()
}

// WithLiveFailureReason keeps a fixed Gateway ReasonCode and the adapter's
// diagnostic stage. The original error remains reachable by errors.Is/As;
// arbitrary error text is never formatted, even with %#v or %+v.
func WithLiveFailureReason(cause error, reason string) error {
	if cause == nil {
		return nil
	}
	var original *modelFailureDiagnostic
	if errors.As(cause, &original) && original != nil {
		return &modelFailureDiagnostic{stage: normalizedModelFailureStage(original.stageName()), kind: normalizedModelFailureKind(original.kindName()), status: normalizedModelHTTPStatus(original.status),
			reason: normalizedModelFailureReason(reason), http: original.http.normalized(), cause: func() error { return cause }}
	}
	stage, kind := "MODEL_GATEWAY", "UNKNOWN"
	switch reason {
	case "live_preparation_unavailable", "invalid_context", "invalid_request":
		stage, kind = "MODEL_PREPARE", "REQUEST"
	case "live_current_authority_unavailable", "gateway_unavailable", "inference_disabled", "cancelled":
		stage, kind = "MODEL_CURRENT", "NATIVE_CURRENT"
	case "live_release_or_settlement_unknown":
		stage, kind = "MODEL_RELEASE_SETTLEMENT", "NATIVE_RELEASE_SETTLEMENT"
	case "live_wire_unavailable":
		stage, kind = "MODEL_WIRE", "NATIVE_WIRE"
	case "invalid_live_text_response":
		stage, kind = "MODEL_NORMALIZE", "RESPONSE_SHAPE"
	}
	return &modelFailureDiagnostic{stage: normalizedModelFailureStage(stage), kind: normalizedModelFailureKind(kind),
		reason: normalizedModelFailureReason(reason), cause: func() error { return cause }}
}

// SafeModelFailureSummary is intended for fixed internal server diagnostics.
// It does not expose a provider response, an endpoint, or the original cause's
// Error string. Unknown errors are left to the caller's existing redaction.
func SafeModelFailureSummary(cause error) (string, bool) {
	var diagnostic *modelFailureDiagnostic
	if errors.As(cause, &diagnostic) && diagnostic != nil {
		return diagnostic.Error(), true
	}
	var provider ProviderError
	var pointer *ProviderError
	if (errors.As(cause, &pointer) && pointer != nil) || errors.As(cause, &provider) {
		classified := normalizedProviderError(cause)
		return WithLiveFailureReason(cause, "provider_"+classified.Code).Error(), true
	}
	for _, entry := range []struct {
		cause  error
		reason string
	}{
		{ErrInvalid, "invalid_request"}, {ErrUnavailable, "gateway_unavailable"},
		{ErrAdapter, "invalid_live_text_response"}, {ErrLivePreparation, "live_preparation_unavailable"},
		{ErrDeadline, "provider_DEADLINE"},
	} {
		if errors.Is(cause, entry.cause) {
			return WithLiveFailureReason(cause, entry.reason).Error(), true
		}
	}
	return "", false
}
