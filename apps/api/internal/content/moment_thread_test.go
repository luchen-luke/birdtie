package content

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/identity"
	"github.com/birdtie/birdtie/apps/api/internal/momentmedia"
)

func TestMomentThreadClosedOrderedContract(t *testing.T) {
	in := MomentThreadInput{OperationID: "00000000-0000-4000-8000-000000000153", Action: "CREATE_PUBLISH", LocalScope: "00000000-0000-4000-8000-000000000154", Audience: "PUBLIC", Items: []MomentThreadItem{
		{Input: MomentInput{CityID: "test-city", Title: "第一条", Body: "独立正文一", TimePrecision: "unknown", LocationPrecision: "city"}, Media: []momentmedia.AttachmentInput{}},
		{Input: MomentInput{CityID: "test-city", Title: "第二条", Body: "独立正文二", TimePrecision: "unknown", LocationPrecision: "city"}, Media: []momentmedia.AttachmentInput{}},
	}}
	raw, _ := json.Marshal(in)
	decoded, e := DecodeMomentThread(raw, false)
	if e != nil || MomentThreadDigest(decoded) != MomentThreadDigest(in) {
		t.Fatal("closed thread input", e)
	}
	reordered := in
	reordered.Items = []MomentThreadItem{in.Items[1], in.Items[0]}
	if MomentThreadDigest(reordered) == MomentThreadDigest(in) {
		t.Fatal("ordered entries absent from approval digest")
	}
	var fields map[string]json.RawMessage
	_ = json.Unmarshal(raw, &fields)
	fields["approval"] = json.RawMessage(`"unapproved"`)
	extra, _ := json.Marshal(fields)
	if _, e = DecodeMomentThread(extra, false); e != identity.ErrControlInvalid {
		t.Fatal("preview accepted approval", e)
	}
	duplicate := append([]byte(`{"operationId":"00000000-0000-4000-8000-000000000153",`), raw[1:]...)
	if _, e = DecodeMomentThread(duplicate, false); e != identity.ErrControlInvalid {
		t.Fatal("duplicate thread field", e)
	}
	one := in
	one.Items = one.Items[:1]
	if _, e = NormalizeMomentThread(one, false); e != identity.ErrControlInvalid {
		t.Fatal("single ordinary post masqueraded as thread")
	}
	in.Items[0].Input.OrganizationID = &in.OperationID
	if _, e = NormalizeMomentThread(in, false); e != identity.ErrControlInvalid {
		t.Fatal("thread gained organization authority")
	}
}

