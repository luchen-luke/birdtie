package postgres

import (
	"bytes"
	"context"
	"crypto/sha256"
	"image"
	"io"
	"net/url"
	"testing"

	"github.com/birdtie/birdtie/apps/api/internal/content"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	"github.com/birdtie/birdtie/apps/api/internal/legacymedia"
)

func TestMapContentDisplayNativeCurrentAuthority(t *testing.T) {
	f := legacyMigrationNativeFixture(t)
	ctx := f.Context
	input, raw := map170SyntheticInput(t, true)
	for _, apply := range []func() error{
		func() error { _, e := f.Importer.Apply(ctx, legacyTestRunID, input.Source, input.Manifest); return e },
		func() error {
			_, e := f.Importer.ApplyPersonalArchives(ctx, legacyTestRunID, input.Source, input.Manifest)
			return e
		},
		func() error {
			_, e := f.Importer.ApplyNativeAdaptation(ctx, legacyTestRunID, input.Source, input.Manifest)
			return e
		},
		func() error {
			_, e := f.Importer.ApplyLegacyPrivateMedia(ctx, legacyTestRunID, input, func(context.Context, legacymedia.Record) (io.ReadCloser, error) {
				return io.NopCloser(bytes.NewReader(raw)), nil
			})
			return e
		},
		func() error {
			_, e := f.Importer.ApplyContextAdaptation(ctx, legacyTestRunID, input.Source, input.Manifest)
			return e
		},
		func() error {
			_, e := f.Importer.ApplyLegacyMapRestore(ctx, legacyTestRunID, input.Source, input.Manifest)
			return e
		},
		func() error {
			_, e := f.Importer.ApplyLegacyMapAnchorRestore(ctx, legacyTestRunID, input.Source, input.Manifest)
			return e
		},
	} {
		if e := apply(); e != nil {
			t.Fatal("synthetic import", e)
		}
	}
	viewer, _ := legacyMigrationUUID()
	digest := sha256.Sum256([]byte("display29-synthetic-reader"))
	if _, e := f.Pool.Exec(ctx, `INSERT INTO accounts(id,account_type,status)VALUES($1,'person','active')`, viewer); e != nil {
		t.Fatal(e)
	}
	var session string
	if e := f.Pool.QueryRow(ctx, `WITH cl AS(SELECT clock_timestamp() at) INSERT INTO sessions(account_id,token_sha256,authentication_method,expires_at,idle_expires_at)SELECT $1,$2,'synthetic_test',at+interval '1 hour',at+interval '1 hour'FROM cl RETURNING id`, viewer, digest[:]).Scan(&session); e != nil {
		t.Fatal(e)
	}
	store := New(f.Pool, true)
	actor := identity.Actor{ID: viewer, AccountType: "person"}
	mapDisplayCache.Lock()
	mapDisplayCache.items = map[string]mapMarkerThumbnail{}
	mapDisplayCache.bytes = 0
	mapDisplayCache.Unlock()
	page, e := store.ReadHumanMapContent(ctx, digest, actor, content.MapContentQuery{CityID: "aberdeen-gb", Bounds: content.MapContentBounds{West: -3, South: 56, East: -1, North: 58}, Zoom: 12})
	if e != nil {
		t.Fatal("viewport", e)
	}
	mapDisplayCache.Lock()
	cached := len(mapDisplayCache.items)
	mapDisplayCache.Unlock()
	if cached != 0 {
		t.Fatal("viewport eagerly generated reading images")
	}
	var selected content.MapContentMoment
	var media content.MapContentMedia
	for _, pin := range page.Pins {
		for _, selector := range pin.Moments {
			detail, e := store.ReadHumanMapContentMoment(ctx, digest, actor, selector.ID)
			if e != nil {
				t.Fatal("detail", e)
			}
			for _, item := range detail.Media {
				if item.Kind == "IMAGE" {
					u, _ := url.Parse(item.ContentPath)
					if u.Query().Get("variant") != "display" || item.ByteCount > mapDisplayMaxBytes || item.Width > 1280 || item.Height > 1280 {
						t.Fatal("display descriptor missing")
					}
					b, e := store.ReadHumanMapContentBinary(ctx, digest, actor, "moments", detail.ID, item.ID, item.SourceVersion, "display")
					if e != nil || len(b.Bytes) != item.ByteCount || legacymedia.Hash(b.Bytes) != item.SHA256 {
						t.Fatal("all images remain reachable", e)
					}
					cfg, _, e := image.DecodeConfig(bytes.NewReader(b.Bytes))
					if e != nil || cfg.Width != item.Width || cfg.Height != item.Height {
						t.Fatal("real derivative metadata", e)
					}
					if e = b.ResponseProof.RevalidateHumanControl(ctx); e != nil {
						t.Fatal("fresh display proof", e)
					}
					selected, media = detail, item
				}
			}
		}
	}
	if media.ID == "" {
		t.Fatal("no fixture image")
	}
	var originalBefore string
	if e = f.Pool.QueryRow(ctx, `SELECT derivative_sha256 FROM private_moment_images WHERE id=$1`, media.ID).Scan(&originalBefore); e != nil {
		t.Fatal(e)
	}
	b, e := store.ReadHumanMapContentBinary(ctx, digest, actor, "moments", selected.ID, media.ID, media.SourceVersion, "display")
	if e != nil {
		t.Fatal("cached image", e)
	}
	if _, e = store.ReadHumanMapContentBinary(ctx, digest, actor, "moments", selected.ID, media.ID, media.SourceVersion, ""); e == nil {
		t.Fatal("display selector accepted as original")
	}
	if _, e = store.ReadHumanMapContentBinary(ctx, digest, actor, "moments", selected.ID, media.ID, media.SourceVersion, "marker"); e == nil {
		t.Fatal("display selector accepted as marker")
	}
	if _, e = f.Pool.Exec(ctx, `INSERT INTO account_blocks(blocker_account_id,blocked_account_id)VALUES($1,$2)`, viewer, selected.Author.AccountID); e != nil {
		t.Fatal(e)
	}
	if e = b.ResponseProof.RevalidateHumanControl(ctx); e == nil {
		t.Fatal("blocked cached display proof still current")
	}
	if _, e = store.ReadHumanMapContentBinary(ctx, digest, actor, "moments", selected.ID, media.ID, media.SourceVersion, "display"); e == nil {
		t.Fatal("blocked cached image readable")
	}
	if _, e = f.Pool.Exec(ctx, `DELETE FROM account_blocks WHERE blocker_account_id=$1 AND blocked_account_id=$2`, viewer, selected.Author.AccountID); e != nil {
		t.Fatal(e)
	}
	var originalAfter string
	if e = f.Pool.QueryRow(ctx, `SELECT derivative_sha256 FROM private_moment_images WHERE id=$1`, media.ID).Scan(&originalAfter); e != nil || originalBefore != originalAfter {
		t.Fatal("original mutated", e)
	}
	if _, e = f.Pool.Exec(ctx, `UPDATE sessions SET revoked_at=clock_timestamp() WHERE id=$1`, session); e != nil {
		t.Fatal(e)
	}
	if _, e = store.ReadHumanMapContentBinary(ctx, digest, actor, "moments", selected.ID, media.ID, media.SourceVersion, "display"); e == nil {
		t.Fatal("logged out image readable")
	}
}
