package activityparticipation

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	ea "github.com/birdtie/birdtie/apps/api/internal/entityaction"
)

const participationUnitOwner = "11111111-1111-4111-8111-111111111111"
const participationUnitActivity = "22222222-2222-4222-8222-222222222222"
const participationUnitOp = "33333333-3333-4333-8333-333333333333"

func TestHumanParticipationOperationStrictRequest(t *testing.T) {
	good := `{"operationId":"` + participationUnitOp + `","operation":"JOIN"}`
	if _, e := DecodeOperation([]byte(good)); e != nil {
		t.Fatal(e)
	}
	for _, raw := range []string{"null", "[]", "{}", good + "{}", strings.Replace(good, `"JOIN"`, `null`, 1), strings.Replace(good, `"JOIN"`, `"join"`, 1), strings.Replace(good, participationUnitOp, "00000000-0000-0000-0000-000000000000", 1), strings.Replace(good, `"operation":"JOIN"`, `"operation":"JOIN","operation":"CANCEL_RSVP"`, 1), strings.Replace(good, `"operation":"JOIN"`, `"operation":"JOIN","confirmed":true`, 1), strings.Repeat(" ", 4097)} {
		t.Run(raw[:min(len(raw), 80)], func(t *testing.T) {
			if _, e := DecodeOperation([]byte(raw)); e == nil {
				t.Fatal("invalid write accepted")
			}
		})
	}
}
func TestHumanParticipationOperationDigestBinding(t *testing.T) {
	in := OperationInput{participationUnitOp, "JOIN"}
	b := ea.BoundCondition{SourceVersion: strings.Repeat("a", 64), ValidUntil: time.Date(2026, 10, 10, 1, 2, 3, 123456000, time.UTC), Kind: ea.Join, Operation: "JOIN"}
	d, e := HumanParticipationRequestDigest(participationUnitOwner, participationUnitActivity, in, b)
	if e != nil || !operationHex.MatchString(d) {
		t.Fatal(d, e)
	}
	t.Log("FROZEN_REQUEST_DIGEST", d, "EFFECT_KEY", HumanParticipationEffectKey(participationUnitOwner, participationUnitOp))
	goldenOwner, goldenActivity, goldenOp := "a1000000-0000-4000-8000-000000000001", "a1000000-0000-4000-8000-000000000002", "a1000000-0000-4000-8000-000000000003"
	goldenBound := b
	goldenBound.ValidUntil = time.Date(2026, 10, 20, 10, 0, 0, 0, time.UTC)
	goldenDigest, e := HumanParticipationRequestDigest(goldenOwner, goldenActivity, OperationInput{OperationID: goldenOp, Operation: "JOIN"}, goldenBound)
	if e != nil || goldenDigest != "4834866d2dca009519508f6cc425db420c9ed529aab7d866b3adc78ce95c1f37" || HumanParticipationEffectKey(goldenOwner, goldenOp) != "162bbc84f81c0aa6a17b333801b53f3bfe46ca83fb7f3e17333674a3b482962c" {
		t.Fatal("independently frozen Go/Dart request vector changed", goldenDigest, e)
	}
	b2 := b
	b2.ValidUntil = b.ValidUntil.In(time.FixedZone("offset", 8*3600))
	d2, e := HumanParticipationRequestDigest(participationUnitOwner, participationUnitActivity, in, b2)
	if e != nil || d2 != d {
		t.Fatal("UTC canonical digest changed", d2, e)
	}
	for _, tc := range []string{"activity", "op", "action", "source", "until", "submicro"} {
		t.Run(tc, func(t *testing.T) {
			x, y, target := in, b, participationUnitActivity
			switch tc {
			case "activity":
				target = participationUnitOp
			case "op":
				x.OperationID = participationUnitActivity
			case "action":
				x.Operation = "CANCEL_RSVP"
				y.Operation = x.Operation
			case "source":
				y.SourceVersion = strings.Repeat("b", 64)
			case "until":
				y.ValidUntil = y.ValidUntil.Add(time.Microsecond)
			case "submicro":
				y.ValidUntil = y.ValidUntil.Add(time.Nanosecond)
			}
			other, e := HumanParticipationRequestDigest(participationUnitOwner, target, x, y)
			if tc == "submicro" {
				if e == nil {
					t.Fatal("submicrosecond approval accepted")
				}
			} else if e != nil || other == d {
				t.Fatal("different approved request reused digest", other, e)
			}
		})
	}
}
func TestHumanParticipationOperationReceiptAndOpaqueClosure(t *testing.T) {
	n := time.Date(2026, 10, 10, 1, 2, 4, 0, time.UTC)
	effect := n.Add(-time.Second)
	r := OperationReceipt{SchemaVersion: HumanParticipationOperationSchema, OwnerID: participationUnitOwner, ActivityID: participationUnitActivity, OperationID: participationUnitOp, Operation: "JOIN", EffectKey: HumanParticipationEffectKey(participationUnitOwner, participationUnitOp), RequestDigest: strings.Repeat("a", 64), ToolVersion: HumanParticipationToolVersion, Status: "COMMITTED", RecordedAt: n, EffectID: participationUnitOp, State: "going", EffectAt: &effect}
	if !ValidOperationReceipt(r, r.OwnerID, r.ActivityID, r.OperationID) {
		t.Fatal("valid original causal history rejected")
	}
	c := NewOperationClosure(r)
	if !ValidOperationClosure(c, r.OwnerID, r.ActivityID, r.OperationID) {
		t.Fatal(c)
	}
	raw, _ := json.Marshal(c)
	for _, key := range []string{"status", "state", "effectId", "effectAt", "reason", "approval", "sourceVersion"} {
		if strings.Contains(string(raw), `"`+key+`"`) {
			t.Fatal("opaque close leaked", key, string(raw))
		}
	}
	none := r
	none.Status = "NO_EFFECT"
	none.Reason = "USER_CLOSED"
	none.EffectID = ""
	none.State = ""
	none.EffectAt = nil
	if !ValidOperationReceipt(none, none.OwnerID, none.ActivityID, none.OperationID) {
		t.Fatal("permanent closure rejected")
	}
	for _, tc := range []string{"wrongowner", "wrongactivity", "reason", "state", "time", "noeffectshape", "noeffectreason", "schema", "tool", "key"} {
		t.Run(tc, func(t *testing.T) {
			v := r
			switch tc {
			case "wrongowner":
				v.OwnerID = participationUnitOp
			case "wrongactivity":
				v.ActivityID = participationUnitOp
			case "reason":
				v.Reason = "USER_CLOSED"
			case "state":
				v.State = "cancelled"
			case "time":
				v.RecordedAt = effect.Add(-time.Second)
			case "noeffectshape":
				v.Status = "NO_EFFECT"
				v.Reason = "USER_CLOSED"
			case "noeffectreason":
				v = none
				v.Reason = ""
			case "schema":
				v.SchemaVersion = "x"
			case "tool":
				v.ToolVersion = "v2"
			case "key":
				v.EffectKey = strings.Repeat("b", 64)
			}
			if ValidOperationReceipt(v, r.OwnerID, r.ActivityID, r.OperationID) {
				t.Fatal("invalid causal receipt accepted")
			}
		})
	}
}
func TestHumanParticipationOperationStrictClose(t *testing.T) {
	good := `{"operation":"JOIN","requestDigest":"` + strings.Repeat("a", 64) + `"}`
	if _, e := DecodeOperationClose([]byte(good)); e != nil {
		t.Fatal(e)
	}
	for _, raw := range []string{"null", "{}", good + "{}", strings.Replace(good, `"JOIN"`, `null`, 1), strings.Replace(good, strings.Repeat("a", 64), strings.Repeat("A", 64), 1), strings.Replace(good, `"operation":"JOIN"`, `"operation":"JOIN","operation":"JOIN"`, 1), strings.Replace(good, `"operation":"JOIN"`, `"operation":"JOIN","confirmed":true`, 1)} {
		t.Run(raw[:min(len(raw), 80)], func(t *testing.T) {
			if _, e := DecodeOperationClose([]byte(raw)); e == nil {
				t.Fatal("invalid owner close accepted")
			}
		})
	}
}
