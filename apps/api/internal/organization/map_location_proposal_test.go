package organization

import (
	"math"
	"testing"
)

func TestOrganizationMapProposalExactPointAndAxes(t *testing.T) {
	p := ControlInput{Action: "PROPOSE", OperationID: controlTestID, ExpectedAuthorityVersion: 1, CityID: "aberdeen-gb", Latitude: 0, Longitude: 0}
	if _, e := NormalizeControl(p); e != nil {
		t.Fatal("real slug and zero pair", e)
	}
	for n, f := range map[string]func(*ControlInput){"missingCity": func(i *ControlInput) { i.CityID = "" }, "nan": func(i *ControlInput) { i.Latitude = math.NaN() }, "outside": func(i *ControlInput) { i.Longitude = 181 }, "noPairSource": func(i *ControlInput) { i.MembershipID = controlTestID }, "proposalInSubmit": func(i *ControlInput) { i.ProposalID = controlTestID }} {
		t.Run(n, func(t *testing.T) {
			i := p
			f(&i)
			if _, e := NormalizeControl(i); e != ErrControlInvalid {
				t.Fatal(e)
			}
		})
	}
	changed := p
	changed.Latitude = math.SmallestNonzeroFloat64
	if ControlDigest(controlTestID, changed) == ControlDigest(controlTestID, p) {
		t.Fatal("rounded arbitrary point input")
	}
	for _, action := range []string{"APPROVE_POINT", "REJECT_POINT", "WITHDRAW_POINT"} {
		i := ControlInput{Action: action, OperationID: controlTestID, ExpectedAuthorityVersion: 2, ProposalID: controlTestID, ExpectedProposalVersion: 1}
		if _, e := NormalizeControl(i); e != nil {
			t.Fatal(action, e)
		}
	}
}
