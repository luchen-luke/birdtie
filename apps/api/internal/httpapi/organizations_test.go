package httpapi

import (
	"testing"

	"github.com/birdtie/birdtie/apps/api/internal/organization"
)

func TestValidOrganizationProfile(t *testing.T) {
	tests := []struct {
		name  string
		input organization.ProfileInput
		valid bool
	}{
		{"valid", organization.ProfileInput{Name: "  学生社团  ", Description: "  羽毛球活动  ", OfficialLinks: []string{" https://example.org/events "}}, true},
		{"short name", organization.ProfileInput{Name: "A"}, false},
		{"insecure link", organization.ProfileInput{Name: "学生社团", OfficialLinks: []string{"http://example.org"}}, false},
		{"link credentials", organization.ProfileInput{Name: "学生社团", OfficialLinks: []string{"https://user:password@example.org"}}, false},
		{"too many links", organization.ProfileInput{Name: "学生社团", OfficialLinks: make([]string, 9)}, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := validOrganizationProfile(&tc.input); got != tc.valid {
				t.Fatalf("validOrganizationProfile() = %t, want %t", got, tc.valid)
			}
			if tc.name == "valid" && (tc.input.Name != "学生社团" || tc.input.OfficialLinks[0] != "https://example.org/events") {
				t.Fatalf("profile was not normalized: %#v", tc.input)
			}
		})
	}
}
