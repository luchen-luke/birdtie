package postgres

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	arp "github.com/birdtie/birdtie/apps/api/internal/agentresultprojection"
	ec "github.com/birdtie/birdtie/apps/api/internal/entitycover"
	"github.com/birdtie/birdtie/apps/api/internal/foundation"
	pp "github.com/birdtie/birdtie/apps/api/internal/placephoto"
)

func TestPlacePhotoMediaRetentionExactOwnershipAndBounds(t *testing.T) {
	marker := "birdtie-human-media-owned:isolated-test"
	if !ValidMediaRetentionOwnership("birdtie_owned_migration_a1", marker) {
		t.Fatal("owned marker rejected")
	}
	for _, bad := range []struct{ db, marker string }{{"postgres", marker}, {"birdtie_owned_migration_a1", ""}, {"birdtie_owned_migration_a1", marker + "\n"}} {
		if ValidMediaRetentionOwnership(bad.db, bad.marker) {
			t.Fatal("ambiguous database allowed")
		}
	}
	store := &Store{}
	if _, e := store.PurgeHumanMediaDerivatives(context.Background(), "postgres", marker, 1); e == nil {
		t.Fatal("unowned store reached SQL")
	}
	if _, e := store.PurgeHumanMediaDerivatives(context.Background(), "birdtie_owned_x", marker, 101); e == nil {
		t.Fatal("unbounded purge")
	}
}

func TestPublicMomentPhotoRetentionIsExplicitAndSeparate(t *testing.T) {
	store := &Store{}
	if _, e := store.PurgeHumanMediaDerivatives(context.Background(), "birdtie_production", "", 1); e == nil {
		t.Fatal("public bytes reached unowned maintenance")
	}
	raw, e := json.Marshal(MediaRetentionResult{PlacePhotos: 1, EntityCovers: 2, PublicMomentPhotos: 3})
	if e != nil || !strings.Contains(string(raw), `"publicMomentPhotos":3`) {
		t.Fatal("new public family not separately counted")
	}
}

func TestPlacePhotoSharedComparatorPreservesSlicesAndLease(t *testing.T) {
	now := time.Now().UTC()
	base := arp.Receipt{Items: []arp.Item{}, Activities: []foundation.Activity{}, Places: []foundation.Place{}, PublicCommercialRefs: []arp.Ref{}, ObservedAt: now, ValidUntil: now.Add(30 * time.Second), Proof: "original-complete-source"}
	id := "9931ef56-8c9d-4241-9437-99373492c5a1"
	for _, kind := range []string{"empty", "nil", "place", "activity"} {
		t.Run(kind, func(t *testing.T) {
			old := base
			switch kind {
			case "nil":
				old.Items = nil
				old.Activities = nil
				old.Places = nil
			case "place":
				m := &pp.Metadata{EntityType: "place", EntityID: id, Version: 1, ThumbnailPath: pp.Path(id, 1), Width: 320, Height: 180, ByteCount: 200, SHA256: strings.Repeat("a", 64), ExpiresAt: now.Add(20 * time.Second)}
				old.Items = []arp.Item{{Entity: arp.Ref{Type: "place", ID: id}, Title: "原地点", PlacePhoto: m}}
				old.Places = []foundation.Place{{ID: id, Name: "原地点", PlacePhoto: m}}
			case "activity":
				ref := ec.Ref{Type: "activity", ID: id}
				m := &ec.Metadata{Ref: ref, Version: 1, ThumbnailPath: ec.Path(ref, 1), Width: 320, Height: 180, ByteCount: 200, SHA256: strings.Repeat("a", 64), ExpiresAt: now.Add(20 * time.Second)}
				old.Items = []arp.Item{{Entity: arp.Ref{Type: "activity", ID: id}, Title: "原活动", Cover: m}}
				old.Activities = []foundation.Activity{{ID: id, Title: "原活动", Cover: m}}
			}
			current := old
			current.ObservedAt = now.Add(time.Millisecond)
			if !unchangedOutputProjection(old, current) {
				t.Fatal("same empty/nil/current source rejected", kind)
			}
			changed := current
			changed.Proof = "different-authority"
			if unchangedOutputProjection(old, changed) {
				t.Fatal("source comparison weakened")
			}
			if kind == "place" {
				changed := current
				changed.Places = append([]foundation.Place{}, current.Places...)
				m := *changed.Places[0].PlacePhoto
				m.ExpiresAt = now.Add(10 * time.Second)
				changed.Places[0].PlacePhoto = &m
				if unchangedOutputProjection(old, changed) {
					t.Fatal("shortened actual source extended old lease")
				}
			}
		})
	}
	different := base
	different.Activities = nil
	if unchangedOutputProjection(base, different) {
		t.Fatal("nil/empty data change erased")
	}
}
