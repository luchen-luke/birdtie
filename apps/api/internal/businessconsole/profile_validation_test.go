package businessconsole

import "testing"

func TestBusinessConsoleProfileExplicitTimeZone(t *testing.T) {
	for _, tc := range []struct {
		zone  string
		valid bool
	}{{"Local", false}, {"Europe/London", true}, {"UTC", true}, {"Asia/Shanghai", true}, {"", true}, {"Unknown/Zone", false}} {
		t.Run(tc.zone, func(t *testing.T) {
			f := ProfileFacts{Name: "合成商家", TimeZone: tc.zone, OpeningHours: []HoursDay{}, OfficialLinks: []string{}}
			if (ValidateProfile(f) == nil) != tc.valid {
				t.Fatalf("explicit zone %q validation mismatch", tc.zone)
			}
		})
	}
}
