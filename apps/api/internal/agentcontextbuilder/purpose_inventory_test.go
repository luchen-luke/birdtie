package agentcontextbuilder

import (
	"encoding/json"
	"github.com/birdtie/birdtie/apps/api/internal/actorref"
	"github.com/birdtie/birdtie/apps/api/internal/agentpolicysettings"
	"strings"
	"testing"
	"time"
)

func taskInventoryFixture() PurposeInventory {
	at := time.Date(2026, 10, 7, 0, 0, 0, 0, time.UTC)
	return PurposeInventory{SchemaVersion: PurposeInventorySchema, Owner: actorref.PrincipalRef{Type: actorref.Person, ID: "81000000-0000-4000-8000-000000000001"}, AgentID: "81000000-0000-4000-8000-000000000002", ObservedAt: at, ValidUntil: at.Add(30 * time.Second), Limit: 50, Grants: []PurposeInventoryGrant{{ID: "81000000-0000-4000-8000-000000000003", Revision: 1, Purpose: TaskContextRead, TaskID: "81000000-0000-4000-8000-000000000004", CityID: "aberdeen", TaskUpdatedAt: at.Add(-time.Minute), ProfileFields: []string{"personalPreferences"}, MemoryIDs: []string{}, PlaceIDs: []string{}, ActivityIDs: []string{}, RelationshipTieIDs: []string{}, PolicyFamilies: []agentpolicysettings.Family{}, CreatedAt: at.Add(-time.Second), ExpiresAt: at.Add(time.Minute)}}}
}
func TestTaskContextInventoryMetadataNoAuthority(t *testing.T) {
	v := taskInventoryFixture()
	if ValidatePurposeInventory(v) != nil {
		t.Fatal("valid metadata denied")
	}
	raw, _ := json.Marshal(v)
	for _, forbidden := range []string{"queryDigest", "currentQuery", "rowToken", "authority", "review", "session", "modelAccess"} {
		if strings.Contains(string(raw), forbidden) {
			t.Fatal("private/control data leaked", forbidden)
		}
	}
	v.Grants = []PurposeInventoryGrant{}
	if ValidatePurposeInventory(v) != nil {
		t.Fatal("real empty denied")
	}
}
func TestTaskContextInventoryRejectInvalidMetadata(t *testing.T) {
	for _, name := range []string{"cross owner", "purpose", "future", "read TTL", "nil rows", "duplicate field", "truncation", "infinite grant"} {
		t.Run(name, func(t *testing.T) {
			v := taskInventoryFixture()
			switch name {
			case "cross owner":
				v.Owner.Type = actorref.Organization
			case "purpose":
				v.Grants[0].Purpose = "MODEL_EGRESS"
			case "future":
				v.Grants[0].CreatedAt = v.ObservedAt.Add(time.Second)
			case "read TTL":
				v.ValidUntil = v.ObservedAt.Add(time.Minute)
			case "nil rows":
				v.Grants = nil
			case "duplicate field":
				v.Grants[0].ProfileFields = []string{"agentNotes", "agentNotes"}
			case "truncation":
				v.Truncated = true
			case "infinite grant":
				v.Grants[0].ExpiresAt = v.ObservedAt.Add(time.Hour)
			}
			if ValidatePurposeInventory(v) == nil {
				t.Fatal("invalid inventory accepted")
			}
		})
	}
}
