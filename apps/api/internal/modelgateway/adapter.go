package modelgateway

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"time"
)

// ProviderRequest excludes native IDs/policy/budget/context selectors. These
// fields are provider-neutral and cannot approve tools or select an owner.
type ProviderRequest struct {
	TaskKind            TaskKind   `json:"task_kind"`
	PromptVersion       string     `json:"prompt_version"`
	Messages            []Message  `json:"messages"`
	OutputMode          OutputMode `json:"output_mode"`
	OutputSchemaVersion string     `json:"output_schema_version"`
	ToolAllowlist       []string   `json:"tool_allowlist"`
	MaxOutputTokens     int        `json:"max_output_tokens"`
	DeadlineAt          time.Time  `json:"deadline_at"`
}

type ProviderDescriptor struct {
	ProviderID   string
	ModelID      string
	ModelVersion string
	Mode         ExecutionMode
}

// ProviderAdapter performs only request/response translation. It has no source,
// actor resolver, tool executor, Memory, billing authorization or trace method.
// Current implementations are synthetic test adapters; no SDK is installed.
type ProviderAdapter interface {
	Descriptor() ProviderDescriptor
	Complete(context.Context, ProviderRequest) ([]byte, error)
}

type ProviderError struct {
	Code      string
	Retryable bool
}

func (e ProviderError) Error() string { return "model provider error: " + e.Code }

func validDescriptor(d ProviderDescriptor) bool {
	return d.Mode == OfflineContract && identifier.MatchString(d.ProviderID) && identifier.MatchString(d.ModelID) &&
		identifier.MatchString(d.ModelVersion) && !validUUID(d.ProviderID) && !validUUID(d.ModelID) && !validUUID(d.ModelVersion)
}

func providerRequest(r Request) ProviderRequest {
	return ProviderRequest{TaskKind: r.TaskKind, PromptVersion: r.PromptVersion, Messages: append([]Message{}, r.Messages...),
		OutputMode: r.OutputMode, OutputSchemaVersion: r.OutputSchemaVersion, ToolAllowlist: append([]string{}, r.ToolAllowlist...),
		MaxOutputTokens: r.Budget.MaxOutputTokens, DeadlineAt: r.DeadlineAt}
}

func adapterPresent(a ProviderAdapter) bool {
	if a == nil {
		return false
	}
	v := reflect.ValueOf(a)
	switch v.Kind() {
	case reflect.Pointer, reflect.Interface, reflect.Map, reflect.Slice, reflect.Func, reflect.Chan:
		return !v.IsNil()
	}
	return true
}

func normalizedProviderError(err error) ProviderError {
	if errors.Is(err, context.DeadlineExceeded) {
		return ProviderError{"DEADLINE", false}
	}
	if errors.Is(err, context.Canceled) {
		return ProviderError{"CANCELLED", false}
	}
	var provider ProviderError
	var pointer *ProviderError
	if errors.As(err, &pointer) && pointer != nil {
		provider = *pointer
	} else if !errors.As(err, &provider) {
		return ProviderError{"UNKNOWN", false}
	}
	{
		switch provider.Code {
		case "RATE_LIMIT", "TEMPORARY":
			return ProviderError{provider.Code, true}
		case "REFUSED", "AUTHENTICATION", "INVALID_REQUEST":
			return ProviderError{provider.Code, false}
		}
	}
	return ProviderError{"UNKNOWN", false}
}

func parseUsage(raw json.RawMessage, maxOutput int) (Usage, error) {
	u := Usage{Status: "UNKNOWN", CostStatus: "UNKNOWN"}
	if len(raw) == 0 {
		return u, nil
	}
	obj, err := strictObject(raw, []string{"status"}, []string{"input_tokens", "output_tokens"})
	if err != nil {
		return Usage{}, err
	}
	if json.Unmarshal(obj["status"], &u.Status) != nil {
		return Usage{}, ErrAdapter
	}
	if u.Status == "UNKNOWN" {
		if len(obj) != 1 {
			return Usage{}, ErrAdapter
		}
		return u, nil
	}
	if u.Status != "KNOWN" || len(obj) != 3 || json.Unmarshal(obj["input_tokens"], &u.InputTokens) != nil || json.Unmarshal(obj["output_tokens"], &u.OutputTokens) != nil ||
		u.InputTokens == nil || u.OutputTokens == nil || *u.InputTokens < 0 || *u.InputTokens > 1_000_000 || *u.OutputTokens < 0 || *u.OutputTokens > int64(maxOutput) {
		return Usage{}, ErrAdapter
	}
	return u, nil
}

