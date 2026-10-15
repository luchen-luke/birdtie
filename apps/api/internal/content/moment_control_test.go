package content

import (
	"encoding/json"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	"strings"
	"testing"
	"time"
)

const momentControlTestOp = "11111111-1111-4111-8111-111111111111"
const momentControlTestID = "22222222-2222-4222-8222-222222222222"

func TestMomentPublicControlAllowsOptionalGeographyAndPreservesContext(t *testing.T) {
	for _, action := range []string{"PUBLISH", "UPDATE_PUBLIC"} {
		in := momentControlTestInput()
		in.Action = action
		in.MomentID = momentControlTestID
		in.ExpectedRevision = 2
		if _, err := NormalizeMomentControl(in, false); err != nil {
			t.Fatal("explicit city-only public action rejected", action, err)
		}
		in.Input.LocationPrecision = "none"
		if _, err := NormalizeMomentControl(in, false); err != nil {
			t.Fatal("public action required optional geography", action, err)
		}
		in.Input.PlaceID = momentControlTestID
		in.Input.LocationPrecision = "place"
		normalized, err := NormalizeMomentControl(in, false)
		if err != nil {
			t.Fatal(action, err)
		}
		raw, _ := json.Marshal(normalized)
		if _, err = DecodeMomentControl(raw, false); err != nil {
			t.Fatal("closed public action", err)
		}
		empty := ""
		in.Input.CommunityID = &empty
		if _, err = NormalizeMomentControl(in, false); err == nil {
			t.Fatal("public action changed an original private context", action)
		}
	}
}

func momentControlTestInput() MomentControlInput {
	return MomentControlInput{OperationID: momentControlTestOp, Action: "CREATE", Input: &MomentInput{CityID: "aberdeen-gb", Title: " 本人中文<稿> ", Body: " 自写正文 ", TimePrecision: "unknown", LocationPrecision: "city"}}
}
func TestMomentControlClosedInputsPreserveContextIntent(t *testing.T) {
	in, e := NormalizeMomentControl(momentControlTestInput(), false)
	if e != nil || in.Input.Title != "本人中文<稿>" {
		t.Fatal(e)
	}
	raw, _ := json.Marshal(in)
	v, e := DecodeMomentControl(raw, false)
	if e != nil || MomentControlDigest(v) != MomentControlDigest(in) {
		t.Fatal("normalized Unicode interop", e)
	}
	empty := ""
	clear := in
	copy := *in.Input
	copy.ActivityID = &empty
	clear.Input = &copy
	if MomentControlInputHash(in.Input) == MomentControlInputHash(clear.Input) {
		t.Fatal("omitted original link equals explicit deletion")
	}
	for _, change := range []struct{ name, raw string }{
		{"duplicate", strings.Replace(string(raw), `"action":"CREATE"`, `"action":"CREATE","action":"UPDATE"`, 1)},
		{"owner", strings.Replace(string(raw), `"action":"CREATE"`, `"action":"CREATE","ownerId":"other"`, 1)},
		{"nullContext", strings.Replace(string(raw), `"occurredAt":null`, `"occurredAt":null,"activityId":null`, 1)},
		{"missingBody", strings.Replace(string(raw), `"body":"自写正文",`, "", 1)},
		{"fakeApproval", strings.TrimSuffix(string(raw), "}") + `,"approval":"client"}`},
		{"createTarget", strings.TrimSuffix(string(raw), "}") + `,"momentId":"` + momentControlTestID + `"}`},
	} {
		t.Run(change.name, func(t *testing.T) {
			if _, e := DecodeMomentControl([]byte(change.raw), false); e == nil {
				t.Fatal("closed selector reached authority")
			}
		})
	}
	if _, e := DecodeMomentControl(raw, false); e != nil {
		t.Fatal("unknown occurredAt null must remain compatible", e)
	}
	in.Input.Body = strings.Repeat("汉", 1667)
	if _, e = NormalizeMomentControl(in, false); e == nil {
		t.Fatal("body byte limit widened")
	}
}
func TestMomentControlTerminalRevisionAndTinyReceipt(t *testing.T) {
	in := MomentControlInput{OperationID: momentControlTestOp, Action: "WITHDRAW", MomentID: momentControlTestID, ExpectedRevision: MomentControlMaxRevision}
	if _, e := NormalizeMomentControl(in, false); e != nil {
		t.Fatal("max terminal cleanup blocked", e)
	}
	in.Action = "UPDATE"
	in.Input = momentControlTestInput().Input
	if _, e := NormalizeMomentControl(in, false); e == nil {
		t.Fatal("nonterminal max allowed")
	}
	expected := MomentControlMaxRevision
	now := time.Now()
	r := MomentControlReceipt{OperationID: momentControlTestOp, Status: "SUCCEEDED", Action: "WITHDRAW", MomentID: momentControlTestID, ExpectedRevision: &expected, AppliedRevision: expected, PayloadDigest: strings.Repeat("a", 64), CommittedAt: &now}
	if !ValidMomentControlReceipt(r, momentControlTestOp) {
		t.Fatal("saturated concrete original fact rejected")
	}
	r.AppliedRevision++
	if ValidMomentControlReceipt(r, momentControlTestOp) {
		t.Fatal("Web unsafe revision exposed")
	}
	stopped := MomentControlReceipt{OperationID: momentControlTestOp, Status: "STOPPED"}
	if !ValidMomentControlReceipt(stopped, momentControlTestOp) {
		t.Fatal("zero effect tombstone rejected")
	}
	stopped.MomentID = momentControlTestID
	if ValidMomentControlReceipt(stopped, momentControlTestOp) {
		t.Fatal("STOPPED carries target/effect")
	}
	in = momentControlTestInput()
	in.SourceVersion = strings.Repeat("a", 64)
	in.Approval = "opaque"
	if _, e := NormalizeMomentControl(in, true); e != nil {
		t.Fatal(e)
	}
	in.SourceVersion = strings.Repeat("A", 64)
	if _, e := NormalizeMomentControl(in, true); e != identity.ErrControlInvalid {
		t.Fatal("noncanonical source", e)
	}
}
