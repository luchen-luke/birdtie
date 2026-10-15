package postgres

import (
	"encoding/json"
	"errors"
	"github.com/birdtie/birdtie/apps/api/internal/content"
	"github.com/birdtie/birdtie/apps/api/internal/httpapi"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	"net/http/httptest"
	"testing"
)

func TestMomentHistoryNativeBeyondHundredAndCurrentPublicRelations(t *testing.T) {
	f := savedPageNative(t)
	token, digest, e := identity.NewToken()
	if e != nil {
		t.Fatal(e)
	}
	f.exec(t, `UPDATE sessions SET token_sha256=$2 WHERE id=$1`, f.session, digest[:])
	f.digest = digest
	f.exec(t, `INSERT INTO moments(author_account_id,city_id,title,body,time_precision,location_precision,created_at,updated_at)
 SELECT $1,'aberdeen-gb','合成分页草稿'||g,'本人私人历史','unknown','city',clock_timestamp()-g*interval '1 second',clock_timestamp()-g*interval '1 second' FROM generate_series(1,105) g`, f.actor.ID)
	q := content.MomentHistoryQuery{Status: "active"}
	first, e := f.store.ReadOwnMomentHistory(f.ctx, f.digest, f.actor, q)
	if e != nil || len(first.Items) != 20 || first.NextCursor == nil {
		t.Fatal("first owner history", e)
	}
	router := httpapi.New(f.store, f.store, nil, f.store, nil, nil, nil, nil, nil, nil, nil, nil, false, nil, nil, nil)
	req := httptest.NewRequest("GET", "/v1/me/moments/history", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	var wire struct{ Data content.OwnMomentHistoryPage }
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &wire) != nil || len(wire.Data.Items) != 20 {
		t.Fatal("actual registered native owner page", w.Code, w.Body.String())
	}
	if e = f.store.RevalidateOwnMomentHistory(f.ctx, f.digest, f.actor, q, first); e != nil {
		t.Fatal("current encoded owner proof", e)
	}
	seen := map[string]bool{}
	cursor := ""
	for pages := 0; pages < 6; pages++ {
		q.Cursor = cursor
		p, e := f.store.ReadOwnMomentHistory(f.ctx, f.digest, f.actor, q)
		if e != nil {
			t.Fatal("owner keyset page", e)
		}
		for _, m := range p.Items {
			if seen[m.ID] {
				t.Fatal("duplicate owner row")
			}
			seen[m.ID] = true
		}
		if p.NextCursor == nil {
			break
		}
		cursor = *p.NextCursor
	}
	if len(seen) != 105 {
		t.Fatal("owner truncated beyond hundred", len(seen))
	}
	q.Cursor = first.RefreshCursor
	f.exec(t, `UPDATE sessions SET idle_expires_at=clock_timestamp()+interval '35 minutes' WHERE id=$1`, f.session)
	if p, e := f.store.ReadOwnMomentHistory(f.ctx, f.digest, f.actor, q); e != nil || len(p.Items) != 20 || p.SnapshotAt != first.SnapshotAt {
		t.Fatal("idle renewal or first-page refresh changed source", e)
	}
	f.exec(t, `UPDATE moments SET body='当前修改的私人正文' WHERE id=$1`, first.Items[0].ID)
	if _, e = f.store.ReadOwnMomentHistory(f.ctx, f.digest, f.actor, q); !errors.Is(e, content.ErrMomentHistoryChanged) {
		t.Fatal("changed owner cursor accepted", e)
	}
	author := f.id(t)
	f.exec(t, `INSERT INTO accounts(id,account_type,status) VALUES($1,'person','active')`, author)
	f.exec(t, `INSERT INTO user_profiles(account_id,display_name,visibility) VALUES($1,'合成公开作者','public')`, author)
	agent := f.id(t)
	f.exec(t, `INSERT INTO agents(id,agent_type,principal_account_id,status) VALUES($1,'personal',$2,'active')`, agent, author)
	f.exec(t, `INSERT INTO moments(author_account_id,city_id,title,body,time_precision,location_precision) VALUES($1,'aberdeen-gb','合成未公开草稿','NEVER_PUBLIC_HISTORY_PRIVATE_BODY','unknown','city')`, author)
	f.exec(t, `INSERT INTO moments(author_account_id,city_id,title,body,time_precision,location_precision,status,visibility,revision,author_confirmed_at,published_at)
 SELECT $1,'aberdeen-gb','合成城市公开历史'||g,'PUBLIC_SYNTHETIC_TEXT','unknown','city','published','public',2,clock_timestamp()-interval '40 days',clock_timestamp()-interval '31 days'-g*interval '1 second' FROM generate_series(1,25) g`, author)
	access := content.PublicMomentAccess{Actor: f.actor, SessionDigest: f.digest}
	pub := content.MomentHistoryQuery{Scope: "author", AuthorID: author}
	p, e := f.store.ReadPublicMomentHistory(f.ctx, access, pub)
	if e != nil || len(p.Items) != 20 || p.NextCursor == nil || p.Items[0].LocationPrecision != "city" || p.Items[0].PlaceID != "" {
		t.Fatal("unbounded-by-map public author history", e)
	}
	publicRequest := httptest.NewRequest("GET", "/v1/moments/history?scope=city&cityId=aberdeen-gb", nil)
	publicWire := httptest.NewRecorder()
	router.ServeHTTP(publicWire, publicRequest)
	var publicData struct {
		Data content.PublicMomentHistoryPage
	}
	if publicWire.Code != 200 || json.Unmarshal(publicWire.Body.Bytes(), &publicData) != nil || len(publicData.Data.Items) != 20 {
		t.Fatal("actual registered native city PUBLIC page", publicWire.Code, publicWire.Body.String())
	}
	if e = f.store.RevalidatePublicMomentHistory(f.ctx, access, pub, p); e != nil {
		t.Fatal("public final proof", e)
	}
	pub.Cursor = *p.NextCursor
	last, e := f.store.ReadPublicMomentHistory(f.ctx, access, pub)
	if e != nil || len(last.Items) != 5 || last.NextCursor != nil {
		t.Fatal("author final page", e)
	}
	filtered := content.MomentHistoryQuery{Scope: "following", CityID: "aberdeen-gb"}
	empty, e := f.store.ReadPublicMomentHistory(f.ctx, access, filtered)
	if e != nil || len(empty.Items) != 0 {
		t.Fatal("no following is honest empty", e)
	}
	f.exec(t, `INSERT INTO follows(follower_account_id,person_account_id) VALUES($1,$2)`, f.actor.ID, author)
	following, e := f.store.ReadPublicMomentHistory(f.ctx, access, filtered)
	if e != nil || len(following.Items) != 20 {
		t.Fatal("PUBLIC relation filter", e)
	}
	f.exec(t, `DELETE FROM follows WHERE follower_account_id=$1 AND person_account_id=$2`, f.actor.ID, author)
	filtered.Cursor = following.RefreshCursor
	if _, e = f.store.ReadPublicMomentHistory(f.ctx, access, filtered); !errors.Is(e, content.ErrMomentHistoryChanged) {
		t.Fatal("removed relation kept cached members", e)
	}
	request := f.id(t)
	f.exec(t, `INSERT INTO connection_requests(id,sender_account_id,recipient_account_id,city_id,note,scope,state,expires_at) VALUES($1,$2,$3,NULL,'合成本人同意好友','friend','accepted',clock_timestamp()+interval '1 hour')`, request, f.actor.ID, author)
	f.exec(t, `INSERT INTO person_ties(person_a_account_id,person_b_account_id,request_id) VALUES(LEAST($1::uuid,$2::uuid),GREATEST($1::uuid,$2::uuid),$3)`, f.actor.ID, author, request)
	friends, e := f.store.ReadPublicMomentHistory(f.ctx, access, content.MomentHistoryQuery{Scope: "friends", CityID: "aberdeen-gb"})
	if e != nil || len(friends.Items) != 20 {
		t.Fatal("accepted friendship public filter", e)
	}
	pub.Cursor = p.RefreshCursor
	f.exec(t, `INSERT INTO account_blocks(blocker_account_id,blocked_account_id) VALUES($1,$2)`, author, f.actor.ID)
	if e = f.store.RevalidatePublicMomentHistory(f.ctx, access, content.MomentHistoryQuery{Scope: "author", AuthorID: author}, p); e == nil {
		t.Fatal("block released old public payload")
	}
	if _, e = f.store.ReadPublicMomentHistory(f.ctx, access, pub); e == nil {
		t.Fatal("block kept old cursor")
	}
	f.exec(t, `UPDATE sessions SET revoked_at=clock_timestamp() WHERE id=$1`, f.session)
	if _, e = f.store.ReadOwnMomentHistory(f.ctx, f.digest, f.actor, content.MomentHistoryQuery{Status: "active"}); !errors.Is(e, identity.ErrUnauthorized) {
		t.Fatal("revoked session read owner history", e)
	}
}
