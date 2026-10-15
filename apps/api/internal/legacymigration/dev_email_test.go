package legacymigration

import (
	"strings"
	"testing"
)

func TestDevEmailSelectionRequiresExactUniqueEmailOnlyOwners(t *testing.T) {
	first, second := "16400000-0000-4000-8000-000000000001", "16400000-0000-4000-8000-000000000002"
	p := DevEmailPlan{Original: Plan{Owners: []OwnerPlan{{SourceID: first, SourceStatus: "active", SourceVersion: 1, SourceSHA256: strings.Repeat("a", 64)}, {SourceID: second, SourceStatus: "active", SourceVersion: 1, SourceSHA256: strings.Repeat("b", 64)}}}}
	users := []any{map[string]any{"id": first, "email": "a@example.test"}, map[string]any{"id": second, "email": "b@example.test"}}
	a := map[string]any{"id": "16400000-0000-4000-8000-000000000003", "user_id": first, "provider": "email", "subject": " A@EXAMPLE.TEST ", "verified_at": "2026-10-15T00:00:00Z"}
	b := map[string]any{"id": "16400000-0000-4000-8000-000000000004", "user_id": second, "provider": "email", "subject": "b@example.test", "verified_at": "2026-10-15T00:00:00Z"}
	tables := map[string]any{"users": users, "auth_identities": []any{a, b}}
	if e := selectDevEmails(&p, tables); e != nil || len(p.Bindings) != 2 || p.Counts.EmailOnlyOwners != 2 {
		t.Fatal("two exact owners not selected", e)
	}
	b["subject"] = "a@example.test"
	bad := DevEmailPlan{Original: p.Original}
	if selectDevEmails(&bad, tables) == nil {
		t.Fatal("ambiguous email owner accepted")
	}
	b["subject"] = "b@example.test"
	b["verified_at"] = nil
	bad = DevEmailPlan{Original: p.Original}
	if selectDevEmails(&bad, tables) == nil {
		t.Fatal("unverified source identity accepted")
	}
	b["verified_at"] = "2026-10-15T00:00:00Z"
	users[1].(map[string]any)["email"] = "different@example.test"
	bad = DevEmailPlan{Original: p.Original}
	if selectDevEmails(&bad, tables) == nil {
		t.Fatal("name or unrelated email guessed")
	}
	if _, e := BuildDevEmailPlan("16400000-0000-4000-8000-000000000001", []byte("{}"), []byte("{}")); e == nil {
		t.Fatal("unverified original snapshot accepted")
	}
}
