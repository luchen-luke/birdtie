package modelgateway

import (
	"bytes"
	"encoding/json"
	"io"
	"strings"
	"time"
	"unicode/utf8"
)

// strictObject rejects duplicate/case alias keys instead of last-value-wins.
// Every decoded nested object is subsequently passed through the same helper.
func strictObject(data []byte, required []string, optional []string) (map[string]json.RawMessage, error) {
	if len(data) == 0 || len(data) > MaxResultBytes || !utf8.Valid(data) {
		return nil, ErrAdapter
	}
	d := json.NewDecoder(bytes.NewReader(data))
	d.UseNumber()
	token, err := d.Token()
	if err != nil || token != json.Delim('{') {
		return nil, ErrAdapter
	}
	allowed := map[string]bool{}
	for _, key := range required {
		allowed[key] = true
	}
	for _, key := range optional {
		allowed[key] = true
	}
	values := map[string]json.RawMessage{}
	for d.More() {
		token, err = d.Token()
		key, ok := token.(string)
		if err != nil || !ok || !allowed[key] || values[key] != nil {
			return nil, ErrAdapter
		}
		var raw json.RawMessage
		if d.Decode(&raw) != nil {
			return nil, ErrAdapter
		}
		values[key] = raw
	}
	if token, err = d.Token(); err != nil || token != json.Delim('}') {
		return nil, ErrAdapter
	}
	if _, err = d.Token(); err != io.EOF {
		return nil, ErrAdapter
	}
	for _, key := range required {
		if values[key] == nil || bytes.Equal(values[key], []byte("null")) {
			return nil, ErrAdapter
		}
	}
	return values, nil
}

func stringValue(raw json.RawMessage, max int) (string, error) {
	var v string
	if json.Unmarshal(raw, &v) != nil || strings.TrimSpace(v) == "" || len(v) > max || strings.ContainsRune(v, 0) {
		return "", ErrAdapter
	}
	return v, nil
}

// DecodeRequest is the bounded/closed internal wire parser. Its references are
// selectors only, not BoundaryFacts. No HTTP route accepts this request yet.
func DecodeRequest(data []byte, now time.Time) (Request, error) {
	var r Request
	if len(data) > MaxRequestBytes {
		return r, ErrInvalid
	}
	obj, err := strictObject(data, []string{"schema_version", "run_id", "agent_ref", "task_kind", "prompt_version", "input_schema_version", "output_schema_version", "context_snapshot_ref", "data_policy_ref", "budget_ref", "budget", "messages", "output_mode", "tool_allowlist", "capabilities_required", "deadline_at"}, nil)
	if err != nil {
		return r, ErrInvalid
	}
	a, err := strictObject(obj["agent_ref"], []string{"AgentID", "Principal", "Role"}, nil)
	if err != nil {
		return r, ErrInvalid
	}
	if _, err = strictObject(a["Principal"], []string{"type", "id"}, nil); err != nil {
		return r, ErrInvalid
	}
	if _, err = strictObject(obj["budget"], []string{"max_output_tokens"}, nil); err != nil {
		return r, ErrInvalid
	}
	var messages []json.RawMessage
	if json.Unmarshal(obj["messages"], &messages) != nil {
		return r, ErrInvalid
	}
	for _, m := range messages {
		if _, err = strictObject(m, []string{"role", "content"}, nil); err != nil {
			return r, ErrInvalid
		}
	}
	type wireRequest Request
	var wire wireRequest
	if json.Unmarshal(data, &wire) != nil {
		return Request{}, ErrInvalid
	}
	r = Request(wire)
	if ValidateRequest(r, now) != nil {
		return Request{}, ErrInvalid
	}
	return r, nil
}

// UnmarshalJSON keeps standard callers on the same closed parser. Historical
// fixtures requiring a supplied clock use DecodeRequest explicitly instead.
func (r *Request) UnmarshalJSON(data []byte) error {
	if r == nil {
		return ErrInvalid
	}
	*r = Request{}
	parsed, err := DecodeRequest(data, time.Now())
	if err != nil {
		return err
	}
	*r = parsed
	return nil
}

