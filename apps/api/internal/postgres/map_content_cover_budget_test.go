package postgres

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"

	"github.com/birdtie/birdtie/apps/api/internal/content"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	"github.com/birdtie/birdtie/apps/api/internal/legacymedia"
)

func coverBudgetImage(source, id string) content.MapContentImage {
	return content.MapContentImage{Kind: "MOMENT_IMAGE", ID: id, Revision: 1,
		VisibilitySource: "SELF_PRIVATE", MIMEType: "image/jpeg", Width: 300, Height: 200,
		ByteCount: 100, SHA256: strings.Repeat("a", 64),
		SourceVersion: content.MomentControlHash([]any{source, id}),
		ContentPath:   "/v1/me/map-content/moments/" + source + "/media/" + id + "/content"}
}
func coverBudgetID(i int) string { return fmt.Sprintf("18400000-0000-4000-8000-%012d", i+1) }
func coverBudgetPin(id, moment string) content.MapContentPin {
	return content.MapContentPin{MarkerID: id, Kind: "MOMENT_ANCHOR", MomentID: moment,
		VisibilitySources: []string{"SELF_PRIVATE"},
		Anchor:            content.MapContentAnchor{CoordinateSystem: "wgs84", Precision: "point", Latitude: 31.2, Longitude: 121.4}}
}

func TestMapContentCoverBudgetAllRepeatedProjections(t *testing.T) {
	pins := []content.MapContentPin{}
	targets := []mapContentCoverTarget{}
	for i := 0; i < 40; i++ {
		mid := coverBudgetID(0)
		if i == 39 {
			mid = coverBudgetID(1)
		}
		pin := coverBudgetPin(fmt.Sprintf("anchor:%d", i), mid)
		pins = append(pins, pin)
		targets = append(targets, mapContentCoverTarget{i, pin, ""})
	}
	metadata, thumbnails := 0, 0
	err := attachMapContentCovers(context.Background(), pins, targets, "",
		func(pin content.MapContentPin) ([]content.MapContentImage, error) {
			metadata++
			return []content.MapContentImage{coverBudgetImage(pin.MomentID, coverBudgetID(100))}, nil
		}, func(image content.MapContentImage) (*content.MapContentImage, error) {
			thumbnails++
			return &image, nil
		})
	if err != nil || metadata != 2 || thumbnails != 2 {
		t.Fatal("projection count spent metadata/thumbnail slots", err, metadata, thumbnails)
	}
	for i, pin := range pins {
		if pin.Cover == nil || pin.MarkerID != targets[i].pin.MarkerID || pin.Anchor != targets[i].pin.Anchor {
			t.Fatal("13th/later independent projection lost cover or original identity", i)
		}
	}
	pins[0].Cover.Width = 1
	if pins[1].Cover.Width != 300 {
		t.Fatal("shared request result mutated another pin descriptor")
	}
}

func TestMapContentCoverBudgetSelectedAndSourceLimit(t *testing.T) {
	pins := []content.MapContentPin{}
	targets := []mapContentCoverTarget{}
	for i := 0; i < 25; i++ {
		pin := coverBudgetPin(fmt.Sprintf("anchor:%d", i), coverBudgetID(i))
		pins = append(pins, pin)
		targets = append(targets, mapContentCoverTarget{i, pin, ""})
	}
	duplicate := coverBudgetPin("late-repeat", pins[0].MomentID)
	pins = append(pins, duplicate)
	targets = append(targets, mapContentCoverTarget{25, duplicate, ""})
	ids := []string{}
	calls := 0
	err := attachMapContentCovers(context.Background(), pins, targets, pins[24].MarkerID,
		func(pin content.MapContentPin) ([]content.MapContentImage, error) {
			ids = append(ids, pin.MomentID)
			return []content.MapContentImage{coverBudgetImage(pin.MomentID, coverBudgetID(100))}, nil
		},
		func(image content.MapContentImage) (*content.MapContentImage, error) { calls++; return &image, nil })
	if err != nil || len(ids) != 24 || calls != 24 || ids[0] != pins[24].MomentID {
		t.Fatal("selected-first dual source budget", err, len(ids), calls, ids)
	}
	if pins[23].Cover != nil || pins[24].Cover == nil || pins[25].Cover == nil {
		t.Fatal("new source escaped budget or admitted late repeat lost cover")
	}
}

