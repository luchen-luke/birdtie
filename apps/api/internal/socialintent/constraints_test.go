package socialintent

import (
	"encoding/json"
	"testing"
)

func TestParseConstraintsByModality(t *testing.T) {
	cases := []struct {
		name     string
		mode     string
		json     string
		accepted bool
	}{
		{"online without geography", "ONLINE", `{"category":"badminton"}`, true},
		{"online with platform", "ONLINE", `{"onlinePlatform":"Zoom"}`, true},
		{"explicit time window distinct from intent expiry", "ONLINE", `{"startsAt":"2030-01-01T18:00:00+08:00","endsAt":"2030-01-01T19:00:00+08:00"}`, true},
		{"online rejects physical area", "ONLINE", `{"areaLabel":"Aberdeen"}`, false},
		{"in person with coarse area", "IN_PERSON", `{"areaLabel":" Aberdeen centre "}`, true},
		{"in person with canonical Place", "IN_PERSON", `{"placeId":"22222222-2222-4222-8222-222222222222"}`, true},
		{"reject truncated Place UUID", "IN_PERSON", `{"placeId":"22222222-2222-4222-222222222222"}`, false},
		{"in person needs location", "IN_PERSON", `{}`, false},
		{"in person rejects online platform", "IN_PERSON", `{"areaLabel":"Aberdeen","onlinePlatform":"Zoom"}`, false},
		{"hybrid accepts both", "HYBRID", `{"areaLabel":"Aberdeen","onlinePlatform":"Zoom"}`, true},
		{"hybrid needs online", "HYBRID", `{"areaLabel":"Aberdeen"}`, false},
		{"hybrid needs physical", "HYBRID", `{"onlinePlatform":"Zoom"}`, false},
		{"reject unknown fields", "ONLINE", `{"preciseLatitude":57.1}`, false},
		{"reject malformed", "ONLINE", `[]`, false},
		{"reject participant mismatch", "ONLINE", `{"minParticipants":5,"maxParticipants":2}`, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			value, encoded, err := ParseConstraints(json.RawMessage(tc.json), tc.mode)
			if (err == nil) != tc.accepted {
				t.Fatalf("accepted=%t, err=%v", tc.accepted, err)
			}
			if err == nil && (len(encoded) == 0 || (tc.mode == "ONLINE" && value.AreaLabel != "")) {
				t.Fatalf("invalid normalization: %+v %s", value, encoded)
			}
		})
	}
}
func TestParseConstraintsConcreteTimeRejectsMalformedWindow(t *testing.T) {
	for _, raw := range []string{`{"startsAt":"2030-01-01T18:00:00Z"}`, `{"endsAt":"2030-01-01T19:00:00Z"}`, `{"startsAt":"2030-01-01T18:00:00","endsAt":"2030-01-01T19:00:00Z"}`, `{"startsAt":"2030-02-30T18:00:00Z","endsAt":"2030-03-01T19:00:00Z"}`, `{"startsAt":"2030-01-01T19:00:00Z","endsAt":"2030-01-01T18:00:00Z"}`, `{"startsAt":"2030-01-01T19:00:00Z","endsAt":"2031-01-01T18:00:00Z"}`} {
		if _, _, e := ParseConstraints(json.RawMessage(raw), "ONLINE"); e == nil {
			t.Fatal("invalid concrete time", raw)
		}
	}
}
