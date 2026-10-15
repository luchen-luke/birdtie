package legacysocial

import (
	"os"
	"path/filepath"
	"testing"
)

// This opt-in pure check reads only the preserved source selected by the
// operator. Output never includes a source selector, body, name or digest.
func TestLegacySocialBuildProtectedEightTableProjection(t *testing.T) {
	root := os.Getenv("BIRDTIE_LEGACY_SOCIAL_PROTECTED_ROOT")
	if root == "" {
		t.Skip("NOT_RUN: protected preservation input not configured")
	}
	read := func(parts ...string) []byte {
		raw, e := os.ReadFile(filepath.Join(append([]string{root}, parts...)...))
		if e != nil {
			t.Fatal("protected fixture unavailable")
		}
		return raw
	}
	run := "c5df0d7c-f361-4275-9838-feb6d1d92352"
	original := read("civu-private-extract-20261015-01", "source.json")
	originalManifest := read("civu-private-extract-20261015-01", "manifest.json")
	source := read("civu-private-social-supplement-20261015-01", "social-source.private.json")
	manifest := read("civu-private-social-supplement-20261015-01", "social-manifest.private.json")
	p, e := Build(run, original, originalManifest, source, manifest)
	if e != nil {
		t.Fatal("sealed social source rejected")
	}
	want := Counts{Friendships: 5, AcceptedFriends: 4, PendingFriends: 1, Follows: 21, Conversations: 11, Members: 23, Messages: 39, MessageRequests: 1, Images: 1}
	if p.Counts != want {
		t.Fatal("social source cardinality changed")
	}
	kinds := map[string]int{}
	for _, m := range p.Messages {
		kinds[m.Kind]++
		if (m.Kind == "ENTITY" || m.Kind == "IMAGE") && m.Body != "" {
			t.Fatal("empty source body was replaced")
		}
	}
	if kinds["TEXT"] != 15 || kinds["SYSTEM"] != 10 || kinds["STICKER"] != 7 || kinds["ENTITY"] != 6 || kinds["IMAGE"] != 1 {
		t.Fatal("source message types were changed")
	}
	readOnlyDirect, groups := 0, 0
	for _, c := range p.Conversations {
		if c.Kind == "PERSONAL_GROUP" {
			groups++
		} else if c.AcceptanceSource == "" {
			readOnlyDirect++
		}
	}
	if readOnlyDirect != 6 || groups != 1 {
		t.Fatal("active conversation was mistaken for acceptance")
	}
	again, e := Build(run, original, originalManifest, source, manifest)
	if e != nil || again.ProjectionSHA256 != p.ProjectionSHA256 {
		t.Fatal("projection identity is not stable")
	}
	altered := append([]byte(nil), source...)
	altered = append(altered, ' ')
	if _, e = Build(run, original, originalManifest, altered, manifest); e == nil {
		t.Fatal("unsealed supplement accepted")
	}
}
