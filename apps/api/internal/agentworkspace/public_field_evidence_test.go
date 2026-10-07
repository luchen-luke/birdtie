package agentworkspace

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	arp "github.com/birdtie/birdtie/apps/api/internal/agentresultprojection"
	"github.com/birdtie/birdtie/apps/api/internal/foundation"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
)

func publicEvidenceWorkspaceUnit(t *testing.T) (Results, Task) {
	t.Helper()
	now := time.Date(2026, 10, 7, 5, 0, 0, 0, time.UTC)
	owner, id, entity := "53000000-0000-4000-8000-000000000001", "53000000-0000-4000-8000-000000000002", "53000000-0000-4000-8000-000000000003"
	task := Task{ID: id, PrincipalType: "person", PrincipalID: owner, ActingUserID: owner, CityID: "aberdeen-gb", ContextType: "CITY", ContextID: "aberdeen-gb", Intent: FindActivity, Status: TaskCompleted, Query: "找活动", UpdatedAt: now.Add(-time.Hour), Filters: map[string]string{}, Conversation: []Message{}}
	raw, _ := json.Marshal(task)
	ref := arp.Ref{Type: "activity", ID: entity}
	r := arp.Receipt{Activities: []foundation.Activity{{ID: entity, Visibility: "public", Title: "获准原对象", Source: foundation.Source{UpdatedAt: now.Add(-time.Hour)}}}, Items: []arp.Item{{Entity: ref, Title: "获准原对象", Scope: arp.AuthorizedView, Detail: &ref}}, PublicCommercialRefs: []arp.Ref{ref}, ObservedAt: now, ValidUntil: now.Add(20 * time.Second), Proof: strings.Repeat("a", 64), Seal: strings.Repeat("b", 64)}
	p, e := arp.BuildPublicFieldEvidence(arp.Access{Actor: identity.Actor{ID: owner, AccountType: "person"}, SessionDigest: [32]byte{1}, TaskID: id, ExpectedTask: raw}, arp.Query{Kind: "activity", CityID: "aberdeen-gb"}, r)
	if e != nil {
		t.Fatal(e)
	}
	return Results{Task: &task, PrincipalType: "PERSON", PrincipalID: owner, NativeProjection: true, ProjectionItems: r.Items, PublicCommercialRefs: r.PublicCommercialRefs, PublicFieldEvidence: p, Activities: r.Activities, Query: task.Query, CityID: task.CityID}, task
}
func TestPublicFieldEvidenceUnitContractRebuildSameNativeVersion(t *testing.T) {
	r, task := publicEvidenceWorkspaceUnit(t)
	first := WithContract(r, task, "first")
	second := WithContract(first, task, "second")
	if first.ResultSet.PublicFieldEvidence == nil || second.ResultSet.PublicFieldEvidence == nil || len(second.ResultSet.PublicFieldEvidence.FieldEvidenceSet.Claims) != 6 || len(second.ResultSet.Items) != 1 || second.ResultSet.GeneratedAt.Equal(second.ResultSet.PublicFieldEvidence.ObservedAt) {
		t.Fatal("rebuild lost evidence or used task time as native read time")
	}
	first.ResultSet.PublicFieldEvidence.FieldEvidenceSet.Claims[0].Field = "tampered"
	if second.ResultSet.PublicFieldEvidence.FieldEvidenceSet.Claims[0].Field == "tampered" {
		t.Fatal("deep copy lost")
	}
}
func TestPublicFieldEvidenceUnitContractNeverRestoresOldMetadata(t *testing.T) {
	for _, mode := range []string{"task", "owner", "org", "online", "failed", "version", "filters", "refs", "public_refs", "non_native", "wire_only"} {
		t.Run(mode, func(t *testing.T) {
			r, task := publicEvidenceWorkspaceUnit(t)
			r = WithContract(r, task, "old")
			switch mode {
			case "task":
				task.ID = "53000000-0000-4000-8000-000000000004"
			case "owner":
				r.PrincipalID = "53000000-0000-4000-8000-000000000004"
			case "org":
				r.PrincipalType = "ORGANIZATION"
				task.PrincipalType = "organization"
			case "online":
				task.ContextType = "ONLINE"
			case "failed":
				task.Status = TaskFailed
			case "version":
				task.UpdatedAt = task.UpdatedAt.Add(time.Second)
			case "filters":
				task.Filters = map[string]string{"category": "sports"}
			case "refs":
				r.ProjectionItems = append([]arp.Item{}, r.ProjectionItems...)
				r.ProjectionItems[0].Title = "updated title"
			case "public_refs":
				r.PublicCommercialRefs = nil
			case "non_native":
				r.NativeProjection = false
			case "wire_only":
				r.PublicFieldEvidence = nil
			}
			current := WithContract(r, task, "current")
			if current.ResultSet.PublicFieldEvidence != nil {
				t.Fatal("stale/foreign metadata restored")
			}
			control := r
			control.PublicFieldEvidence = nil
			withoutMetadata := WithContract(control, task, "current")
			originalItems, _ := json.Marshal(withoutMetadata.ResultSet.Items)
			actualItems, _ := json.Marshal(current.ResultSet.Items)
			if string(originalItems) != string(actualItems) {
				t.Fatal("metadata refusal changed the original entity projection")
			}
		})
	}
}
