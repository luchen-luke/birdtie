package postgres

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/content"
	"github.com/birdtie/birdtie/apps/api/internal/httpapi"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	"github.com/birdtie/birdtie/apps/api/internal/legacymedia"
	rm "github.com/birdtie/birdtie/apps/api/internal/legacyreferencemedia"
)

func TestReferenceTargetsNativeOwnerPaginationAndIndependentImagePurpose(t *testing.T) {
	f := legacyMigrationNativeFixture(t)
	in, raw := referenceMediaFixtureInput(t)
	decode := func(raw []byte) map[string]any {
		d := json.NewDecoder(bytes.NewReader(raw))
		d.UseNumber()
		var value map[string]any
		if d.Decode(&value) != nil {
			t.Fatal("synthetic envelope")
		}
		return value
	}
	source := decode(in.Source)
	tables := source["tables"].(map[string]any)
	clone := func(original map[string]any) map[string]any {
		copy := map[string]any{}
		for key, value := range original {
			copy[key] = value
		}
		return copy
	}
	place := tables["places"].([]any)[0].(map[string]any)
	image := tables["place_images"].([]any)[0].(map[string]any)
	for i := 0; i < 20; i++ {
		p := clone(place)
		p["id"] = fmt.Sprintf("16800000-0000-4000-8000-%012d", i+1)
		p["name"] = fmt.Sprintf("Synthetic original contribution %02d", i)
		tables["places"] = append(tables["places"].([]any), p)
		r := clone(image)
		r["id"] = fmt.Sprintf("16810000-0000-4000-8000-%012d", i+1)
		r["place_id"], r["is_cover"], r["sort_order"], r["source_note_id"] = p["id"], false, json.Number("0"), nil
		tables["place_images"] = append(tables["place_images"].([]any), r)
	}
	in.Source, in.Manifest = legacySyntheticInput(t, func(target map[string]any) {
		for key, value := range tables {
			target[key] = value
		}
	})
	matched := decode(in.Matched)
	matched["source"], matched["sourceFileSha256"] = decode(in.Source)["source"], legacymedia.Hash(in.Source)
	in.Matched, _ = json.Marshal(matched)
	seal := decode(in.AuthenticatedSeal)
	seal["matcherFileSHA256"] = legacymedia.Hash(in.Matched)
	in.AuthenticatedSeal, _ = json.Marshal(seal)
	in.ExpectedSealSHA256 = legacymedia.Hash(in.AuthenticatedSeal)
	if _, e := f.Importer.Apply(f.Context, legacyTestRunID, in.Source, in.Manifest); e != nil {
		t.Fatal(e)
	}
	if _, e := f.Importer.ApplyPersonalArchives(f.Context, legacyTestRunID, in.Source, in.Manifest); e != nil {
		t.Fatal(e)
	}
	if _, e := f.Importer.ApplyNativeAdaptation(f.Context, legacyTestRunID, in.Source, in.Manifest); e != nil {
		t.Fatal(e)
	}
	if _, e := f.Importer.ApplyJourneyDetails(f.Context, legacyTestRunID, in.Source, in.Manifest); e != nil {
		t.Fatal(e)
	}
	if _, e := f.Importer.ApplyLegacyPrivateMedia(f.Context, legacyTestRunID, in, func(context.Context, legacymedia.Record) (io.ReadCloser, error) {
		return io.NopCloser(bytes.NewReader(raw)), nil
	}); e != nil {
		t.Fatal(e)
	}
	if _, e := f.Importer.ApplyLegacyReferenceMedia(f.Context, legacyTestRunID, in, func(context.Context, rm.Asset) (io.ReadCloser, error) {
		return io.NopCloser(bytes.NewReader(raw)), nil
	}); e != nil {
		t.Fatal(e)
	}
	var owner string
	if f.Pool.QueryRow(f.Context, `SELECT owner_account_id::text FROM legacy_private_media_import_items LIMIT 1`).Scan(&owner) != nil {
		t.Fatal("synthetic owner")
	}
	if _, e := f.Pool.Exec(f.Context, `UPDATE accounts SET status='active' WHERE id=$1`, owner); e != nil {
		t.Fatal(e)
	}
	token, digest, e := identity.NewToken()
	if e != nil {
		t.Fatal(e)
	}
	if _, e = f.Pool.Exec(f.Context, `INSERT INTO sessions(account_id,token_sha256,authentication_method,expires_at,idle_expires_at)VALUES($1,$2,'synthetic_test',clock_timestamp()+interval '2 hours',clock_timestamp()+interval '1 hour')`, owner, digest[:]); e != nil {
		t.Fatal(e)
	}
	store := New(f.Pool, true)
	api := httpapi.New(store, store, nil, store, nil, nil, nil, nil, store, nil, nil, nil, false, nil, nil, nil)
	call := func(method, path string, body any, bearer string) *httptest.ResponseRecorder {
		var input io.Reader
		if body != nil {
			encoded, _ := json.Marshal(body)
			input = bytes.NewReader(encoded)
		}
		r := httptest.NewRequest(method, path, input)
		if bearer != "" {
			r.Header.Set("Authorization", "Bearer "+bearer)
		}
		if body != nil {
			r.Header.Set("Content-Type", "application/json")
		}
		w := httptest.NewRecorder()
		api.ServeHTTP(w, r)
		return w
	}
	path := "/v1/me/reference-image-targets?targetType=PLACE"
	if call("GET", path, nil, "").Code != 401 {
		t.Fatal("anonymous target discovery")
	}
	readPage := func(path string) content.ReferenceImageTargetsPage {
		w := call("GET", path, nil, token)
		var value struct {
			Data content.ReferenceImageTargetsPage `json:"data"`
		}
		if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &value) != nil || !value.Data.Valid(time.Now().UTC()) {
			t.Fatal("current registered target page", w.Code)
		}
		return value.Data
	}
	first := readPage(path)
	if len(first.Items) != 20 || first.NextCursor == nil {
		t.Fatal("first page")
	}
	second := readPage(path + "&cursor=" + url.QueryEscape(*first.NextCursor))
	if len(second.Items) != 1 || second.NextCursor != nil || second.SourceVersion != first.SourceVersion || second.ValidUntil.After(first.ValidUntil) {
		t.Fatal("bounded original continuation")
	}
	var selected content.ReferenceImageTarget
	for _, item := range first.Items {
		if item.ImageCount == 1 {
			selected = item
			break
		}
	}
	images := call("GET", "/v1/me/reference-images?targetType=PLACE&targetId="+selected.TargetID, nil, token)
	var imagePage struct {
		Data content.ReferenceImagePage `json:"data"`
	}
	if images.Code != 200 || json.Unmarshal(images.Body.Bytes(), &imagePage) != nil || len(imagePage.Data.Items) != 1 || imagePage.Data.Items[0].CanCopy || imagePage.Data.Items[0].CanPublish {
		t.Fatal("selected private purpose")
	}
	item := imagePage.Data.Items[0]
	operation, _ := legacyMigrationUUID()
	if call("DELETE", "/v1/me/reference-images/"+item.ID, content.ReferenceImageDelete{OperationID: operation, ExpectedRevision: item.Revision, ExpectedSourceVersion: item.SourceVersion}, token).Code != 200 {
		t.Fatal("original purpose delete")
	}
	if call("GET", path+"&cursor="+url.QueryEscape(*first.NextCursor), nil, token).Code != 409 {
		t.Fatal("old source cursor survived original purpose change")
	}
	if len(readPage(path).Items) != 20 || call("GET", "/v1/me/reference-image-operations/"+operation, nil, token).Code != 200 {
		t.Fatal("fresh targets or original operation lost")
	}
	otherToken, otherDigest, _ := identity.NewToken()
	var other string
	if f.Pool.QueryRow(f.Context, `INSERT INTO accounts(id,account_type)VALUES(gen_random_uuid(),'person')RETURNING id::text`).Scan(&other) != nil {
		t.Fatal("isolated current reader")
	}
	if _, e = f.Pool.Exec(f.Context, `INSERT INTO sessions(account_id,token_sha256,authentication_method,expires_at,idle_expires_at)VALUES($1,$2,'synthetic_test',clock_timestamp()+interval '2 hours',clock_timestamp()+interval '1 hour')`, other, otherDigest[:]); e != nil {
		t.Fatal(e)
	}
	w := call("GET", path, nil, otherToken)
	var otherPage struct {
		Data content.ReferenceImageTargetsPage `json:"data"`
	}
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &otherPage) != nil || len(otherPage.Data.Items) != 0 {
		t.Fatal("foreign owner discovery")
	}
	if e = store.RevokeSession(f.Context, digest); e != nil || call("GET", path, nil, token).Code != 401 {
		t.Fatal("retired session discovery")
	}
}
