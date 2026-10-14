package postgres

import (
	"context"
	"errors"
	"testing"
	"time"

	mp "github.com/birdtie/birdtie/apps/api/internal/mapprojection"
	pp "github.com/birdtie/birdtie/apps/api/internal/placephoto"
)

func mapMediaNativePlace(t *testing.T, r mp.Receipt, id string) mp.Item {
	t.Helper()
	for _, i := range r.View.Items {
		if i.Kind == "PLACE" && i.ID == id {
			return i
		}
	}
	t.Fatal("original synthetic place absent from narrow viewport", id)
	return mp.Item{}
}
func mapMediaNativeQuery() mp.Query {
	return mp.Query{CityID: "aberdeen-gb", West: -2.101, South: 57.149, East: -2.099, North: 57.151, Media: true}
}

func TestMapPlaceMediaNativeFullCountPhotoLeaseAndRetirement(t *testing.T) {
	f, id, reviewer := photoNativeSetup(t)
	_, published := photoNativePublish(t, f, reviewer, id)
	for n := 0; n < 37; n++ {
		f.exec(t, `WITH c AS(SELECT clock_timestamp()-interval '1 hour' AS at)INSERT INTO moments(id,author_account_id,city_id,place_id,title,body,location_precision,status,visibility,author_confirmed_at,published_at)SELECT $1,$2,'aberdeen-gb',$3,'合成公开计数成员','私人原图/原始正文不进入地图','place','published','public',at,at FROM c`, f.id(t), f.actor.ID, id)
	}
	a := mp.Access{Actor: f.actor, Digest: f.digest}
	q := mapMediaNativeQuery()
	r, e := f.store.ReadMapLayers(f.ctx, a, q)
	if e != nil {
		t.Fatal("actual media batch", e)
	}
	i := mapMediaNativePlace(t, r, id)
	if r.View.SchemaVersion != mp.MediaSchemaVersion || i.PlacePhoto == nil || i.RecentMoments == nil || i.RecentMoments.Count != 37 {
		t.Fatal("full current set / controlled photo", i, e)
	}
	moments := 0
	for _, x := range r.View.Items {
		if x.Kind == "MOMENT" {
			moments++
		}
	}
	if moments != 30 || !r.View.Truncated || i.RecentMoments.Count <= int64(moments) {
		t.Fatal("bounded map items used as count", moments, i.RecentMoments, r.View.Truncated)
	}
	final, e := f.store.FinalizeMapLayers(f.ctx, a, r)
	if e != nil {
		t.Fatal("unchanged exact lease", e)
	}
	j := mapMediaNativePlace(t, mp.Receipt{View: final}, id)
	if j.PlacePhoto == nil || j.PlacePhoto.ExpiresAt != i.PlacePhoto.ExpiresAt || j.RecentMoments == nil || *j.RecentMoments != *i.RecentMoments || final.ValidUntil != r.View.ValidUntil {
		t.Fatal("fresh capture extended old decoration lease", j)
	}
	// Withdraw only the independently approved derivative. All original
	// entities, true coordinates and complete count remain independently legal.
	photoNativeWrite(t, f, reviewer, id, pp.Input{OperationID: f.id(t), Action: "WITHDRAW", ExpectedVersion: published.AppliedVersion, CandidateID: published.CandidateID, CandidateVersion: published.CandidateVersion})
	final, e = f.store.FinalizeMapLayers(f.ctx, a, r)
	if e != nil {
		t.Fatal("photo withdrawal retired base", e)
	}
	j = mapMediaNativePlace(t, mp.Receipt{View: final}, id)
	if j.PlacePhoto != nil || j.ID != i.ID || j.Anchor != i.Anchor || j.SourceVersion != i.SourceVersion || j.RecentMoments == nil || j.RecentMoments.Count != 37 {
		t.Fatal("photo/base/count conflated", j)
	}
	// A payload stamped more than thirty days ago still maps as its canonical
	// public Moment; only the complete recent window changes independently.
	f.exec(t, `UPDATE moments SET author_confirmed_at=clock_timestamp()-interval '31 days',published_at=clock_timestamp()-interval '31 days' WHERE place_id=$1`, id)
	fresh, e := f.store.ReadMapLayers(f.ctx, a, q)
	if e != nil {
		t.Fatal(e)
	}
	j = mapMediaNativePlace(t, fresh, id)
	if j.RecentMoments == nil || j.RecentMoments.Count != 0 {
		t.Fatal("known zero/unknown conflated", j.RecentMoments)
	}
}

