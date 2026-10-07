package agentenrichmentpurpose

import (
	"bytes"
	"encoding/json"
	"errors"
	"github.com/birdtie/birdtie/apps/api/internal/actorref"
	"testing"
	"time"
)

func validPurposeSelection() Selection {
	return Selection{TaskID: "11111111-1111-4111-8111-111111111111", MomentID: "22222222-2222-4222-8222-222222222222", MomentRevision: 1, Fields: []string{"title", "body"}, DeadlineAt: time.Now().UTC().Truncate(time.Microsecond).Add(time.Minute)}
}
func TestEnrichmentPurposeShape(t *testing.T) {
	s, e := Normalize(validPurposeSelection())
	if e != nil || s.Fields[0] != "body" {
		t.Fatal(s, e)
	}
	for _, change := range []func(*Selection){func(s *Selection) { s.Fields = []string{"body", "body"} }, func(s *Selection) { s.Fields = []string{"media"} }, func(s *Selection) { s.Fields = nil }, func(s *Selection) { s.MomentRevision = 0 }, func(s *Selection) { s.TaskID = "" }, func(s *Selection) { s.MomentID = "00000000-0000-0000-0000-000000000000" }, func(s *Selection) { s.DeadlineAt = time.Time{} }} {
		s := validPurposeSelection()
		change(&s)
		if _, e := Normalize(s); !errors.Is(e, ErrInvalid) {
			t.Fatal(s, e)
		}
	}
}
func TestEnrichmentPurposeStrictWire(t *testing.T) {
	for _, raw := range []string{`{"id":"a","id":"b"}`, `{"id":null}`, `{"id":"a","confirmed":true}`, `{"id":"a"} {}`, `[]`, `{}`} {
		if _, e := StrictObject([]byte(raw), "id"); !errors.Is(e, ErrInvalid) {
			t.Fatal(raw, e)
		}
	}
	if _, e := StrictObject([]byte(`{"id":"a"}`), "id"); e != nil {
		t.Fatal(e)
	}
}
func TestEnrichmentPurposeResolutionCannotGrantFromWire(t *testing.T) {
	if _, e := json.Marshal(Resolution{}); !errors.Is(e, ErrServerOnly) {
		t.Fatal(e)
	}
	var r Resolution
	if e := json.Unmarshal([]byte(`{}`), &r); !errors.Is(e, ErrServerOnly) {
		t.Fatal(e)
	}
}

func TestEnrichmentPreviewReceiptPureClosedMetadata(t *testing.T) {
	now := time.Now().UTC()
	id := validPurposeSelection().TaskID
	good := PreviewReceipt{SchemaVersion: "agent-enrichment-purpose-preview-receipt-v1", PreviewID: id, Purpose: Purpose, Owner: actorref.PrincipalRef{Type: actorref.Person, ID: id}, AgentID: id, State: "OPEN_UNCONSUMED", PreviewObservedAt: now, PreviewExpiresAt: now.Add(time.Minute), ObservedAt: now, ValidUntil: now.Add(30 * time.Second)}
	if ValidatePreviewReceipt(good, Purpose) != nil {
		t.Fatal("valid metadata rejected")
	}
	for _, change := range []func(*PreviewReceipt){func(v *PreviewReceipt) { v.ModelAccess = true }, func(v *PreviewReceipt) { v.State = "CLOSED_UNCONSUMED" }, func(v *PreviewReceipt) { v.ConsumedGrantID = id }, func(v *PreviewReceipt) { v.ValidUntil = v.ObservedAt.Add(time.Minute) }, func(v *PreviewReceipt) { v.Owner.Type = actorref.Organization }, func(v *PreviewReceipt) { v.Purpose = "TASK_CONTEXT_READ" }, func(v *PreviewReceipt) { v.State = "APPROVAL_RECORDED" }} {
		v := good
		change(&v)
		if ValidatePreviewReceipt(v, Purpose) == nil {
			t.Fatal("invalid metadata accepted")
		}
	}
	good.State = "CLOSED_UNCONSUMED"
	good.ObservedAt = good.PreviewExpiresAt
	good.ValidUntil = good.ObservedAt.Add(time.Second)
	if ValidatePreviewReceipt(good, Purpose) != nil {
		t.Fatal("past original deadline metadata denied")
	}
	raw, _ := json.Marshal(good)
	for _, bad := range []string{"selection", "review", "query", "sourceBinding", "authority", "session", "token"} {
		if bytes.Contains(raw, []byte(`"`+bad+`"`)) {
			t.Fatal("bodyless metadata includes prohibited field")
		}
	}
}
