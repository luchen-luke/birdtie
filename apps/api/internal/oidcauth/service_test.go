package oidcauth

import (
	"net/url"
	"testing"
)

func TestClientRedirectBoundary(t *testing.T) {
	for _, test := range []struct {
		value string
		want  bool
	}{
		{"birdtie-auth://callback", true},
		{"birdtie-auth://other", false},
		{"birdtie-auth://callback/other", false},
		{"birdtie-auth://user@callback", false},
		{"birdtie-auth://callback?code=forged", false},
		{"https://birdtie.example/oidc/callback", true},
		{"http://birdtie.example/oidc/callback", false},
		{"http://localhost:7357/", true},
	} {
		u, err := url.Parse(test.value)
		if err != nil {
			t.Fatalf("parse %q: %v", test.value, err)
		}
		if got := validClientRedirect(u); got != test.want {
			t.Errorf("validClientRedirect(%q) = %v, want %v", test.value, got, test.want)
		}
	}
}
