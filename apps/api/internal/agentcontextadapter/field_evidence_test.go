package agentcontextadapter

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/birdtie/birdtie/apps/api/internal/agentconfidence"
	acb "github.com/birdtie/birdtie/apps/api/internal/agentcontextbuilder"
	"github.com/birdtie/birdtie/apps/api/internal/agentmemory"
)

func adapterFieldFixture(t *testing.T) acb.Bundle {
	t.Helper()
	b := adapterFixture()
	b.Profile = map[string]json.RawMessage{"preferredActivityTypes": json.RawMessage(`["badminton"]`), "agentNotes": json.RawMessage(`"PRIVATE_PROFILE_CANARY"`)}
	b.Memories[0].MemoryType = agentmemory.TypePreference
	b.Memories[0].Summary = "我不偏好羽毛球活动"
	b.Memories[0].MemoryKey = "activity_category:badminton"
	b.Memories[0].StructuredValue = json.RawMessage(`{"activityCategory":"badminton","nature":"human-correction","PRIVATE_STRUCTURED_CANARY":true}`)
	a := agentconfidence.NewDirectDeclaration()
	b.Memories[0].Confidence = &a
	var e error
	b.FieldEvidenceSet, e = acb.BuildFieldEvidenceSet(b)
	if e != nil {
		t.Fatal(e)
	}
	return b
}

func TestFieldEvidenceAdapterDoesNotRevealHiddenStructuredCategory(t *testing.T) {
	b := adapterFieldFixture(t)
	b.Profile = map[string]json.RawMessage{}
	b.Memories[0].Summary = "我明确记录的自定义内容"
	// This hidden structured value is not the original typed human declaration.
	// No value/category may leak into metadata just by matching MemoryKey.
	var e error
	b.FieldEvidenceSet, e = acb.BuildFieldEvidenceSet(b)
	if e != nil {
		t.Fatal(e)
	}
	one := 1
	v, e := Project(b, Budget{MaxEncodedBytes: MaxEncodedBytes, ItemLimit: &one, Priority: []string{"memories", "profile", "places", "activities", "relationships"}})
	if e != nil {
		t.Fatal(e)
	}
	raw, _ := json.Marshal(v.FieldEvidenceSet)
	if strings.Contains(string(raw), "badminton") || strings.Contains(string(raw), "PRIVATE_STRUCTURED_CANARY") || len(v.FieldEvidenceSet.Conflicts) != 0 {
		t.Fatal("hidden structured value exposed through semantic metadata")
	}
}
func TestFieldEvidenceAdapterBudgetsMetadataAndNeverLeaksOmittedContents(t *testing.T) {
	b := adapterFieldFixture(t)
	zero := 0
	v, e := Project(b, Budget{MaxEncodedBytes: MaxEncodedBytes, ItemLimit: &zero})
	if e != nil {
		t.Fatal(e)
	}
	if v.FieldEvidenceSet == nil || len(v.FieldEvidenceSet.Claims) != 2 || len(v.FieldEvidenceSet.Conflicts) != 0 {
		t.Fatal("required metadata or omitted conflict leaked")
	}
	raw, _ := json.Marshal(v)
	for _, hidden := range []string{b.Memories[0].ID, "PRIVATE_STRUCTURED_CANARY", "PRIVATE_PROFILE_CANARY", "preferredActivityTypes", "INTERNAL_XMIN_CANARY"} {
		if strings.Contains(string(raw), hidden) {
			t.Fatal("omitted field metadata/content", hidden)
		}
	}
	if v.Budget.Used != len(raw) {
		t.Fatal("evidence excluded from exact byte budget", v.Budget.Used, len(raw))
	}
	// Changing the decimal Limit/Used fields can shrink the envelope by a few
	// bytes; a one-byte subtraction is not a stable insufficient-budget oracle.
	if _, e = Project(b, Budget{MaxEncodedBytes: len(raw) - 32, ItemLimit: &zero}); e == nil {
		t.Fatal("mandatory evidence bypassed budget")
	}
	full, e := Project(b, Budget{MaxEncodedBytes: MaxEncodedBytes})
	if e != nil || len(full.FieldEvidenceSet.Conflicts) != 1 || full.FieldEvidenceSet.Conflicts[0].OtherClaimsOmitted {
		t.Fatal("full selected conflict missing", e)
	}
	fullRaw, _ := json.Marshal(full)
	if strings.Contains(string(fullRaw), "PRIVATE_STRUCTURED_CANARY") || strings.Contains(string(fullRaw), "INTERNAL_XMIN_CANARY") {
		t.Fatal("structured values/xmin leaked")
	}
}
func TestFieldEvidenceAdapterOneRetainedSideStaysPendingWithoutOtherID(t *testing.T) {
	b := adapterFieldFixture(t)
	one := 1
	v, e := Project(b, Budget{MaxEncodedBytes: MaxEncodedBytes, ItemLimit: &one, Priority: []string{"profile", "memories", "places", "activities", "relationships"}})
	if e != nil {
		t.Fatal(e)
	}
	// agentNotes sorts before preferredActivityTypes. Prioritize the exact
	// category field by removing this unrelated field from the selected bundle.
	delete(b.Profile, "agentNotes")
	b.FieldEvidenceSet, e = acb.BuildFieldEvidenceSet(b)
	if e != nil {
		t.Fatal(e)
	}
	v, e = Project(b, Budget{MaxEncodedBytes: MaxEncodedBytes, ItemLimit: &one, Priority: []string{"profile", "memories", "places", "activities", "relationships"}})
	if e != nil || len(v.FieldEvidenceSet.Conflicts) != 1 || !v.FieldEvidenceSet.Conflicts[0].OtherClaimsOmitted || len(v.FieldEvidenceSet.Conflicts[0].ClaimIDs) != 1 {
		t.Fatal("budget silently resolved contradiction", e)
	}
	raw, _ := json.Marshal(v)
	if strings.Contains(string(raw), b.Memories[0].ID) || strings.Contains(string(raw), b.Memories[0].Summary) {
		t.Fatal("omitted conflict side exposed")
	}
}
func TestFieldEvidenceAdapterRejectsTamperedCurrentMetadata(t *testing.T) {
	for name, change := range map[string]func(*acb.Bundle){
		"removedConflict":     func(b *acb.Bundle) { b.FieldEvidenceSet.Conflicts = []acb.FieldEvidenceConflict{} },
		"authority":           func(b *acb.Bundle) { b.FieldEvidenceSet.GrantsAuthority = true },
		"owner":               func(b *acb.Bundle) { b.FieldEvidenceSet.Owner.ID = b.Memories[0].ID },
		"removedClaim":        func(b *acb.Bundle) { b.FieldEvidenceSet.Claims = b.FieldEvidenceSet.Claims[1:] },
		"wrongVersion":        func(b *acb.Bundle) { b.FieldEvidenceSet.Claims[0].Source.Version.Token = strings.Repeat("b", 64) },
		"changedNativeSource": func(b *acb.Bundle) { b.Sources[0].Version.Token = strings.Repeat("b", 64) },
	} {
		t.Run(name, func(t *testing.T) {
			b := adapterFieldFixture(t)
			change(&b)
			if _, e := Project(b, DefaultBudget()); e == nil {
				t.Fatal("tampered evidence")
			}
		})
	}
}