func TestMapContentCoverBudgetExactDescriptorsAndNoSHABorrow(t *testing.T) {
	t.Run("one-source-multiple-media", func(t *testing.T) {
		pins := []content.MapContentPin{}
		targets := []mapContentCoverTarget{}
		images := []content.MapContentImage{}
		for i := 0; i < 25; i++ {
			images = append(images, coverBudgetImage(coverBudgetID(0), coverBudgetID(i+100)))
			pin := coverBudgetPin(fmt.Sprintf("anchor:%d", i), coverBudgetID(0))
			pins = append(pins, pin)
			targets = append(targets, mapContentCoverTarget{i, pin, images[i].ID})
		}
		repeat := coverBudgetPin("late-exact-repeat", coverBudgetID(0))
		pins = append(pins, repeat)
		targets = append(targets, mapContentCoverTarget{25, repeat, images[0].ID})
		metadata, thumbs := 0, 0
		err := attachMapContentCovers(context.Background(), pins, targets, pins[24].MarkerID,
			func(content.MapContentPin) ([]content.MapContentImage, error) { metadata++; return images, nil },
			func(image content.MapContentImage) (*content.MapContentImage, error) { thumbs++; return &image, nil })
		if err != nil || metadata != 1 || thumbs != 24 || pins[24].Cover == nil || pins[23].Cover != nil || pins[25].Cover == nil {
			t.Fatal("descriptor budget/late reuse", err, metadata, thumbs)
		}
		if pins[0].Cover.ID != images[0].ID || pins[24].Cover.ID != images[24].ID {
			t.Fatal("explicit media identity replaced by equal file SHA")
		}
	})
	t.Run("same-sha-different-sources", func(t *testing.T) {
		pins := []content.MapContentPin{coverBudgetPin("a", coverBudgetID(0)), coverBudgetPin("b", coverBudgetID(1))}
		targets := []mapContentCoverTarget{{0, pins[0], ""}, {1, pins[1], ""}}
		metadata, thumbs := 0, 0
		err := attachMapContentCovers(context.Background(), pins, targets, "",
			func(pin content.MapContentPin) ([]content.MapContentImage, error) {
				metadata++
				return []content.MapContentImage{coverBudgetImage(pin.MomentID, coverBudgetID(100))}, nil
			},
			func(image content.MapContentImage) (*content.MapContentImage, error) { thumbs++; return &image, nil })
		if err != nil || metadata != 2 || thumbs != 2 || pins[0].Cover.SHA256 != pins[1].Cover.SHA256 || pins[0].Cover.SourceVersion == pins[1].Cover.SourceVersion || pins[0].Cover.ContentPath == pins[1].Cover.ContentPath {
			t.Fatal("file equality borrowed another source grant", err, metadata, thumbs)
		}
	})
}

