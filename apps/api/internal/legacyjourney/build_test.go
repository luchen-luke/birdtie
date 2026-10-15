package legacyjourney

import "testing"

func TestJourneyBuildRequiresVerifiedWholeSource(t *testing.T) {
	if p, e := Build("15800000-0000-4000-8000-000000000001", []byte(`{"tables":{"journeys":[]}}`), []byte(`{}`)); e == nil || len(p.Journeys) != 0 {
		t.Fatal("unverified schedule derived from unbound input")
	}
}
