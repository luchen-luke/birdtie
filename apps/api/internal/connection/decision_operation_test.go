package connection

import (
	"testing"
	"time"
)

const decisionOwner = "11111111-1111-4111-8111-111111111111"
const decisionRequest = "33333333-3333-4333-8333-333333333333"
const decisionOp = "44444444-4444-4444-8444-444444444444"

func TestConnectionDecisionOperationContract(t *testing.T) {
	d, e := DecisionDigest(decisionOwner, decisionRequest, "accept")
	if e != nil {
		t.Fatal(e)
	}
	r := DecisionOperationReceipt{SchemaVersion: DecisionOperationSchema, OwnerID: decisionOwner, RequestID: decisionRequest, OperationID: decisionOp, RequestDigest: d, Action: "accept", Scope: "friend", Status: "COMMITTED", State: "accepted", RecordedAt: time.Date(2026, 10, 7, 0, 0, 0, 0, time.UTC)}
	if e = ValidateDecisionReceipt(r, decisionOwner, decisionRequest, decisionOp, "accept"); e != nil {
		t.Fatal(e)
	}
	for _, tc := range []string{"owner", "digest", "action", "state", "grant-like-status", "time", "false-no-effect"} {
		t.Run(tc, func(t *testing.T) {
			x := r
			switch tc {
			case "owner":
				x.OwnerID = decisionOp
			case "digest":
				x.RequestDigest = "same-looking"
			case "action":
				x.Action = "decline"
			case "state":
				x.State = "pending"
			case "grant-like-status":
				x.Status = "APPROVED"
			case "time":
				x.RecordedAt = time.Time{}
			case "false-no-effect":
				x.Status = "NO_EFFECT"
				x.State = ""
				x.Reason = "MISSING"
			}
			if ValidateDecisionReceipt(x, decisionOwner, decisionRequest, decisionOp, "accept") == nil {
				t.Fatal("unbound outcome accepted")
			}
		})
	}
	r.Status = "NO_EFFECT"
	r.State = ""
	r.Reason = "EXPIRED"
	if ValidateDecisionReceipt(r, decisionOwner, decisionRequest, decisionOp, "accept") != nil {
		t.Fatal("closed authoritative rejection shape")
	}
}
