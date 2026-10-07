package agentcontextrelevance

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/actorref"
	"github.com/birdtie/birdtie/apps/api/internal/agentcognitive"
	acb "github.com/birdtie/birdtie/apps/api/internal/agentcontextbuilder"
	"github.com/birdtie/birdtie/apps/api/internal/agentevent"
	"github.com/birdtie/birdtie/apps/api/internal/agentruntime"
)

func TestFieldEvidenceRelevanceRebuildsOnlyKeptFieldsAndSources(t *testing.T) {
	b := fixture("帮我找羽毛球")
	now := time.Date(2026, 10, 6, 0, 0, 0, 0, time.UTC)
	b.ObservedAt = now
	b.ExpiresAt = now.Add(time.Minute)
	b.Agent = agentcognitive.AgentReference{AgentID: "43000000-0000-4000-8000-000000000001", Principal: actorref.PrincipalRef{Type: actorref.Person, ID: "43000000-0000-4000-8000-000000000002"}, Role: agentruntime.PersonalAgent}
	for i := range b.Memories {
		b.Memories[i].ValidUntil = now.Add(time.Hour)
	}
	for i := range b.Sources {
		b.Sources[i].NativeTime = now.Add(-time.Second)
		if b.Sources[i].Kind == "PURPOSE_PRIVATE_PROFILE" {
			b.Sources[i].ID = b.Agent.AgentID
		}
		if strings.HasPrefix(b.Sources[i].Kind, "PURPOSE_") && b.Sources[i].Kind != "PURPOSE_RELATIONSHIP_TIE" {
			b.Sources[i].Version = agentevent.SourceVersion{Kind: agentevent.RevisionVersion, Revision: 1}
		} else {
			b.Sources[i].Version = agentevent.SourceVersion{Kind: agentevent.UpdatedAtDigestVersion, Token: strings.Repeat("a", 64)}
		}
	}
	var e error
	b.FieldEvidenceSet, e = acb.BuildFieldEvidenceSet(b)
	if e != nil {
		t.Fatal(e)
	}
	before, _ := json.Marshal(b)
	v, e := project(b)
	if e != nil {
		t.Fatal(e)
	}
	if v.Context.FieldEvidenceSet == nil || acb.FieldEvidenceMatchesBundle(v.Context) != nil {
		t.Fatal("new field evidence inconsistent with selected view")
	}
	raw, _ := json.Marshal(v.Context.FieldEvidenceSet)
	for _, hidden := range []string{"agentNotes", "EXPLICIT_MEMORY:c:", "PUBLIC_PLACE:p2:", "PUBLIC_ACTIVITY:e2:", "tie1", "native-retained"} {
		if strings.Contains(string(raw), hidden) {
			t.Fatal("excluded metadata survived relevance", hidden)
		}
	}
	after, _ := json.Marshal(b)
	if string(before) != string(after) {
		t.Fatal("original native bundle mutated")
	}
}
