package httpapi

import (
	"net/http/httptest"
	"strings"
	"testing"
)

func TestOrganizationMapControlClosedPointPresence(t *testing.T) {
	b := `{"action":"PROPOSE","operationId":"` + avatarTestOwner + `","expectedAuthorityVersion":2,"cityId":"aberdeen-gb","latitude":0,"longitude":0,"expectedCanonicalRevision":0}`
	if _, _, e := decodeOrgControl(httptest.NewRecorder(), orgControlRequest("POST", "/", b), false); e != nil {
		t.Fatal("zero coordinate/real city", e)
	}
	for _, bad := range []string{strings.Replace(b, `,"latitude":0`, "", 1), strings.Replace(b, `,"longitude":0`, "", 1), strings.Replace(b, `"latitude":0`, `"latitude":91`, 1), strings.Replace(b, `"cityId":"aberdeen-gb"`, `"cityId":null`, 1)} {
		if _, _, e := decodeOrgControl(httptest.NewRecorder(), orgControlRequest("POST", "/", bad), false); e == nil {
			t.Fatal("missing/excess point accepted")
		}
	}
}