func normalizeResponse(r Request, d ProviderDescriptor, raw []byte) (Result, error) {
	result, err := parseNormalizedResponse(r, d, raw)
	if err != nil && errors.Is(err, ErrAdapter) && !errors.Is(err, ErrOutputSchema) {
		return result, errors.Join(ErrAdapter, ErrOutputSchema)
	}
	return result, err
}

func parseNormalizedResponse(r Request, d ProviderDescriptor, raw []byte) (Result, error) {
	result := emptyResult(r, OfflineContract, Invalid, "invalid_adapter_response")
	obj, err := strictObject(raw, []string{"status", "request_id", "finish_reason"}, []string{"text", "structured", "tool_proposals", "usage"})
	if err != nil {
		return result, invalidOutputJSON(raw)
	}
	var status Status
	if json.Unmarshal(obj["status"], &status) != nil {
		return result, ErrAdapter
	}
	requestID, err := stringValue(obj["request_id"], 80)
	if err != nil || !safeRequestID.MatchString(requestID) {
		return result, ErrAdapter
	}
	finish, err := stringValue(obj["finish_reason"], 40)
	if err != nil {
		return result, ErrAdapter
	}
	u, err := parseUsage(obj["usage"], r.Budget.MaxOutputTokens)
	if err != nil {
		return result, ErrAdapter
	}
	if status != Completed && status != Refused && status != Truncated && status != Unavailable {
		return result, ErrAdapter
	}
	if status == Refused || status == Unavailable {
		if obj["text"] != nil || obj["structured"] != nil || obj["tool_proposals"] != nil || ((status == Refused && finish != "refusal") || (status == Unavailable && finish != "unavailable")) {
			return result, ErrAdapter
		}
	} else {
		if (status == Completed && finish != "stop") || (status == Truncated && finish != "length") {
			return result, ErrAdapter
		}
		if status == Truncated {
			// No partial structured object or tool proposal is released for a
			// truncated completion; callers cannot accidentally execute it.
			if obj["structured"] != nil || obj["tool_proposals"] != nil {
				return result, ErrAdapter
			}
			if obj["text"] != nil {
				if _, err = stringValue(obj["text"], 8192); err != nil {
					return result, ErrAdapter
				}
			}
		} else {
			switch r.OutputMode {
			case Text:
				if obj["structured"] != nil || obj["tool_proposals"] != nil {
					return result, ErrAdapter
				}
				result.Text, err = stringValue(obj["text"], 8192)
			case Structured:
				if obj["text"] != nil || obj["tool_proposals"] != nil {
					return result, ErrAdapter
				}
				if r.TaskKind == MemoryCandidateExtraction {
					result.Candidate, err = decodeCandidate(obj["structured"])
				} else {
					result.Answer, err = decodeAnswer(obj["structured"])
				}
			case ToolProposals:
				if obj["text"] != nil || obj["structured"] != nil {
					return result, ErrAdapter
				}
				result.ToolProposals, err = decodeTools(obj["tool_proposals"], r.ToolAllowlist)
			default:
				return result, ErrAdapter
			}
			if err != nil {
				return emptyResult(r, OfflineContract, Invalid, "invalid_adapter_response"), ErrAdapter
			}
		}
	}
	result.Status = status
	result.Usage = u
	result.ProviderID = d.ProviderID
	result.ProviderModelVersion = d.ModelID + "@" + d.ModelVersion
	result.ProviderRequestID = requestID
	result.FinishReason = finish
	result.ReasonCode = "synthetic_adapter_contract_only"
	return result, nil
}
