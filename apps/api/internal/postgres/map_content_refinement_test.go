package postgres

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"github.com/birdtie/birdtie/apps/api/internal/content"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	av "github.com/birdtie/birdtie/apps/api/internal/profileavatar"
	"image"
	"image/jpeg"
	"strings"
	"testing"
	"time"
)

func TestMapContentRefinementNativeViewportPagingAndAuthor(t *testing.T) {
	f := legacyMigrationNativeFixture(t)
	ctx := f.Context
	owner, _ := legacyMigrationUUID()
	digest := sha256.Sum256([]byte("synthetic-map-refinement"))
	if _, e := f.Pool.Exec(ctx, `INSERT INTO accounts(id,account_type,status)VALUES($1,'person','active')`, owner); e != nil {
		t.Fatal(e)
	}
	if _, e := f.Pool.Exec(ctx, `INSERT INTO user_profiles(account_id,display_name,visibility)VALUES($1,'Synthetic author','private')`, owner); e != nil {
		t.Fatal(e)
	}
	if _, e := f.Pool.Exec(ctx, `WITH cl AS(SELECT clock_timestamp() at) INSERT INTO sessions(account_id,token_sha256,authentication_method,expires_at,idle_expires_at)SELECT $1,$2,'synthetic_test',at+interval '1 hour',at+interval '1 hour' FROM cl`, owner, digest[:]); e != nil {
		t.Fatal(e)
	}
	if _, e := f.Pool.Exec(ctx, `INSERT INTO places(id,name,category_code,latitude,longitude,coordinate_system,location_precision,publication_status,source_label,source_ref,maintainer_label)
 SELECT md5('refine-place:'||i)::uuid,'Synthetic real point','park',10+i*0.0001,20+i*0.0001,'wgs84','point','draft','LOCAL_SYNTHETIC_FIXTURE','disposable://map-refinement','synthetic' FROM generate_series(1,81)i`); e != nil {
		t.Fatal(e)
	}
	if _, e := f.Pool.Exec(ctx, `INSERT INTO moments(id,author_account_id,title,body,status,visibility,location_precision,created_at)
 SELECT md5('refine-moment:'||i)::uuid,$1,'','Synthetic body','draft','private','none','2020-01-02T03:04:05Z'::timestamptz FROM generate_series(1,81)i`, owner); e != nil {
		t.Fatal(e)
	}
	if _, e := f.Pool.Exec(ctx, `INSERT INTO moment_location_links(id,moment_id,kind,place_id,precision,position)
 SELECT md5('refine-link:'||i)::uuid,md5('refine-moment:'||i)::uuid,'PLACE',md5('refine-place:'||i)::uuid,'place',0 FROM generate_series(1,81)i`); e != nil {
		t.Fatal(e)
	}
	s := New(f.Pool, true)
	a := identity.Actor{ID: owner, AccountType: "person"}
	q := content.MapContentQuery{Bounds: content.MapContentBounds{West: 19, South: 9, East: 21, North: 11}, Zoom: 0}
	page, e := s.ReadHumanMapContent(ctx, digest, a, q)
	if e != nil || len(page.Pins) != 40 || page.TotalPointCount != 81 || page.TotalContentCount != 81 || page.NextCursor == nil || !strings.Contains(page.ViewportPath, "sourceVersion=") {
		t.Fatal("first real-point page", e, len(page.Pins), page.TotalPointCount)
	}
	seen := map[string]bool{}
	var previousCursor string
	for number := 0; ; number++ {
		for _, pin := range page.Pins {
			if pin.Kind != "PLACE_CONTENT" || seen[pin.MarkerID] {
				t.Fatal("different coordinates grouped or repeated")
			}
			seen[pin.MarkerID] = true
		}
		if page.NextCursor == nil {
			break
		}
		previousCursor = *page.NextCursor
		q.SourceVersion, q.Cursor = page.SourceVersion, *page.NextCursor
		page, e = s.ReadHumanMapContent(ctx, digest, a, q)
		if e != nil || len(page.Pins) > 40 || page.ValidUntil.Sub(page.ObservedAt) < 25*time.Second {
			t.Fatal("fresh bounded continuation", e)
		}
		if number > 3 {
			t.Fatal("nonterminating pagination")
		}
	}
	if len(seen) != 81 {
		t.Fatal("dropped real point", len(seen))
	}
	var id string
	if e = f.Pool.QueryRow(ctx, `SELECT md5('refine-moment:1')::uuid::text`).Scan(&id); e != nil {
		t.Fatal(e)
	}
	detail, e := s.ReadHumanMapContentMoment(ctx, digest, a, id)
	if e != nil || detail.Author == nil || detail.Author.DisplayName != "Synthetic author" || detail.Author.AccountID != owner || detail.Author.Avatar != nil || detail.CreatedAtSource != "NATIVE" || detail.PublishedAt != nil || detail.CreatedAt.Format(time.RFC3339) != "2020-01-02T03:04:05Z" {
		t.Fatal("current author and original created time", e, fmt.Sprint(detail.Author), detail.CreatedAtSource)
	}
	// Existing avatar endpoint remains an independent current-authority read.
	var imageBytes bytes.Buffer
	if e = jpeg.Encode(&imageBytes, image.NewRGBA(image.Rect(0, 0, 128, 128)), nil); e != nil {
		t.Fatal(e)
	}
	thumbnail, e := av.Prepare(imageBytes.Bytes(), "image/jpeg")
	if e != nil {
		t.Fatal(e)
	}
	if _, e = f.Pool.Exec(ctx, `INSERT INTO person_avatars(account_id,version,thumbnail,sha256)VALUES($1,1,$2,$3)`, owner, thumbnail.Bytes, thumbnail.SHA256); e != nil {
		t.Fatal(e)
	}
	withAvatar, e := s.ReadHumanMapContentMoment(ctx, digest, a, id)
	if e != nil || withAvatar.Author.Avatar == nil || withAvatar.Author.Avatar.AccountID != owner {
		t.Fatal("author avatar metadata", e)
	}
	access := av.Access{AccountID: owner, AccountType: "person", SessionDigest: digest}
	metadata, raw, e := s.ReadAvatarContent(ctx, access, owner, 1)
	if e != nil || metadata.SHA256 != thumbnail.SHA256 || !bytes.Equal(raw, thumbnail.Bytes) {
		t.Fatal("controlled real thumbnail", e)
	}
	if _, e = f.Pool.Exec(ctx, `UPDATE person_avatars SET version=2,thumbnail=NULL,sha256=NULL WHERE account_id=$1`, owner); e != nil {
		t.Fatal(e)
	}
	if _, _, e = s.ReadAvatarContent(ctx, access, owner, 1); e == nil {
		t.Fatal("retired avatar remained readable")
	}
	if e = withAvatar.ResponseProof.RevalidateHumanControl(ctx); e == nil {
		t.Fatal("old author avatar proof survived removal")
	}
	if _, e = f.Pool.Exec(ctx, `UPDATE user_profiles SET display_name='Synthetic author updated' WHERE account_id=$1`, owner); e != nil {
		t.Fatal(e)
	}
	if e = detail.ResponseProof.RevalidateHumanControl(ctx); e == nil {
		t.Fatal("stale author proof survived rename")
	}
	if _, e = f.Pool.Exec(ctx, `UPDATE moments SET body=body||' changed',revision=revision+1 WHERE id=$1`, id); e != nil {
		t.Fatal(e)
	}
	q.Cursor = previousCursor
	if _, e = s.ReadHumanMapContent(ctx, digest, a, q); e == nil {
		t.Fatal("changed viewport resumed")
	}
	if _, e = f.Pool.Exec(ctx, `UPDATE sessions SET revoked_at=clock_timestamp() WHERE token_sha256=$1`, digest[:]); e != nil {
		t.Fatal(e)
	}
	if _, e = s.ReadHumanMapContentMoment(ctx, digest, a, id); e == nil {
		t.Fatal("logged-out reader retained author")
	}
}
