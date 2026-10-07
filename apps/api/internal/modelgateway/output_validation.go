package modelgateway

import (
	"encoding/json"
	"errors"
	"unicode/utf8"
)

// These are fixed classifications, never provider text or permission grants.
var (
	ErrOutputRefused   = errors.New("model output refused")
	ErrOutputTruncated = errors.New("model output truncated")
	ErrOutputSchema    = errors.New("model output schema invalid")
	ErrOutputEntity    = errors.New("model output entity outside current source scope")
	ErrOutputSource    = errors.New("model output source changed or unavailable")
	ErrOutputRepair    = errors.New("model output repair limit exhausted")
)

type outputSyntaxError struct{ repairable bool }

func (outputSyntaxError) Error() string   { return "invalid model output JSON" }
func (outputSyntaxError) Unwrap() []error { return []error{ErrAdapter, ErrOutputSchema} }

// Only a bounded actual malformed response receives this private marker. A
// valid JSON object with extra/authority/tool fields is never format-repairable.
func invalidOutputJSON(raw []byte) error {
	return outputSyntaxError{len(raw) > 0 && len(raw) <= MaxResultBytes && utf8.Valid(raw) && !json.Valid(raw)}
}

func RepairableOutputJSON(err error) bool {
	var syntax outputSyntaxError
	return errors.As(err, &syntax) && syntax.repairable
}

// ValidateEntityScope is shape/semantic matching, not a source resolver. Its
// allowlist must come from the current native domain projection. The actual
// native release also revalidates that projection, owner, versions and expiry.
func ValidateEntityScope(result Result, allowed []EntityProposal) error {
	set := make(map[EntityProposal]bool, len(allowed))
	for _, ref := range allowed {
		if ref.Type != "ACTIVITY" || !validUUID(ref.ID) || set[ref] {
			return ErrOutputSource
		}
		set[ref] = true
	}
	if result.Candidate != nil || len(result.ToolProposals) != 0 {
		return ErrOutputEntity
	}
	if result.Status != Completed {
		if result.Answer != nil || result.Text != "" {
			return ErrOutputSchema
		}
		return nil
	}
	if result.Answer == nil {
		return nil // Original scalar TEXT has no entity authority.
	}
	seen := map[EntityProposal]bool{}
	for _, ref := range result.Answer.EntityRefs {
		if !set[ref] || seen[ref] {
			return ErrOutputEntity
		}
		seen[ref] = true
	}
	return nil
}
