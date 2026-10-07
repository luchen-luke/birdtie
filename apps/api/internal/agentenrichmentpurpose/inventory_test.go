package agentenrichmentpurpose

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/actorref"
)

func inventoryUnitFixture() Inventory {
	at := time.Date(2026, 10, 7, 0, 0, 0, 0, time.UTC)
	owner := actorref.PrincipalRef{Type: actorref.Person, ID: "49000000-0000-4000-8000-000000000001"}
	g := Grant{SchemaVersion: Schema, ID: "49000000-0000-4000-8000-000000000002", PreviewID: "49000000-0000-4000-8000-000000000003", Purpose: Purpose, Owner: owner, AgentID: "49000000-0000-4000-8000-000000000004", Revision: 1, CreatedAt: at.Add(-time.Minute), ExpiresAt: at.Add(time.Minute), ObservedAt: at,
		Selection: Selection{TaskID: "49000000-0000-4000-8000-000000000005", MomentID: "49000000-0000-4000-8000-000000000006", MomentRevision: 2, Fields: []string{"title"}, DeadlineAt: at.Add(time.Minute)}}
	return Inventory{SchemaVersion: InventorySchema, Owner: owner, AgentID: g.AgentID, ObservedAt: at, ValidUntil: at.Add(30 * time.Second), Limit: InventoryLimit, Grants: []Grant{g}}
}

func TestEnrichmentInventoryShapeClosedMetadata(t *testing.T) {
	v := inventoryUnitFixture()
	if ValidateInventory(v) != nil {
		t.Fatal("valid original grant metadata denied")
	}
	raw, _ := json.Marshal(v)
	for _, forbidden := range []string{"content", "taskQuery", "source_binding", "authority", "sessionDigest", "review"} {
		if strings.Contains(string(raw), `"`+forbidden+`"`) {
			t.Fatal("non-metadata exported", forbidden)
		}
	}
	for _, tc := range []struct {
		name   string
		change func(*Inventory)
	}{
		{"different owner", func(v *Inventory) { v.Owner.ID = v.AgentID }},
		{"different Agent", func(v *Inventory) { v.AgentID = v.Owner.ID }},
		{"unknown purpose", func(v *Inventory) { v.Grants[0].Purpose = "MODEL_EGRESS" }},
		{"model authority", func(v *Inventory) { v.Grants[0].ModelAccess = true }},
		{"retention authority", func(v *Inventory) { v.Grants[0].CandidateRetentionAllowed = true }},
		{"snapshot clock replaced", func(v *Inventory) { v.Grants[0].ObservedAt = v.ObservedAt.Add(time.Second) }},
		{"read lease extended", func(v *Inventory) { v.ValidUntil = v.ValidUntil.Add(time.Second) }},
		{"fake truncation", func(v *Inventory) { v.Truncated = true }},
		{"duplicate grant", func(v *Inventory) { v.Grants = append(v.Grants, v.Grants[0]) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			v := inventoryUnitFixture()
			tc.change(&v)
			if ValidateInventory(v) == nil {
				t.Fatal("invalid metadata accepted")
			}
		})
	}
}

func TestEnrichmentInventoryExpiredAndRevokedAreVisibleHistory(t *testing.T) {
	v := inventoryUnitFixture()
	v.Grants[0].ExpiresAt = v.ObservedAt.Add(-time.Second)
	v.Grants[0].Selection.DeadlineAt = v.Grants[0].ExpiresAt
	revoked := v.ObservedAt.Add(-2 * time.Second)
	v.Grants[0].RevokedAt = &revoked
	v.Grants[0].Revision = 2
	if ValidateInventory(v) != nil {
		t.Fatal("expired/withdrawn history incorrectly needs active analysis approval")
	}
	v.Grants = []Grant{}
	if ValidateInventory(v) != nil {
		t.Fatal("real empty inventory rejected")
	}
}
