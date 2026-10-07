package agentmemory

import (
	"encoding/json"
	"github.com/birdtie/birdtie/apps/api/internal/actorref"
	"strings"
	"testing"
	"time"
)

func orgRecordTest(t *testing.T, category OrganizationCategory) Record {
	t.Helper()
	now := time.Date(2026, 10, 3, 3, 0, 0, 0, time.UTC)
	kind, key, e := OrganizationMemoryKey(category, "explicit-note")
	if e != nil {
		t.Fatal(e)
	}
	r, e := NewOrganizationExplicit("74000000-0000-4000-8000-000000000001", "74000000-0000-4000-8000-000000000002", actorref.PrincipalRef{Type: actorref.Organization, ID: "74000000-0000-4000-8000-000000000003"}, 1, PutInput{ExpectedVersion: 0, MemoryType: kind, MemoryKey: key, Summary: "合成管理员声明，不是核验的合作/出席", StructuredValue: json.RawMessage(`{"note":"人工填写","tags":["合成"]}`), Visibility: VisibilityPrivate, ValidUntil: now.Add(time.Hour)}, now, now)
	if e != nil {
		t.Fatal(e)
	}
	return r
}
func TestOrganizationMemoryRecordEightKindsAndPersonalBoundary(t *testing.T) {
	for _, category := range OrganizationCategories() {
		t.Run(string(category), func(t *testing.T) {
			r := orgRecordTest(t, category)
			if ValidateOrganizationRecord(r) != nil {
				t.Fatal("Org record rejected")
			}
			if ValidateRecord(r) == nil {
				t.Fatal("Personal validator widened")
			}
			if _, e := r.OwnerRef(); e == nil {
				t.Fatal("Personal owner widened")
			}
			c, key, e := OrganizationCategoryForKey(r.MemoryType, r.MemoryKey)
			if e != nil || c != category || key != "explicit-note" {
				t.Fatal("typed namespace not reversible")
			}
			r.Status = StatusDeleted
			r.Summary = ""
			r.StructuredValue = json.RawMessage(`{}`)
			if ValidateOrganizationRecord(r) != nil {
				t.Fatal("terminal tombstone rejected")
			}
		})
	}
}
func TestOrganizationMemoryRecordRejectsForeignShapes(t *testing.T) {
	for name, change := range map[string]func(*Record){"person": func(r *Record) { r.OwnerType = actorref.Person }, "business": func(r *Record) { r.OwnerType = actorref.Business }, "inferred": func(r *Record) { r.SourceType = SourceInferred; r.Status = StatusPendingReview }, "probability": func(r *Record) { r.Confidence = .9 }, "other_type": func(r *Record) { r.MemoryType = TypeIdentity }, "person_namespace": func(r *Record) { r.MemoryKey = "person.note" }, "oversize_key": func(r *Record) { r.MemoryKey = "org.v1.faq." + strings.Repeat("x", 61) }, "reinforced": func(r *Record) { r.LastReinforcedAt = &r.UpdatedAt }, "source_selector": func(r *Record) { r.StructuredValue = json.RawMessage(`{"sourceId":"fake"}`) }, "null_note": func(r *Record) { r.StructuredValue = json.RawMessage(`{"note":null}`) }, "null_tags": func(r *Record) { r.StructuredValue = json.RawMessage(`{"tags":null}`) }, "mixed_tags": func(r *Record) { r.StructuredValue = json.RawMessage(`{"tags":[1]}`) }, "pending": func(r *Record) { r.Status = StatusPendingReview }} {
		t.Run(name, func(t *testing.T) {
			r := orgRecordTest(t, OrgFAQ)
			change(&r)
			if ValidateOrganizationRecord(r) == nil {
				t.Fatal("unsupported Org shape accepted")
			}
		})
	}
	now := time.Now().UTC()
	if _, e := NewExplicit("74000000-0000-4000-8000-000000000001", "74000000-0000-4000-8000-000000000002", actorref.PrincipalRef{Type: actorref.Organization, ID: "74000000-0000-4000-8000-000000000003"}, 1, PutInput{MemoryType: TypeOrganization, MemoryKey: "org.v1.faq.note", Summary: "explicit", StructuredValue: json.RawMessage(`{}`), Visibility: VisibilityPrivate, ValidUntil: now.Add(time.Hour)}, now, now); e == nil {
		t.Fatal("Personal constructor widened")
	}
}