func TestMapPlaceMediaNativeWaitSourceAndOptionalReadFailure(t *testing.T) {
	f, id, reviewer := photoNativeSetup(t)
	photoNativePublish(t, f, reviewer, id)
	a := mp.Access{Actor: f.actor, Digest: f.digest}
	q := mapMediaNativeQuery()
	r, e := f.store.ReadMapLayers(f.ctx, a, q)
	if e != nil {
		t.Fatal(e)
	}
	// Same role values restored still have a new native photo source epoch.
	f.exec(t, `UPDATE city_editor_memberships SET state='revoked' WHERE city_id='aberdeen-gb' AND account_id=$1`, reviewer.AccountID)
	f.exec(t, `UPDATE city_editor_memberships SET state='active' WHERE city_id='aberdeen-gb' AND account_id=$1`, reviewer.AccountID)
	v, e := f.store.FinalizeMapLayers(f.ctx, a, r)
	if e != nil {
		t.Fatal("editor media ABA erased legal place", e)
	}
	if i := mapMediaNativePlace(t, mp.Receipt{View: v}, id); i.PlacePhoto != nil {
		t.Fatal("same-value role ABA borrowed fresh photo", i)
	}
	// An unavailable optional table cannot become an authoritative count zero.
	f.exec(t, `ALTER TABLE place_photo_publications RENAME TO isolated_map_photo_unavailable`)
	fresh, e := f.store.ReadMapLayers(f.ctx, a, q)
	if e != nil {
		t.Fatal("optional-source failure removed base", e)
	}
	i := mapMediaNativePlace(t, fresh, id)
	if i.PlacePhoto != nil || i.RecentMoments != nil {
		t.Fatal("unread metadata/count fabricated", i)
	}
	f.exec(t, `ALTER TABLE isolated_map_photo_unavailable RENAME TO place_photo_publications`)
	r, e = f.store.ReadMapLayers(f.ctx, a, q)
	if e != nil {
		t.Fatal(e)
	}
	lock, e := f.pool.Begin(f.ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer lock.Rollback(context.Background())
	var pid int
	if e = lock.QueryRow(f.ctx, `SELECT pg_backend_pid()`).Scan(&pid); e != nil {
		t.Fatal(e)
	}
	if _, e = lock.Exec(f.ctx, `UPDATE sessions SET revoked_at=clock_timestamp() WHERE id=$1`, f.session); e != nil {
		t.Fatal(e)
	}
	done := make(chan error, 1)
	go func() { _, err := f.store.FinalizeMapLayers(f.ctx, a, r); done <- err }()
	hcNativeWaitPID(t, f, pid)
	if _, e = lock.Exec(f.ctx, `UPDATE sessions SET revoked_at=NULL WHERE id=$1`, f.session); e != nil {
		t.Fatal(e)
	}
	if e = lock.Commit(f.ctx); e != nil {
		t.Fatal(e)
	}
	select {
	case e = <-done:
		if !errors.Is(e, mp.ErrChanged) {
			t.Fatal("wait accepted old Session authority", e)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("actual wait did not finish")
	}
}

func mapMediaNativeHoldUntil(t *testing.T, f *savedPageNativeFixture, a mp.Access, r mp.Receipt, until time.Time) mp.View {
	t.Helper()
	lock, e := f.pool.Begin(f.ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer lock.Rollback(context.Background())
	var pid int
	if e = lock.QueryRow(f.ctx, `SELECT pg_backend_pid()`).Scan(&pid); e != nil {
		t.Fatal(e)
	}
	if _, e = lock.Exec(f.ctx, `SELECT id FROM sessions WHERE id=$1 FOR UPDATE`, f.session); e != nil {
		t.Fatal(e)
	}
	type result struct {
		v mp.View
		e error
	}
	done := make(chan result, 1)
	go func() { v, e := f.store.FinalizeMapLayers(f.ctx, a, r); done <- result{v, e} }()
	hcNativeWaitPID(t, f, pid)
	deadline := time.Now().Add(8 * time.Second)
	for {
		var expired bool
		if e = f.pool.QueryRow(f.ctx, `SELECT clock_timestamp()>=$1`, until).Scan(&expired); e != nil {
			t.Fatal(e)
		}
		if expired {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("actual PG deadline never reached")
		}
		time.Sleep(15 * time.Millisecond)
	}
	if e = lock.Commit(f.ctx); e != nil {
		t.Fatal(e)
	}
	select {
	case got := <-done:
		if got.e != nil {
			t.Fatal("optional expiry removed legal base", got.e)
		}
		return got.v
	case <-time.After(5 * time.Second):
		t.Fatal("final native wait never completed")
	}
	return mp.View{}
}

func TestMapPlaceMediaNativeVisibleSetAndActualExpiryAfterWait(t *testing.T) {
	f, id, reviewer := photoNativeSetup(t)
	input := photoNativeInput(t, f)
	var retainedUntil time.Time
	if e := f.pool.QueryRow(f.ctx, `SELECT clock_timestamp()+interval '4 seconds'`).Scan(&retainedUntil); e != nil {
		t.Fatal(e)
	}
	input.Rights.RetentionUntil = retainedUntil
	_, candidate := photoNativeWrite(t, f, photoNativeAccess(f), id, input)
	photoNativeWrite(t, f, reviewer, id, pp.Input{OperationID: f.id(t), Action: "APPROVE", CandidateID: candidate.CandidateID, CandidateVersion: 1, RightsConfirmed: true, PhotoConfirmed: true})
	a := mp.Access{Actor: f.actor, Digest: f.digest}
	q := mapMediaNativeQuery()
	r, e := f.store.ReadMapLayers(f.ctx, a, q)
	if e != nil {
		t.Fatal(e)
	}
	i := mapMediaNativePlace(t, r, id)
	if i.PlacePhoto == nil {
		t.Fatal("controlled short lease absent")
	}
	v := mapMediaNativeHoldUntil(t, f, a, r, i.PlacePhoto.ExpiresAt)
	j := mapMediaNativePlace(t, mp.Receipt{View: v}, id)
	if j.PlacePhoto != nil || j.Anchor != i.Anchor || j.RecentMoments == nil || j.RecentMoments.Count != 0 || v.ValidUntil != r.View.ValidUntil {
		t.Fatal("photo expiry conflated with point/count", j)
	}
	// The original public Moment itself remains readable; only its membership
	// in the thirty-day summary expires during a real final Session wait.
	f.exec(t, `WITH c AS(SELECT clock_timestamp()-interval '30 days'+interval '4 seconds' AS at)INSERT INTO moments(id,author_account_id,city_id,place_id,title,body,location_precision,status,visibility,author_confirmed_at,published_at)SELECT $1,$2,'aberdeen-gb',$3,'合成窗口边界','不导出正文','place','published','public',at,at FROM c`, f.id(t), f.actor.ID, id)
	r, e = f.store.ReadMapLayers(f.ctx, a, q)
	if e != nil {
		t.Fatal(e)
	}
	i = mapMediaNativePlace(t, r, id)
	if i.RecentMoments == nil || i.RecentMoments.Count != 1 {
		t.Fatal("actual30d member absent", i)
	}
	v = mapMediaNativeHoldUntil(t, f, a, r, i.RecentMoments.ValidUntil)
	j = mapMediaNativePlace(t, mp.Receipt{View: v}, id)
	if j.RecentMoments != nil || j.Anchor != i.Anchor || j.SourceVersion != i.SourceVersion {
		t.Fatal("expired summary guessed fresh zero/removed point", j)
	}
	peer, _ := orgHumanNativePerson(t, f)
	f.exec(t, `INSERT INTO user_profiles(account_id,display_name,visibility)VALUES($1,'合成private profile仍可明确公开动态','private')`, peer.AccountID)
	f.exec(t, `WITH c AS(SELECT clock_timestamp()-interval '1 hour' AS at)INSERT INTO moments(id,author_account_id,city_id,place_id,title,body,location_precision,status,visibility,author_confirmed_at,published_at)SELECT $1,$2,'aberdeen-gb',$3,'合成当前peer公开动态','不导出正文','place','published','public',at,at FROM c`, f.id(t), peer.AccountID, id)
	r, e = f.store.ReadMapLayers(f.ctx, a, q)
	if e != nil {
		t.Fatal(e)
	}
	if n := mapMediaNativePlace(t, r, id).RecentMoments; n == nil || n.Count != 1 {
		t.Fatal("author profile visibility became content permission", n)
	}
	f.exec(t, `INSERT INTO account_blocks(blocker_account_id,blocked_account_id)VALUES($1,$2)`, f.actor.ID, peer.AccountID)
	r, e = f.store.ReadMapLayers(f.ctx, a, q)
	if e != nil {
		t.Fatal(e)
	}
	if n := mapMediaNativePlace(t, r, id).RecentMoments; n == nil || n.Count != 0 {
		t.Fatal("viewer current block did not filter complete set", n)
	}
	r, e = f.store.ReadMapLayers(f.ctx, mp.Access{}, q)
	if e != nil {
		t.Fatal(e)
	}
	if n := mapMediaNativePlace(t, r, id).RecentMoments; n == nil || n.Count != 1 {
		t.Fatal("anonymous borrowed signed viewer namespace", n)
	}
}
