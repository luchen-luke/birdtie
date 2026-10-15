package legacyreferencemedia

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReferenceImagePurposeNamespacesAndPrivateProjection(t *testing.T) {
	id := "16700000-0000-4000-8000-000000000001"
	if StableID("MOMENT_COVER", id) == StableID("PLACE_IMAGE", id) || StableID("material", id) != StableID("material", id) {
		t.Fatal("purpose identity collision")
	}
	p := Plan{Assets: []Asset{{OwnerSourceID: "PRIVATE_OWNER", InputSHA256: "PRIVATE_INPUT"}}, References: []Reference{{Attribution: "PRIVATE_ATTRIBUTION"}}}
	raw, e := json.Marshal(p)
	if e != nil || strings.Contains(string(raw), "PRIVATE_") {
		t.Fatal("private projection escaped")
	}
	if _, e := Build(id, []byte(`{}`), []byte(`{}`), []byte(`{}`), []byte(`{}`), strings.Repeat("a", 64)); e == nil {
		t.Fatal("unverified input accepted")
	}
}

func TestReferenceMediaProtectedPrepare(t *testing.T) {
	root := os.Getenv("BIRDTIE_REFERENCE_MEDIA_PREPARE_ROOT")
	if root == "" {
		t.Skip("explicit protected metadata prepare only")
	}
	read := func(name string) []byte {
		b, e := os.ReadFile(filepath.Join(root, name))
		if e != nil {
			t.Fatal("protected input unavailable")
		}
		return b
	}
	p, e := Build("c5df0d7c-f361-4275-9838-feb6d1d92352", read("civu-private-extract-20261015-01/source.json"), read("civu-private-extract-20261015-01/manifest.json"), read("civu-private-media-match-20261015-01/matched.private.json"), read("civu-private-media-match-20261015-01/authenticated-seal.json"), "6aa8fdb2326ae66b743355959fef5584b31ce4472371bebb961dbf8abc28f169")
	if e != nil {
		t.Fatal("verified metadata plan rejected", e)
	}
	if p.Counts.PrivateImages != 77 || p.Counts.PlaceImages != 75 || p.Counts.PlaceRelations != 148 || p.Counts.ReusedPlaceReferences != 58 || p.Counts.PlaceTombstones != 18 || p.Counts.MomentCovers != 4 || p.Counts.SystemArtworkArchives != 3 {
		t.Fatal("actual protected aggregate changed", p.Counts)
	}
	raw, _ := json.Marshal(p.Counts)
	t.Log(string(raw))
}
