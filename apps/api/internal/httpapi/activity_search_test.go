package httpapi

import (
	"net/http/httptest"
	"testing"
)

func TestParseActivitySearch(t *testing.T) {
	tests := []struct {
		query    string
		filtered bool
		code     string
	}{
		{"", false, ""},
		{"bounds=-2.2,57.1,-2.0,57.3&from=2026-10-03T00%3A00%3A00Z&to=2026-10-05T00%3A00%3A00Z&category=badminton", true, ""},
		{"category=", true, ""},
		{"bounds=-2,57,-1", true, "invalid_bounds"},
		{"bounds=NaN,57,-1,58", true, "invalid_bounds"},
		{"bounds=-1,57,-2,58", true, "invalid_bounds"},
		{"from=tomorrow", true, "invalid_time_range"},
		{"from=2026-10-05T00%3A00%3A00Z&to=2026-10-03T00%3A00%3A00Z", true, "invalid_time_range"},
		{"category=Badminton%20Club", true, "invalid_category"},
	}
	for _, tc := range tests {
		t.Run(tc.query, func(t *testing.T) {
			r := httptest.NewRequest("GET", "/v1/cities/aberdeen-gb/activities?"+tc.query, nil)
			filter, filtered, code := parseActivitySearch(r)
			if filtered != tc.filtered || code != tc.code {
				t.Fatalf("filtered=%t code=%q; want %t %q", filtered, code, tc.filtered, tc.code)
			}
			if tc.code == "" && filter.From != nil && filter.To != nil && !filter.From.Before(*filter.To) {
				t.Fatal("valid interval is not ordered")
			}
		})
	}
}

func TestParseActivitySearchAcceptsTimezoneOffset(t *testing.T) {
	r := httptest.NewRequest("GET", "/?from=2026-10-03T12%3A00%3A00%2B01%3A00", nil)
	filter, _, code := parseActivitySearch(r)
	if code != "" || filter.From == nil || filter.From.UTC().Hour() != 11 {
		t.Fatalf("unexpected offset parse: %#v %q", filter.From, code)
	}
}
