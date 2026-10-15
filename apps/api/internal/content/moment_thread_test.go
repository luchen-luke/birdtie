package content

import (
	"encoding/json"
	"testing"

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
