package postgres

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	"github.com/birdtie/birdtie/apps/api/internal/legacymedia"
	la "github.com/birdtie/birdtie/apps/api/internal/legacymomentauthor"
	rm "github.com/birdtie/birdtie/apps/api/internal/legacyreferencemedia"
	"io"
	"net/url"
	"reflect"
	"testing"
)

func map172Input(t *testing.T) (LegacyPrivateMediaInput, []byte) {
	input, raw := map170SyntheticInput(t)
	var doc struct {
		Tables map[string][]map[string]any `json:"tables"`
	}
	d := json.NewDecoder(bytes.NewReader(input.Source))
	d.UseNumber()
	if d.Decode(&doc) != nil {
		t.Fatal("synthetic source")
	}
	for _, m := range doc.Tables["media_assets"] {
		if m["kind"] == "image" && m["status"] == "ready" {
			for _, u := range doc.Tables["users"] {
				if u["id"] == m["owner_id"] {
					u["avatar_url"] = "https://47.110.154.133/media/" + m["id"].(string)
					m["public_url"] = u["avatar_url"]
					break
				}
			}
			break
		}
	}
	input.Source, input.Manifest = legacySyntheticInput(t, func(tables map[string]any) {
		for k, rows := range doc.Tables {
			values := []any{}
			for _, row := range rows {
				values = append(values, row)
			}
			tables[k] = values
		}
	})
	var header map[string]json.RawMessage
	json.Unmarshal(input.Source, &header)
	var matched map[string]any
	json.Unmarshal(input.Matched, &matched)
	matched["source"] = header["source"]
	matched["sourceFileSha256"] = legacymedia.Hash(input.Source)
	input.Matched, _ = json.Marshal(matched)
	var seal map[string]any
	json.Unmarshal(input.AuthenticatedSeal, &seal)
	seal["matcherFileSHA256"] = legacymedia.Hash(input.Matched)
	input.AuthenticatedSeal, _ = json.Marshal(seal)
	input.ExpectedSealSHA256 = legacymedia.Hash(input.AuthenticatedSeal)
	return input, raw
}
func TestMapContent172OriginalAuthorPlan(t *testing.T) {
	input, _ := map172Input(t)
	p, e := authorPlan(legacyTestRunID, input)
	if e != nil || p.Counts.Avatars != 1 || p.Counts.Moments < 1 {
		t.Fatal("original attribution plan", e, p.Counts)
	}
	again, e := authorPlan(legacyTestRunID, input)
	if e != nil || !reflect.DeepEqual(p, again) {
		t.Fatal("unstable original attribution")
	}
	input.Source[len(input.Source)/2] ^= 1
	if _, e = authorPlan(legacyTestRunID, input); e == nil {
		t.Fatal("unverified source accepted")
	}
}
func TestMapContent172NativeImmutableAttribution(t *testing.T) {
	f := legacyMigrationNativeFixture(t)
	ctx := f.Context
	input, raw := map172Input(t)
	p, e := la.Build(legacyTestRunID, input.Source, input.Manifest, input.Matched, input.AuthenticatedSeal, input.ExpectedSealSHA256)
	if e != nil {
		t.Fatal(e)
	}
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
	} {
		if e := apply(); e != nil {
			t.Fatal("synthetic original import", e)
		}
	}
	var expected la.Author
	for _, a := range p.Authors {
		if a.Avatar != nil {
			expected = a
		}
	}
	var owner string
	if e = f.Pool.QueryRow(ctx, `SELECT target_id::text FROM legacy_entity_mappings WHERE entity_kind='user'AND source_id=$1`, expected.SourceID).Scan(&owner); e != nil {
		t.Fatal(e)
	}
	if _, e = f.Pool.Exec(ctx, `UPDATE user_profiles SET display_name='Later private profile change' WHERE account_id=$1`, owner); e != nil {
		t.Fatal(e)
	}
	result, e := f.Importer.ApplyLegacyMomentAuthorRestore(ctx, legacyTestRunID, input, func(context.Context, rm.Asset) (io.ReadCloser, error) { return io.NopCloser(bytes.NewReader(raw)), nil })
	if e != nil || result.Counts != p.Counts {
		t.Fatal("attribution apply", e, result.Counts)
	}
	again, e := f.Importer.ApplyLegacyMomentAuthorRestore(ctx, legacyTestRunID, input, nil)
	if e != nil || !reflect.DeepEqual(again, result) {
		t.Fatal("idempotent attribution", e)
	}
	if _, e = f.Pool.Exec(ctx, `UPDATE legacy_moment_author_snapshots SET display_name='forged' WHERE owner_account_id=$1`, owner); e == nil {
		t.Fatal("snapshot mutable")
	}
	viewer, _ := legacyMigrationUUID()
	digest := sha256.Sum256([]byte("map172-independent-reader"))
	if _, e = f.Pool.Exec(ctx, `INSERT INTO accounts(id,account_type,status)VALUES($1,'person','active')`, viewer); e != nil {
		t.Fatal(e)
	}
	var session string
	if e = f.Pool.QueryRow(ctx, `INSERT INTO sessions(account_id,token_sha256,authentication_method,expires_at,idle_expires_at)VALUES($1,$2,'synthetic_test',clock_timestamp()+interval '1 hour',clock_timestamp()+interval '1 hour')RETURNING id`, viewer, digest[:]).Scan(&session); e != nil {
		t.Fatal(e)
	}
	store := New(f.Pool, true)
	actor := identity.Actor{ID: viewer, AccountType: "person"}
	var selected string
	for _, fact := range p.Facts {
		if fact.SourceUserID != expected.SourceID {
			continue
		}
		detail, e := store.ReadHumanMapContentMoment(ctx, digest, actor, fact.MomentID)
		if e != nil {
			t.Fatal("read original attribution", e)
		}
		if detail.Author == nil || detail.Author.DisplayName != expected.DisplayName || detail.Author.Avatar == nil || detail.Author.AccountID != owner || detail.PublishedAt != nil {
			t.Fatal("original author missing or current private profile exposed")
		}
		if fact.CreatedAt != nil {
			if detail.CreatedAtSource != "CIVU_ORIGINAL" || !detail.CreatedAt.Equal(*fact.CreatedAt) {
				t.Fatal("original time mismatch")
			}
		} else if detail.CreatedAtSource != "UNKNOWN" {
			t.Fatal("import time exposed")
		}
		av := detail.Author.Avatar
		u, _ := url.Parse(av.ThumbnailPath)
		binary, e := store.ReadHumanMapContentBinary(ctx, digest, actor, "moment-authors", fact.MomentID, expected.Avatar.SourceID, u.Query().Get("sourceVersion"), "")
		if e != nil || len(binary.Bytes) != av.ByteCount || legacymedia.Hash(binary.Bytes) != av.SHA256 || binary.MIMEType != "image/jpeg" {
			t.Fatal("original author thumbnail", e)
		}
		clear(binary.Bytes)
		selected = fact.MomentID
		if _, e = f.Pool.Exec(ctx, `INSERT INTO account_blocks(blocker_account_id,blocked_account_id)VALUES($1,$2)`, viewer, owner); e != nil {
			t.Fatal(e)
		}
		if e = binary.ResponseProof.RevalidateHumanControl(ctx); e == nil {
			t.Fatal("blocked avatar proof current")
		}
		if _, e = store.ReadHumanMapContentBinary(ctx, digest, actor, "moment-authors", fact.MomentID, expected.Avatar.SourceID, u.Query().Get("sourceVersion"), ""); e == nil {
			t.Fatal("blocked avatar readable")
		}
		if _, e = f.Pool.Exec(ctx, `DELETE FROM account_blocks WHERE blocker_account_id=$1 AND blocked_account_id=$2`, viewer, owner); e != nil {
			t.Fatal(e)
		}
		break
	}
	if selected == "" {
		t.Fatal("no original author detail")
	}
	var private bool
	if e = f.Pool.QueryRow(ctx, `SELECT visibility='private'AND display_name='Later private profile change'FROM user_profiles WHERE account_id=$1`, owner).Scan(&private); e != nil || !private {
		t.Fatal("native private profile changed")
	}
	fresh, e := store.ReadHumanMapContentMoment(ctx, digest, actor, selected)
	if e != nil {
		t.Fatal(e)
	}
	u, _ := url.Parse(fresh.Author.Avatar.ThumbnailPath)
	if _, e = f.Pool.Exec(ctx, `UPDATE moments SET status='withdrawn',revision=revision+1,updated_at=clock_timestamp() WHERE id=$1`, selected); e != nil {
		t.Fatal(e)
	}
	if e = fresh.ResponseProof.RevalidateHumanControl(ctx); e == nil {
		t.Fatal("withdrawn attribution proof current")
	}
	if _, e = store.ReadHumanMapContentBinary(ctx, digest, actor, "moment-authors", selected, expected.Avatar.SourceID, u.Query().Get("sourceVersion"), ""); e == nil {
		t.Fatal("withdrawn avatar readable")
	}
	if _, e = f.Pool.Exec(ctx, `UPDATE sessions SET revoked_at=clock_timestamp() WHERE id=$1`, session); e != nil {
		t.Fatal(e)
	}
	if _, e = store.ReadHumanMapContentMoment(ctx, digest, actor, selected); e == nil {
		t.Fatal("logout attribution readable")
	}
}