func TestMapContentCoverBudgetEmptyFailureAndCancellation(t *testing.T) {
	pins := []content.MapContentPin{}
	targets := []mapContentCoverTarget{}
	for i := 0; i < 40; i++ {
		pin := coverBudgetPin(fmt.Sprintf("anchor:%d", i), coverBudgetID(i/2))
		pins = append(pins, pin)
		targets = append(targets, mapContentCoverTarget{i, pin, ""})
	}
	metadata, thumbs := 0, 0
	err := attachMapContentCovers(context.Background(), pins, targets, "",
		func(pin content.MapContentPin) ([]content.MapContentImage, error) {
			metadata++
			if pin.MomentID == coverBudgetID(0) {
				return []content.MapContentImage{coverBudgetImage(pin.MomentID, coverBudgetID(100))}, nil
			}
			return nil, content.ErrUnavailable
		},
		func(content.MapContentImage) (*content.MapContentImage, error) {
			thumbs++
			return nil, content.ErrUnavailable
		})
	if err != nil || metadata != 20 || thumbs != 1 {
		t.Fatal("failed attempts retried per anchor", err, metadata, thumbs)
	}
	for _, pin := range pins {
		if pin.Cover != nil {
			t.Fatal("failure attached an old cover")
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	thumbs = 0
	err = attachMapContentCovers(ctx, pins[:1], targets[:1], "",
		func(pin content.MapContentPin) ([]content.MapContentImage, error) {
			cancel()
			return []content.MapContentImage{coverBudgetImage(pin.MomentID, coverBudgetID(100))}, nil
		},
		func(image content.MapContentImage) (*content.MapContentImage, error) { thumbs++; return &image, nil })
	if !errors.Is(err, content.ErrUnavailable) || thumbs != 0 || pins[0].Cover != nil {
		t.Fatal("cancelled metadata created late thumbnail", err, thumbs)
	}
}

func coverBudgetNativeInput(t *testing.T) (LegacyPrivateMediaInput, []byte) {
	t.Helper()
	in, raw := map170SyntheticInput(t)
	var doc struct {
		Tables map[string][]map[string]any `json:"tables"`
	}
	d := json.NewDecoder(bytes.NewReader(in.Source))
	d.UseNumber()
	if d.Decode(&doc) != nil {
		t.Fatal("synthetic source")
	}
	in.Source, in.Manifest = legacySyntheticInput(t, func(tables map[string]any) {
		for key, rows := range doc.Tables {
			out := []any{}
			for _, row := range rows {
				out = append(out, row)
			}
			tables[key] = out
		}
		clone := func(original map[string]any) map[string]any {
			row := map[string]any{}
			for k, v := range original {
				row[k] = v
			}
			return row
		}
		base := tables["places"].([]any)[0].(map[string]any)
		var link map[string]any
		for _, value := range tables["note_places"].([]any) {
			row := value.(map[string]any)
			if row["note_id"] == "14600000-0000-4000-8000-000000000020" {
				link = row
				break
			}
		}
		if link == nil {
			t.Fatal("synthetic original moment-place source")
		}
		for i := 0; i < 25; i++ {
			id := coverBudgetID(i + 200)
			place := clone(base)
			place["id"] = id
			place["name"] = fmt.Sprintf("隔离原地点%d", i)
			tables["places"] = append(tables["places"].([]any), place)
			relation := clone(link)
			relation["place_id"] = id
			relation["position"] = i + 100
			tables["note_places"] = append(tables["note_places"].([]any), relation)
			reference := clone(tables["place_provider_refs"].([]any)[0].(map[string]any))
			reference["place_id"] = id
			reference["external_id"] = fmt.Sprintf("node/184%06d", i)
			reference["source_payload"] = map[string]any{"promotedFrom": "local-civu-catalog", "sourcePlaceId": id}
			tables["place_provider_refs"] = append(tables["place_provider_refs"].([]any), reference)
		}
	})
	var header map[string]json.RawMessage
	_ = json.Unmarshal(in.Source, &header)
	var matched map[string]any
	_ = json.Unmarshal(in.Matched, &matched)
	matched["source"] = header["source"]
	matched["sourceFileSha256"] = legacymedia.Hash(in.Source)
	in.Matched, _ = json.Marshal(matched)
	var seal map[string]any
	_ = json.Unmarshal(in.AuthenticatedSeal, &seal)
	seal["matcherFileSHA256"] = legacymedia.Hash(in.Matched)
	in.AuthenticatedSeal, _ = json.Marshal(seal)
	in.ExpectedSealSHA256 = legacymedia.Hash(in.AuthenticatedSeal)
	return in, raw
}

// Real map/metadata/marker-binary chain in a marked random child DB only.
func TestMapContentCoverBudgetNativeAllReturnedProjections(t *testing.T) {
	f := legacyMigrationNativeFixture(t)
	in, raw := coverBudgetNativeInput(t)
	ctx := f.Context
	for _, apply := range []func() error{
		func() error { _, e := f.Importer.Apply(ctx, legacyTestRunID, in.Source, in.Manifest); return e },
		func() error {
			_, e := f.Importer.ApplyPersonalArchives(ctx, legacyTestRunID, in.Source, in.Manifest)
			return e
		},
		func() error {
			_, e := f.Importer.ApplyNativeAdaptation(ctx, legacyTestRunID, in.Source, in.Manifest)
			return e
		},
		func() error {
			_, e := f.Importer.ApplyLegacyPrivateMedia(ctx, legacyTestRunID, in, func(context.Context, legacymedia.Record) (io.ReadCloser, error) {
				return io.NopCloser(bytes.NewReader(raw)), nil
			})
			return e
		},
		func() error {
			_, e := f.Importer.ApplyContextAdaptation(ctx, legacyTestRunID, in.Source, in.Manifest)
			return e
		},
		func() error {
			_, e := f.Importer.ApplyLegacyMapRestore(ctx, legacyTestRunID, in.Source, in.Manifest)
			return e
		},
	} {
		if err := apply(); err != nil {
			t.Fatal("synthetic preserved import", err)
		}
	}
	viewer, err := legacyMigrationUUID()
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256([]byte("synthetic-cover-budget-isolated-reader"))
	var session, mid string
	if _, err = f.Pool.Exec(ctx, `INSERT INTO accounts(id,account_type,status)VALUES($1,'person','active')`, viewer); err != nil {
		t.Fatal(err)
	}
	if err = f.Pool.QueryRow(ctx, `INSERT INTO sessions(account_id,token_sha256,authentication_method,expires_at,idle_expires_at)VALUES($1,$2,'synthetic_test',clock_timestamp()+interval '1 hour',clock_timestamp()+interval '30 minutes') RETURNING id`, viewer, digest[:]).Scan(&session); err != nil {
		t.Fatal(err)
	}
	if err = f.Pool.QueryRow(ctx, `SELECT target_id FROM legacy_native_adaptation_entities WHERE kind='moment' AND source_key='14600000-0000-4000-8000-000000000020'`).Scan(&mid); err != nil {
		t.Fatal(err)
	}
	store := New(f.Pool, true)
	actor := identity.Actor{ID: viewer, AccountType: "person"}
	q := content.MapContentQuery{CityID: "aberdeen-gb", Bounds: content.MapContentBounds{West: -2.12, South: 57.14, East: -2.08, North: 57.16}, Zoom: 17}
	page, err := store.ReadHumanMapContent(ctx, digest, actor, q)
	if err != nil {
		t.Fatal("real map output", err)
	}
	count := 0
	var sample *content.MapContentImage
	for _, pin := range page.Pins {
		if pin.MomentID != mid {
			continue
		}
		count++
		if pin.Cover == nil {
			t.Fatal("13th/later current map projection lost its verified marker cover", count)
		}
		if pin.Cover.Width > 160 || pin.Cover.Height > 160 || pin.Cover.VisibilitySource != "CIVU_PUBLIC_MOMENT" {
			t.Fatal("original marker/source contract changed")
		}
		sample = pin.Cover
	}
	if count < 25 || sample == nil {
		t.Fatal("fixture did not return real duplicate projections", count)
	}
	binary, err := store.ReadHumanMapContentBinary(ctx, digest, actor, "moments", mid, sample.ID, sample.SourceVersion, "marker")
	if err != nil || len(binary.Bytes) != sample.ByteCount {
		t.Fatal("actual bounded marker bytes", err)
	}
	clear(binary.Bytes)
	if _, err = f.Pool.Exec(ctx, `UPDATE sessions SET revoked_at=clock_timestamp() WHERE id=$1`, session); err != nil {
		t.Fatal(err)
	}
	if err = page.ResponseProof.RevalidateHumanControl(ctx); err == nil {
		t.Fatal("revoked source survived reused thumbnail result")
	}
}
