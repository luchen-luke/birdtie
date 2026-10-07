package httpapi

import (
	"net/http/httptest"
	"strings"
	"testing"
)

func TestOnlineSocialOpportunityStrictWire(t *testing.T) {
	for _, suffix := range []string{"?", "?ownerId=x", "?intentId=x&intentId=y", "?bad=%xx"} {
		r := httptest.NewRequest("GET", "/v1/me/online-social-opportunities/options"+suffix, nil)
		if onlineSocialWire(r) == nil {
			t.Fatal("query accepted", suffix)
		}
	}
	r := httptest.NewRequest("GET", "/v1/me/online-social-opportunities/options", strings.NewReader("{}"))
	if onlineSocialWire(r) == nil {
		t.Fatal("body accepted")
	}
	r = httptest.NewRequest("GET", "/v1/me/online-social-opportunities/options", nil)
	r.Header.Set("X-Birdtie-Organization-Workspace", "")
	if onlineSocialWire(r) == nil {
		t.Fatal("workspace header accepted")
	}
	r = httptest.NewRequest("GET", "/v1/me/online-social-opportunities/options", nil)
	if onlineSocialWire(r) != nil {
		t.Fatal("normal GET rejected")
	}
}
