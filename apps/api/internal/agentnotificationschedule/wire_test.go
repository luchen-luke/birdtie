package agentnotificationschedule

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestNotificationScheduleUnitWire(t *testing.T) {
	raw, _ := json.Marshal(PutInput{0, scheduleTestSettings(), time.Date(2026, 10, 10, 0, 0, 0, 0, time.UTC)})
	good := string(raw)
	if _, e := DecodePut(raw); e != nil {
		t.Fatal(e)
	}
	for _, tc := range []struct {
		name string
		raw  string
	}{{"missing", strings.Replace(good, `"quiet":null,`, "", 1)}, {"owner", strings.Replace(good, `"enabled":true`, `"ownerId":"secret","enabled":true`, 1)}, {"confirmed", strings.Replace(good, `"enabled":true`, `"confirmed":true,"enabled":true`, 1)}, {"duplicate", strings.Replace(good, `"enabled":true`, `"enabled":false,"enabled":true`, 1)}, {"escaped_duplicate", strings.Replace(good, `"enabled":true`, `"enab\u006ced":false,"enabled":true`, 1)}, {"case", strings.Replace(good, `"enabled"`, `"Enabled"`, 1)}, {"null_bool", strings.Replace(good, `"enabled":true`, `"enabled":null`, 1)}, {"fraction", strings.Replace(good, `"localMinute":1080`, `"localMinute":1.1`, 1)}, {"duplicate_quiet", strings.Replace(good, `"quiet":null`, `"quiet":{"startMinute":1,"startMinute":2,"endMinute":3}`, 1)}, {"quiet_unknown", strings.Replace(good, `"quiet":null`, `"quiet":{"startMinute":1,"endMinute":3,"bypass":true}`, 1)}, {"trailing", good + `{}`}, {"oversize", strings.Repeat(" ", MaxBodyBytes) + good}, {"invalid_utf8", string([]byte{0xff})}} {
		t.Run(tc.name, func(t *testing.T) {
			if _, e := DecodePut([]byte(tc.raw)); e == nil {
				t.Fatal("accepted invalid wire")
			}
		})
	}
	settings, _ := json.Marshal(scheduleTestSettings())
	if _, e := DecodeSettings(settings); e != nil {
		t.Fatal(e)
	}
	if _, e := DecodeSettings([]byte(strings.Replace(string(settings), `"enabled":true`, `"enabled":null`, 1))); e == nil {
		t.Fatal("corrupt persisted shape")
	}
}
