package postgres

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"image"
	"image/color"
	"image/jpeg"
	"io"
	"net/url"
	"testing"
	"time"

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
	lazy, e := store.ReadHumanMapContentMomentLazy(ctx, digest, actor, selected.ID)
	if e != nil {
		t.Fatal("foreign lazy detail", e)
	}
	var original content.MapContentMedia
	for _, item := range lazy.Media {
		if item.ID == media.ID {
			original = item
		}
	}
	descriptor, e := store.ReadHumanMapContentDisplayDescriptor(ctx, digest, actor, selected.ID, original.ID, original.SourceVersion)
	if e != nil || descriptor.Media.SHA256 != media.SHA256 {
		t.Fatal("foreign lazy descriptor", e)
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
	if e = descriptor.ResponseProof.RevalidateHumanControl(ctx); e == nil {
		t.Fatal("blocked lazy descriptor proof still current")
	}
	if _, e = store.ReadHumanMapContentDisplayDescriptor(ctx, digest, actor, selected.ID, original.ID, original.SourceVersion); e == nil {
		t.Fatal("blocked cached lazy descriptor readable")
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

func TestMapContentLazy200NativeColdMetadataAndSelectedReads(t *testing.T) {
	f := legacyMigrationNativeFixture(t)
	ctx := f.Context
	owner, _ := legacyMigrationUUID()
	mid, _ := legacyMigrationUUID()
	pid, _ := legacyMigrationUUID()
	digest := sha256.Sum256([]byte("synthetic-lazy-200-reader"))
	if _, e := f.Pool.Exec(ctx, `INSERT INTO accounts(id,account_type,status)VALUES($1,'person','active')`, owner); e != nil {
		t.Fatal(e)
	}
	var session string
	if e := f.Pool.QueryRow(ctx, `WITH cl AS(SELECT clock_timestamp() at) INSERT INTO sessions(account_id,token_sha256,authentication_method,expires_at,idle_expires_at)SELECT $1,$2,'synthetic_test',at+interval '1 hour',at+interval '1 hour'FROM cl RETURNING id`, owner, digest[:]).Scan(&session); e != nil {
		t.Fatal(e)
	}
	if _, e := f.Pool.Exec(ctx, `INSERT INTO moments(id,author_account_id,title,body,status,visibility,location_precision)VALUES($1,$2,'','Synthetic lazy 200 image fixture','draft','private','none')`, mid, owner); e != nil {
		t.Fatal(e)
	}
	if _, e := f.Pool.Exec(ctx, `INSERT INTO places(id,name,category_code,latitude,longitude,coordinate_system,location_precision,publication_status,source_label,source_ref,maintainer_label)VALUES($1,'Synthetic lazy point','park',10,20,'wgs84','point','draft','LOCAL_SYNTHETIC_FIXTURE','disposable://lazy200','synthetic')`, pid); e != nil {
		t.Fatal(e)
	}
	if _, e := f.Pool.Exec(ctx, `INSERT INTO moment_location_links(id,moment_id,kind,place_id,precision,position)VALUES(gen_random_uuid(),$1,'PLACE',$2,'place',0)`, mid, pid); e != nil {
		t.Fatal(e)
	}
	small := image.NewRGBA(image.Rect(0, 0, 240, 320))
	for y := 0; y < 320; y++ {
		for x := 0; x < 240; x++ {
			small.SetRGBA(x, y, color.RGBA{uint8(x), uint8(y), 100, 255})
		}
	}
	var encoded bytes.Buffer
	if e := jpeg.Encode(&encoded, small, &jpeg.Options{Quality: 90}); e != nil {
		t.Fatal(e)
	}
	raw := encoded.Bytes()
	hash := legacymedia.Hash(raw)
	if _, e := f.Pool.Exec(ctx, `WITH cl AS(SELECT clock_timestamp() at) INSERT INTO private_moment_images(id,owner_account_id,moment_id,operation_id,source_binding,moment_revision,session_id,mime_type,input_size,input_sha256,pixel_risk,purpose,status,revision,derivative,derivative_sha256,preview_expires_at,retain_until,created_at,updated_at)
 SELECT md5('lazy-200-image:'||i)::uuid,$1,$2,md5('lazy-200-image:'||i)::uuid,'synthetic-lazy-200',1,$3,'image/jpeg',$4,$5,'UNKNOWN','PRIVATE_MOMENT_ATTACHMENT','ready_private',1,$6,$5,cl.at+interval '5 minutes',cl.at+interval '1 hour',cl.at+i*interval '1 microsecond',cl.at FROM cl,generate_series(1,200)i`, owner, mid, session, len(raw), hash, raw); e != nil {
		t.Fatal(e)
	}
	if _, e := f.Pool.Exec(ctx, `INSERT INTO media_assets(id,owner_account_id,storage_key,mime_type,byte_size,sha256_hex,state)SELECT id,owner_account_id,'synthetic/lazy200/'||id,'image/jpeg',octet_length(derivative),derivative_sha256,'ready_private'FROM private_moment_images WHERE moment_id=$1`, mid); e != nil {
		t.Fatal(e)
	}
	mapDisplayCache.Lock()
	mapDisplayCache.items = map[string]mapMarkerThumbnail{}
	mapDisplayCache.bytes = 0
	mapDisplayCache.Unlock()
	mapDisplayMetadataCache.Lock()
	mapDisplayMetadataCache.items = map[string]mapDisplayMetadata{}
	mapDisplayMetadataCache.Unlock()
	s := New(f.Pool, true)
	a := identity.Actor{ID: owner, AccountType: "person"}
	decodes, rawReads, headers := mapDisplayDecodeCount.Load(), mapDisplayRawReadBytes.Load(), mapContentHeaderReadBytes.Load()
	started := time.Now()
	detail, e := s.ReadHumanMapContentMomentLazy(ctx, digest, a, mid)
	if e != nil || len(detail.Media) != 200 {
		t.Fatal("200 lazy metadata", e, len(detail.Media))
	}
	if e = detail.ResponseProof.RevalidateHumanControl(ctx); e != nil {
		t.Fatal("lazy detail proof", e)
	}
	detailMS := float64(time.Since(started).Microseconds()) / 1000
	jsonBytes, _ := json.Marshal(detail)
	if mapDisplayDecodeCount.Load() != decodes || mapDisplayRawReadBytes.Load() != rawReads {
		t.Fatal("lazy metadata/proof decoded or fetched original bodies")
	}
	for _, m := range detail.Media {
		if m.ByteCount != len(raw) || m.SHA256 != hash || m.Width != 240 || m.Height != 320 || m.DisplayDescriptorPath != content.MapContentDisplayDescriptorPath(mid, m.ID, m.SourceVersion) {
			t.Fatal("lazy list guessed output metadata")
		}
	}
	page, e := s.ReadHumanMapContentPlaceLazy(ctx, digest, a, pid, "")
	if e != nil || len(page.Items) != 1 || len(page.Items[0].Media) != 200 {
		t.Fatal("lazy place list", e)
	}
	if e = page.ResponseProof.RevalidateHumanControl(ctx); e != nil {
		t.Fatal("lazy place proof", e)
	}
	if mapDisplayDecodeCount.Load() != decodes || mapDisplayRawReadBytes.Load() != rawReads {
		t.Fatal("lazy place proof returned to eager")
	}
	metadataHeaders := mapContentHeaderReadBytes.Load() - headers
	selectedTimes := []float64{}
	var first content.MapContentDisplayDescriptor
	var firstBinary content.MapContentBinary
	for i := 0; i < 2; i++ {
		original := detail.Media[i]
		headerBefore := mapContentHeaderReadBytes.Load()
		started = time.Now()
		v, e := s.ReadHumanMapContentDisplayDescriptor(ctx, digest, a, mid, original.ID, original.SourceVersion)
		if e != nil || !v.Valid(time.Now()) || v.Media.ID != original.ID || v.OriginalSourceVersion != original.SourceVersion {
			t.Fatal("selected descriptor", e)
		}
		if e = v.ResponseProof.RevalidateHumanControl(ctx); e != nil {
			t.Fatal("selected descriptor proof", e)
		}
		b, e := s.ReadHumanMapContentBinary(ctx, digest, a, "moments", mid, original.ID, v.Media.SourceVersion, "display")
		if e != nil || legacymedia.Hash(b.Bytes) != v.Media.SHA256 || len(b.Bytes) != v.Media.ByteCount {
			t.Fatal("selected bytes", e)
		}
		if e = b.ResponseProof.RevalidateHumanControl(ctx); e != nil {
			t.Fatal("selected bytes proof", e)
		}
		cfg, _, e := image.DecodeConfig(bytes.NewReader(b.Bytes))
		if e != nil || cfg.Width != v.Media.Width || cfg.Height != v.Media.Height {
			t.Fatal("actual selected JPEG", e)
		}
		if mapDisplayDecodeCount.Load() != decodes+uint64(i+1) || mapDisplayRawReadBytes.Load() != rawReads+uint64((i+1)*len(raw)) {
			t.Fatal("selected reads were not exactly 0 to 1 to 2 encodes")
		}
		if mapContentHeaderReadBytes.Load()-headerBefore != uint64(4*len(raw)) {
			t.Fatal("selected requests scanned unselected media headers")
		}
		selectedTimes = append(selectedTimes, float64(time.Since(started).Microseconds())/1000)
		if i == 0 {
			first, firstBinary = v, b
		}
	}
	firstOriginal := detail.Media[0]
	if _, e = s.ReadHumanMapContentDisplayDescriptor(ctx, digest, a, mid, detail.Media[1].ID, firstOriginal.SourceVersion); e == nil {
		t.Fatal("wrong media accepted original source")
	}
	if _, e = s.ReadHumanMapContentDisplayDescriptor(ctx, digest, a, mid, firstOriginal.ID, first.Media.SourceVersion); e == nil {
		t.Fatal("display source accepted as original")
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if _, e = s.ReadHumanMapContentDisplayDescriptor(cancelled, digest, a, mid, firstOriginal.ID, firstOriginal.SourceVersion); e == nil {
		t.Fatal("cancelled descriptor succeeded")
	}
	// Compare the identical fixture with genuinely cold caches. The old query
	// still exposes ready display descriptors and remains valid for old clients.
	mapDisplayCache.Lock()
	mapDisplayCache.items = map[string]mapMarkerThumbnail{}
	mapDisplayCache.bytes = 0
	mapDisplayCache.Unlock()
	mapDisplayMetadataCache.Lock()
	mapDisplayMetadataCache.items = map[string]mapDisplayMetadata{}
	mapDisplayMetadataCache.Unlock()
	eagerDecodes, eagerRawReads := mapDisplayDecodeCount.Load(), mapDisplayRawReadBytes.Load()
	started = time.Now()
	eager, e := s.ReadHumanMapContentMoment(ctx, digest, a, mid)
	if e != nil || len(eager.Media) != 200 || eager.SourceVersion == detail.SourceVersion {
		t.Fatal("default eager compatibility", e)
	}
	for _, m := range eager.Media {
		if m.DisplayDescriptorPath != "" {
			t.Fatal("old client received additive lazy field")
		}
		u, _ := url.Parse(m.ContentPath)
		if u.Query().Get("variant") != "display" {
			t.Fatal("old display path changed")
		}
	}
	if e = eager.ResponseProof.RevalidateHumanControl(ctx); e != nil {
		t.Fatal("default eager proof", e)
	}
	eagerMS := float64(time.Since(started).Microseconds()) / 1000
	eagerJSON, _ := json.Marshal(eager)
	if mapDisplayDecodeCount.Load()-eagerDecodes != 200 || mapDisplayRawReadBytes.Load()-eagerRawReads != uint64(200*len(raw)) {
		t.Fatal("cold eager fixture did not encode every image exactly once")
	}
	if _, e = f.Pool.Exec(ctx, `UPDATE private_moment_images SET revision=revision+1 WHERE id=$1`, firstOriginal.ID); e != nil {
		t.Fatal(e)
	}
	if e = first.ResponseProof.RevalidateHumanControl(ctx); e == nil {
		t.Fatal("changed media descriptor proof current")
	}
	if _, e = s.ReadHumanMapContentBinary(ctx, digest, a, "moments", mid, firstOriginal.ID, first.Media.SourceVersion, "display"); e == nil {
		t.Fatal("changed media bytes current")
	}
	if _, e = f.Pool.Exec(ctx, `UPDATE moments SET status='withdrawn',revision=revision+1 WHERE id=$1`, mid); e != nil {
		t.Fatal(e)
	}
	if e = firstBinary.ResponseProof.RevalidateHumanControl(ctx); e == nil {
		t.Fatal("withdrawn cached bytes proof current")
	}
	if _, e = s.ReadHumanMapContentDisplayDescriptor(ctx, digest, a, mid, detail.Media[1].ID, detail.Media[1].SourceVersion); e == nil {
		t.Fatal("withdrawn descriptor accessible")
	}
	if _, e = f.Pool.Exec(ctx, `UPDATE sessions SET revoked_at=clock_timestamp()WHERE id=$1`, session); e != nil {
		t.Fatal(e)
	}
	if _, e = s.ReadHumanMapContentDisplayDescriptor(ctx, digest, a, mid, firstOriginal.ID, firstOriginal.SourceVersion); e == nil {
		t.Fatal("logout descriptor accessible")
	}
	metrics := map[string]any{"fixtureImages": 200, "fixtureWidth": 240, "fixtureHeight": 320, "fixtureImageBytes": len(raw), "coldLazyDetailAndProofMS": detailMS, "lazyDetailJSONBytes": len(jsonBytes), "detailAndPlaceWithProofHeaderBytes": metadataHeaders, "lazyDetailAndPlaceFullPixelDecodes": 0, "lazyDetailAndPlaceDisplayBodyFetchBytes": 0, "selectedDescriptorProofBytesProofMS": selectedTimes, "selectedFullPixelDecodes": []int{1, 2}, "selectedBodyBytes": 2 * len(raw), "coldEagerDetailAndProofMS": eagerMS, "eagerDetailJSONBytes": len(eagerJSON), "eagerFullPixelDecodes": 200, "eagerDisplayBodyFetchBytes": 200 * len(raw), "note": "Synthetic isolated PG timings; not 200 full-size image end-to-end performance"}
	logged, _ := json.Marshal(metrics)
	t.Log(string(logged))
}
