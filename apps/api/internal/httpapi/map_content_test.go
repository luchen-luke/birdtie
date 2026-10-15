package httpapi

import (
	"net/http/httptest"
	"testing"
)

func TestMapContentQueryRejectsDuplicateOrganizationAndUnknown(t *testing.T) {
	for _, u := range []string{"/v1/me/map-content?west=1&west=2", "/v1/me/map-content?unexpected=1", "/v1/me/map-content?"} {
		r := httptest.NewRequest("GET", u, nil)
		if _, e := mapContentQueryValues(r, "west"); e == nil {
			t.Fatal("unsafe query accepted", u)
		}
	}
	r := httptest.NewRequest("GET", "/v1/me/map-content?west=1", nil)
	r.Header.Set("X-Birdtie-Organization-Workspace", "present")
	if _, e := mapContentQueryValues(r, "west"); e == nil {
		t.Fatal("organization frame accepted")
	}
}
