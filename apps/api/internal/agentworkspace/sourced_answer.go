package agentworkspace

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/url"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

var ErrSourcedAnswer = errors.New("sourced answer binding invalid")

// AnswerSource is an untrusted, read-only reference. It is never an entity,
// coordinate, action descriptor, source-reading grant or model-egress approval.
type AnswerSource struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	URL         string `json:"url"`
	Site        string `json:"site,omitempty"`
	PublishedAt string `json:"publishedAt,omitempty"`
}

// SourcedAnswerBinding describes one already-authorized reply. Digest fields
// are correlation data; the native caller must still revalidate its authority,
// source receipt, model run and egress/budget fences before sending any bytes.
type SourcedAnswerBinding struct {
	TaskID               string    `json:"taskId"`
	RequestID            string    `json:"requestId"`
	CurrentQueryDigest   string    `json:"currentQueryDigest"`
	TaskSnapshotDigest   string    `json:"taskSnapshotDigest"`
	SourceEvidenceDigest string    `json:"sourceEvidenceDigest"`
	RunID                string    `json:"runId"`
	GeneratedAt          time.Time `json:"generatedAt"`
	ValidUntil           time.Time `json:"validUntil"`
}

// SourcedAnswer is process-local presentation data, not a reconstructed JSON
// assertion or permission. All caller-owned data is copied at construction.
type SourcedAnswer struct {
	binding SourcedAnswerBinding
	message string
	sources []AnswerSource
	valid   bool
}

func (*SourcedAnswer) UnmarshalJSON([]byte) error   { return ErrSourcedAnswer }
func (*SourcedAnswer) MarshalJSON() ([]byte, error) { return nil, ErrSourcedAnswer }

func answerText(value string, max int, multiline bool) bool {
	if value == "" || strings.TrimSpace(value) != value || !utf8.ValidString(value) || len(value) > max {
		return false
	}
	for _, r := range value {
		if unicode.IsControl(r) && !(multiline && (r == '\n' || r == '\r' || r == '\t')) {
			return false
		}
	}
	return true
}

func answerDigest(value string) bool {
	decoded, err := hex.DecodeString(value)
	return err == nil && len(decoded) == 32 && value == strings.ToLower(value) && value != strings.Repeat("0", 64)
}

func validAnswerURL(value string) bool {
	if !answerText(value, 2048, false) || strings.ContainsAny(value, "\\ \t\r\n") {
		return false
	}
	u, err := url.Parse(value)
	return err == nil && (u.Scheme == "https" || u.Scheme == "http") && u.Hostname() != "" && u.User == nil && u.Opaque == ""
}

func NewSourcedAnswer(binding SourcedAnswerBinding, message string, sources []AnswerSource) (*SourcedAnswer, error) {
	if !answerText(binding.TaskID, 180, false) || !answerText(binding.RequestID, 180, false) ||
		!answerText(binding.RunID, 180, false) || !answerDigest(binding.CurrentQueryDigest) ||
		!answerDigest(binding.TaskSnapshotDigest) || !answerDigest(binding.SourceEvidenceDigest) ||
		binding.GeneratedAt.IsZero() || !binding.ValidUntil.After(binding.GeneratedAt) ||
		binding.ValidUntil.Sub(binding.GeneratedAt) > 15*time.Minute ||
		!answerText(message, 8192, true) || len(sources) == 0 || len(sources) > 10 {
		return nil, ErrSourcedAnswer
	}
	seenIDs, seenURLs := map[string]bool{}, map[string]bool{}
	for _, source := range sources {
		if !answerText(source.ID, 64, false) || !answerText(source.Title, 1024, false) || !validAnswerURL(source.URL) ||
			(source.Site != "" && !answerText(source.Site, 512, false)) ||
			(source.PublishedAt != "" && !answerText(source.PublishedAt, 128, false)) ||
			seenIDs[source.ID] || seenURLs[source.URL] {
			return nil, ErrSourcedAnswer
		}
		seenIDs[source.ID], seenURLs[source.URL] = true, true
	}
	binding.GeneratedAt = binding.GeneratedAt.UTC()
	binding.ValidUntil = binding.ValidUntil.UTC()
	return &SourcedAnswer{binding: binding, message: message, sources: append([]AnswerSource(nil), sources...), valid: true}, nil
}

// SourcedAnswerQueryDigest hashes only the current user question. Conversation
// is checked for consistency and is not included in the exported digest.
func SourcedAnswerQueryDigest(task Task) (string, error) {
	query := task.Query
	current, hasCurrent := task.Filters["currentQuery"]
	if hasCurrent {
		query = current
	}
	if !answerText(strings.TrimSpace(query), 4096, true) {
		return "", ErrSourcedAnswer
	}
	lastUser := ""
	for _, m := range task.Conversation {
		if m.Role == "user" {
			lastUser = m.Text
		}
	}
	if (hasCurrent && lastUser == "") || (lastUser != "" && strings.TrimSpace(lastUser) != strings.TrimSpace(query)) {
		return "", ErrSourcedAnswer
	}
	d := sha256.Sum256([]byte("birdtie.sourced-answer.query.v1\x00" + strings.TrimSpace(query)))
	return hex.EncodeToString(d[:]), nil
}

func SourcedAnswerTaskDigest(task Task) (string, error) {
	raw, err := json.Marshal(task)
	if err != nil || task.ID == "" {
		return "", ErrSourcedAnswer
	}
	d := sha256.Sum256(append([]byte("birdtie.sourced-answer.task.v1\x00"), raw...))
	return hex.EncodeToString(d[:]), nil
}

// ApplySourcedAnswer only decorates the already-projected response. It must be
// called after native preparation and before the existing final revalidation.
// It deliberately does not update the Task, native Items, entities or map.
func ApplySourcedAnswer(result Results, task Task, requestID string, answer *SourcedAnswer, now time.Time) (Results, error) {
	if answer == nil || !answer.valid || now.IsZero() {
		return result, ErrSourcedAnswer
	}
	b := answer.binding
	queryDigest, queryErr := SourcedAnswerQueryDigest(task)
	taskDigest, taskErr := SourcedAnswerTaskDigest(task)
	if queryErr != nil || taskErr != nil || b.TaskID != task.ID || b.RequestID != requestID ||
		b.CurrentQueryDigest != queryDigest || b.TaskSnapshotDigest != taskDigest ||
		now.Before(b.GeneratedAt) || !now.Before(b.ValidUntil) ||
		result.RequestID != requestID || result.ConversationID != task.ID || result.ResultSet.TaskID != task.ID ||
		result.ResultSet.ID == "" || result.ResultSet.GeneratedAt.IsZero() ||
		!result.NativeProjection || !result.ResultSet.GeneratedAt.Equal(task.UpdatedAt) ||
		(result.ResultSet.Status != "ready" && result.ResultSet.Status != "empty") {
		return result, ErrSourcedAnswer
	}
	result.Message = answer.message
	result.ResultSet.Sources = append([]AnswerSource(nil), answer.sources...)
	binding := b
	result.ResultSet.AnswerBinding = &binding
	return result, nil
}
