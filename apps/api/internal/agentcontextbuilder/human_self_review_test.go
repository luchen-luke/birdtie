package agentcontextbuilder

import (
	"bytes"
	"encoding/json"
	"github.com/birdtie/birdtie/apps/api/internal/agentpolicysettings"
	"strings"
	"testing"
	"time"
)

// Existing pure field/source fixture; no native identity, PG or authorization.
func humanSelfReviewWireFixture(t *testing.T) BuiltContext {
	t.Helper()
	b := fieldEvidenceFixture(t)
	r := builderPureRequest()
	r.Mode = HumanSelfReview
	r.Selection = ExactSelfReview
	r.TaskID = ""
	r.TaskUpdatedAt = time.Time{}
	r.CityID = ""
	r.CurrentQuery = ""
	r.ProfileFields = []string{"preferredActivityTypes"}
	r.MemoryIDs = []string{b.Memories[0].ID}
	r.DeadlineAt = b.ExpiresAt
	b.Mode = r.Mode
	b.RequestID = r.RequestID
	b.TaskID = ""
	b.CityID = ""
	b.CurrentQuery = ""
	b.Task = nil
	b.City = nil
	b.Profile = map[string]json.RawMessage{"preferredActivityTypes": json.RawMessage(`["badminton"]`)}
	b.Sections = Sections{Profile: "AVAILABLE", Memories: "AVAILABLE", Policies: "NOT_REQUESTED", Activities: "NOT_REQUESTED", Places: "NOT_REQUESTED", Relationships: "UNAVAILABLE"}
	b.Sources = b.Sources[2:]
	for i := range b.Sources {
		b.Sources[i].Kind = strings.Replace(b.Sources[i].Kind, "PURPOSE_", "HUMAN_", 1)
	}
	fields, e := BuildFieldEvidenceSet(b)
	if e != nil {
		t.Fatal(e)
	}
	b.FieldEvidenceSet = fields
	return BuiltContext{Request: r, Bundle: b, Authority: strings.Repeat("a", 64)}
}
func TestHumanSelfReviewWireOnlySelectedHumanValuesAndPendingSources(t *testing.T) {
	b := humanSelfReviewWireFixture(t)
	before, _ := json.Marshal(b.Bundle)
	out, e := HumanSelfReviewView(b)
	if e != nil {
		t.Fatal(e)
	}
	raw, _ := json.Marshal(out)
	if out.SchemaVersion != HumanSelfReviewSchema || out.ModelAccess != "UNAVAILABLE" || out.MediaAccess != "UNAVAILABLE" || out.MemoryPromotionAllowed || out.GrantsAuthority || out.ComparisonScope != HumanSelfReviewComparison || len(out.FieldEvidenceSet.Conflicts) != 1 || out.FieldEvidenceSet.Conflicts[0].Status != PendingEvidenceConfirmation {
		t.Fatal("human read inherited authority or lost conflict", out)
	}
	if !strings.Contains(string(raw), "我不偏好羽毛球活动") || !strings.Contains(string(raw), "没有列出冲突不代表") {
		t.Fatal("visible declarations/coverage missing")
	}
	for _, canary := range []string{"PRIVATE_STRUCTURED_CANARY", "PRIVATE_XMIN_CANARY", `"structuredValue":`, `"memoryKey":`, `"Authority":`, `"authority":`, `"seal":`, `"SessionDigest":`} {
		if strings.Contains(string(raw), canary) {
			t.Fatal("internal/hidden context leaked", canary)
		}
	}
	out.Profile["preferredActivityTypes"] = json.RawMessage(`[]`)
	out.Memories[0].ValidFrom = timeCopy(out.ObservedAt)
	*out.Memories[0].CreatedAt = out.ObservedAt
	*out.Memories[0].Confidence.Value = 0
	out.FieldEvidenceSet.Claims[0].Source.Version.Revision++
	after, _ := json.Marshal(b.Bundle)
	if !bytes.Equal(before, after) {
		t.Fatal("projection mutated sealed native snapshot")
	}
}
func TestHumanSelfReviewWireClosedShapeAndBudget(t *testing.T) {
	for _, name := range []string{"machine", "wrongOwner", "sourceRevision", "pollutedEvidence", "missingSource", "extraField", "probability", "huge", "tooManyClaims"} {
		t.Run(name, func(t *testing.T) {
			b := humanSelfReviewWireFixture(t)
			switch name {
			case "machine":
				b.Request.Mode = MachineTaskContext
			case "wrongOwner":
				b.Bundle.Agent.Principal.ID = "33000000-0000-4000-8000-000000000099"
			case "sourceRevision":
				b.Bundle.Sources[1].Version.Revision++
			case "pollutedEvidence":
				b.Bundle.FieldEvidenceSet.GrantsAuthority = true
			case "missingSource":
				b.Bundle.Sources = b.Bundle.Sources[1:]
			case "extraField":
				b.Bundle.Profile["authRole"] = json.RawMessage(`"admin"`)
			case "probability":
				*b.Bundle.Memories[0].Confidence.Value = 0.86
			case "huge":
				b.Bundle.Profile["preferredActivityTypes"] = json.RawMessage(`"` + strings.Repeat("x", MaxHumanSelfReviewWireBytes) + `"`)
			case "tooManyClaims":
				for len(b.Bundle.FieldEvidenceSet.Claims) <= MaxFieldClaims {
					b.Bundle.FieldEvidenceSet.Claims = append(b.Bundle.FieldEvidenceSet.Claims, b.Bundle.FieldEvidenceSet.Claims[0])
				}
			}
			if out, e := HumanSelfReviewView(b); e == nil || out.SchemaVersion != "" {
				t.Fatal("invalid view returned partial private payload", name)
			}
		})
	}
}
func TestHumanSelfReviewWireUnconfiguredAndUnknownRemainUnknown(t *testing.T) {
	b := humanSelfReviewWireFixture(t)
	b.Request.ProfileFields = []string{"availability"}
	b.Bundle.Profile = map[string]json.RawMessage{}
	b.Bundle.Sections.Profile = "UNCONFIGURED"
	b.Bundle.Sources = b.Bundle.Sources[1:]
	b.Request.PolicyFamilies = []agentpolicysettings.Family{agentpolicysettings.Autonomy}
	b.Bundle.Policies = []agentpolicysettings.Record{agentpolicysettings.DefaultRecord(agentpolicysettings.Autonomy)}
	b.Bundle.Sections.Policies = "UNCONFIGURED"
	b.Bundle.Memories[0].CreatedAt = nil
	b.Bundle.Memories[0].ValidFrom = nil
	b.Bundle.Memories[0].Confidence = nil
	b.Bundle.FieldEvidenceSet = nil
	out, e := HumanSelfReviewView(b)
	if e != nil {
		t.Fatal(e)
	}
	if out.Sections.Profile != "UNCONFIGURED" || out.Sections.Policies != "UNCONFIGURED" || len(out.Profile) != 0 || out.Policies[0].Configured || out.Policies[0].NativeRevision != 0 || out.Memories[0].Confidence != nil || out.Memories[0].CreatedAt != nil || out.Memories[0].ValidFrom != nil || len(out.FieldEvidenceSet.Conflicts) != 0 {
		t.Fatal("unknown configuration/times became fact", out)
	}
	if b.Bundle.FieldEvidenceSet != nil {
		t.Fatal("legacy description fallback changed original sealed context")
	}
}