func decodeAnswer(data []byte) (*AnswerProposal, error) {
	o, err := strictObject(data, []string{"answer", "entity_refs"}, nil)
	if err != nil {
		return nil, err
	}
	answer, err := stringValue(o["answer"], 8192)
	if err != nil {
		return nil, err
	}
	var rawRefs []json.RawMessage
	if json.Unmarshal(o["entity_refs"], &rawRefs) != nil || rawRefs == nil || len(rawRefs) > 32 {
		return nil, ErrAdapter
	}
	result := &AnswerProposal{Answer: answer, EntityRefs: []EntityProposal{}}
	seen := map[string]bool{}
	for _, raw := range rawRefs {
		obj, err := strictObject(raw, []string{"type", "id"}, nil)
		if err != nil {
			return nil, err
		}
		var ref EntityProposal
		if json.Unmarshal(raw, &ref) != nil || ref.Type != "ACTIVITY" || !validUUID(ref.ID) || seen[ref.ID] {
			return nil, ErrAdapter
		}
		seen[ref.ID] = true
		_ = obj
		result.EntityRefs = append(result.EntityRefs, ref)
	}
	return result, nil
}

func decodeCandidate(data []byte) (*CandidateProposal, error) {
	if _, err := strictObject(data, []string{"status", "predicate", "value", "source_ref_ids", "subject_attribution"}, nil); err != nil {
		return nil, err
	}
	var c CandidateProposal
	if json.Unmarshal(data, &c) != nil || c.Status != "CANDIDATE" || c.Predicate != "ACTIVITY_CATEGORY" ||
		strings.TrimSpace(c.Value) == "" || len(c.Value) > 160 || c.SubjectAttribution != "UNVERIFIED" || len(c.SourceRefIDs) < 1 || len(c.SourceRefIDs) > 8 {
		return nil, ErrAdapter
	}
	// Reuse SAF004's current narrow non-sensitive category vocabulary, not a
	// new permissive generic personal-trait inference vocabulary.
	switch c.Value {
	case "badminton", "basketball", "football", "sports", "culture":
	default:
		return nil, ErrAdapter
	}
	seen := map[string]bool{}
	for _, id := range c.SourceRefIDs {
		if !validUUID(id) || seen[id] {
			return nil, ErrAdapter
		}
		seen[id] = true
	}
	return &c, nil
}

func decodeTools(data []byte, allowlist []string) ([]ToolProposal, error) {
	var raws []json.RawMessage
	if json.Unmarshal(data, &raws) != nil || len(raws) < 1 || len(raws) > 4 {
		return nil, ErrAdapter
	}
	allowed := map[string]bool{}
	for _, tool := range allowlist {
		allowed[tool] = true
	}
	proposals := []ToolProposal{}
	for _, raw := range raws {
		obj, err := strictObject(raw, []string{"tool", "arguments", "reason_summary"}, nil)
		if err != nil {
			return nil, err
		}
		tool, err := stringValue(obj["tool"], 80)
		if err != nil || !allowed[tool] {
			return nil, ErrAdapter
		}
		var args map[string]json.RawMessage
		switch tool {
		case "activity.search":
			args, err = strictObject(obj["arguments"], []string{"query"}, []string{"city_id"})
		case "activity.detail":
			args, err = strictObject(obj["arguments"], []string{"activity_id"}, nil)
		default:
			return nil, ErrAdapter
		}
		if err != nil {
			return nil, err
		}
		decoded := map[string]string{}
		for k, v := range args {
			s, err := stringValue(v, 240)
			if err != nil {
				return nil, err
			}
			decoded[k] = s
		}
		if tool == "activity.detail" && !validUUID(decoded["activity_id"]) {
			return nil, ErrAdapter
		}
		if city := decoded["city_id"]; len(city) > 80 || strings.Contains(city, "://") {
			return nil, ErrAdapter
		}
		reason, err := stringValue(obj["reason_summary"], 500)
		if err != nil {
			return nil, err
		}
		proposals = append(proposals, ToolProposal{Tool: tool, Arguments: decoded, ReasonSummary: reason})
	}
	return proposals, nil
}