func TestMomentThreadEditingClosedSinglePreserveAndOriginalDigest(t *testing.T) {
	empty, restore := "", false
	removed := []MomentThreadRemoval{}
	in := MomentThreadInput{OperationID: "00000000-0000-4000-8000-000000000168", Action: "EDIT", LocalScope: "00000000-0000-4000-8000-000000000169", Audience: "PUBLIC", BaseMomentID: &empty, RemovedItems: &removed, Items: []MomentThreadItem{{Restore: &restore, Input: MomentInput{Body: "单条，无标题和地点", TimePrecision: "unknown", LocationPrecision: "none"}, Media: []momentmedia.AttachmentInput{}}}}
	raw, _ := json.Marshal(in)
	decoded, e := DecodeMomentThread(raw, false)
	if e != nil || len(decoded.Items) != 1 || MomentThreadDigest(decoded) != MomentThreadDigest(in) {
		t.Fatal("single EDIT exact echo", e, string(raw))
	}
	wrong := in
	wrong.Items = append([]MomentThreadItem{}, in.Items...)
	wrong.Items[0].Media = nil
	if _, e = NormalizeMomentThread(wrong, false); e != identity.ErrControlInvalid {
		t.Fatal("new item may not implicitly preserve upload", e)
	}
	base := "00000000-0000-4000-8000-000000000170"
	in.BaseMomentID = &base
	in.Items[0].MomentID = base
	in.Items[0].ExpectedRevision = 2
	in.Items[0].Input.Body = ""
	in.Items[0].Media = nil
	raw, _ = json.Marshal(in)
	decoded, e = DecodeMomentThread(raw, false)
	if e != nil || decoded.Items[0].Media != nil {
		t.Fatal("existing media-only preserve must reach current source gate", e)
	}
	withContext := in
	withContext.Items = append([]MomentThreadItem{}, in.Items...)
	contextID := " ABCDEF00-0000-4000-8000-000000000168 "
	noLocations := []MomentLocationInput{}
	withContext.Items[0].Input.Locations = &noLocations
	withContext.Items[0].Input.ActivityID = &contextID
	withContext.Items[0].Input.CommunityID = &contextID
	withContext.Items[0].Input.OrganizationID = &contextID
	contextRaw, _ := json.Marshal(withContext)
	contextDecoded, contextErr := DecodeMomentThread(contextRaw, false)
	if contextErr != nil || contextDecoded.Items[0].Input.CityID != "" || contextDecoded.Items[0].Input.Locations == nil || len(*contextDecoded.Items[0].Input.Locations) != 0 {
		t.Fatal("EDIT original relationships imposed optional City gate", contextErr)
	}
	for _, slot := range []*string{contextDecoded.Items[0].Input.ActivityID, contextDecoded.Items[0].Input.CommunityID, contextDecoded.Items[0].Input.OrganizationID} {
		if slot == nil || *slot != "abcdef00-0000-4000-8000-000000000168" {
			t.Fatal("EDIT original relationship lost normalization")
		}
	}
	for kind := 0; kind < 3; kind++ {
		invalid := contextDecoded
		invalid.Items = append([]MomentThreadItem{}, contextDecoded.Items...)
		badID := "not-a-uuid"
		slots := []**string{&invalid.Items[0].Input.ActivityID, &invalid.Items[0].Input.CommunityID, &invalid.Items[0].Input.OrganizationID}
		*slots[kind] = &badID
		if _, err := NormalizeMomentThread(invalid, false); err != identity.ErrControlInvalid {
			t.Fatal("EDIT accepted invalid preserved relationship", kind, err)
		}
	}
	if _, err := NormalizeMomentControl(MomentControlInput{OperationID: in.OperationID, Action: "UPDATE", MomentID: base, ExpectedRevision: 2, Input: &contextDecoded.Items[0].Input}, false); err != identity.ErrControlInvalid {
		t.Fatal("EDIT exception broadened old private UPDATE contract", err)
	}
	clear := in
	clear.Items = append([]MomentThreadItem{}, in.Items...)
	clear.Items[0].Media = []momentmedia.AttachmentInput{}
	if MomentThreadDigest(clear) == MomentThreadDigest(in) {
		t.Fatal("explicit clear conflated with preserve")
	}
	if _, e = NormalizeMomentThread(clear, false); e != identity.ErrControlInvalid {
		t.Fatal("blank entry published after clearing media", e)
	}
	var fields map[string]json.RawMessage
	_ = json.Unmarshal(raw, &fields)
	fields["unexpected"] = json.RawMessage(`true`)
	bad, _ := json.Marshal(fields)
	if _, e = DecodeMomentThread(bad, false); e != identity.ErrControlInvalid {
		t.Fatal("open EDIT envelope", e)
	}
	_ = json.Unmarshal(raw, &fields)
	fields["removedItems"] = json.RawMessage(`null`)
	bad, _ = json.Marshal(fields)
	if _, e = DecodeMomentThread(bad, false); e != identity.ErrControlInvalid {
		t.Fatal("implicit removals", e)
	}
	old := MomentThreadInput{OperationID: in.OperationID, Action: "WITHDRAW", ThreadID: base, Audience: "PUBLIC", ExpectedVersion: 3, Items: []MomentThreadItem{}}
	want := MomentControlHash([]any{"moment-thread-v1", old.OperationID, old.Action, old.ThreadID, "", "PUBLIC", int64(3), []any{}})
	if MomentThreadDigest(old) != want {
		t.Fatal("old operation digest changed")
	}
	at := time.Now()
	retired := []MomentThreadMember{{MomentID: base, Position: 24, Status: "withdrawn", AppliedRevision: 4, Media: []momentmedia.StageSelector{}}}
	r := MomentThreadReceipt{SchemaVersion: "moment-thread-operation-v2", OperationID: in.OperationID, Status: "SUCCEEDED", Action: "EDIT", ThreadID: in.LocalScope, RootMomentID: base, Audience: "PUBLIC", ExpectedVersion: 2, AppliedVersion: 3, PayloadDigest: MomentThreadDigest(in), CommittedAt: &at, Members: []MomentThreadMember{{MomentID: "00000000-0000-4000-8000-000000000171", Position: 0, Status: "published", AppliedRevision: 3, Media: []momentmedia.StageSelector{}}}, RemovedMembers: &retired}
	if !ValidMomentThreadReceipt(r, in.OperationID) {
		t.Fatal("retired root and historical position >19 rejected")
	}
}
