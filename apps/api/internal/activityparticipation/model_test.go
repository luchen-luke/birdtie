package activityparticipation

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestPlansCurrentOverviewMinimalOnlineShape(t *testing.T) {
	at := time.Date(2026, 10, 5, 19, 30, 0, 0, time.FixedZone("+08", 8*3600))
	row := Overview{ID: "original-row", ActivityID: "original-activity", Modality: "online", PhysicalPlaceStatus: "not_applicable", StartsAt: &at, EndsAt: &at, TimeZone: "Asia/Shanghai"}
	raw, e := json.Marshal(row)
	if e != nil {
		t.Fatal(e)
	}
	for _, key := range []string{`"modality":"online"`, `"physicalPlaceStatus":"not_applicable"`, `"timeZone":"Asia/Shanghai"`, `+08:00`, `"placeName":""`} {
		if !strings.Contains(string(raw), key) {
			t.Fatal("minimal fact omitted", key)
		}
	}
	for _, private := range []string{"latitude", "longitude", "session", "description", "onlineUrl", "CONVERTED", "attendance"} {
		if strings.Contains(string(raw), private) {
			t.Fatal("unapproved data/fact leak", private)
		}
	}
}
