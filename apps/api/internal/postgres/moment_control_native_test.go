package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/actorref"
	"github.com/birdtie/birdtie/apps/api/internal/content"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	"github.com/jackc/pgx/v5"
)

func mc144Input(t *testing.T, f *savedPageNativeFixture) content.MomentControlInput {
	t.Helper()
	return content.MomentControlInput{OperationID: f.id(t), Action: "CREATE", Input: &content.MomentInput{CityID: "aberdeen-gb", Title: "仅本人草稿144", Body: "PRIVATE_MOMENT_144_DO_NOT_COPY", TimePrecision: "unknown", LocationPrecision: "city"}}
}
func mc144Preview(t *testing.T, f *savedPageNativeFixture, in content.MomentControlInput) (content.MomentControlInput, content.MomentControlPreview) {
	t.Helper()
	p, e := f.store.PreviewMomentControl(f.ctx, f.digest, f.actor, in)
	if e != nil || p.OperationID != in.OperationID || p.Action != in.Action || p.PayloadDigest != content.MomentControlDigest(in) || p.InputSHA256 != content.MomentControlInputHash(in.Input) || !content.MomentControlHex(p.SourceVersion) || p.ResponseProof == nil {
		t.Fatal("actual closed Moment preview", e)
	}
	if e = p.ResponseProof.RevalidateHumanControl(f.ctx); e != nil {
		t.Fatal("actual Moment preview tail proof", e)
	}
	in.SourceVersion = p.SourceVersion
	in.Approval = p.Approval
	return in, p
}
func mc144Write(t *testing.T, f *savedPageNativeFixture, in content.MomentControlInput) (content.MomentControlReceipt, content.MomentControlInput) {
	t.Helper()
	in, _ = mc144Preview(t, f, in)
	r, e := f.store.CommitMomentControl(f.ctx, f.digest, f.actor, in)
	if e != nil || !content.ValidMomentControlReceipt(r, in.OperationID) || r.PayloadDigest != content.MomentControlDigest(in) {
		t.Fatal("actual canonical Moment commit", e)
	}
	return r, in
}

func TestMomentContextLocationAuthoritySeparation(t *testing.T) {
	activity := "b1700000-0000-4000-8000-000000000017"
	in := content.MomentInput{CityID: "aberdeen-gb", ActivityID: &activity}
	for _, tc := range []struct {
		name, city string
		available  bool
		want       string
		wantErr    bool
	}{
		{"current_place_same_city", "aberdeen-gb", true, "aberdeen-gb", false},
		{"current_place_other_city", "another-city", true, "", true},
		{"unavailable_selector", "aberdeen-gb", false, "", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			captured := momentLocationsSnapshot{Resolved: []content.MomentLocationResolved{{Kind: "PLACE", CityID: tc.city, Available: tc.available}}}
			got, err := momentContextCity(in, captured)
			if got != tc.want || (err != nil) != tc.wantErr {
				t.Fatal("association authority did not use current exact City", got, err)
			}
		})
	}
	// An omitted or explicitly removed context does not acquire a fresh
	// external read simply because the person retains a private location ID.
	in.ActivityID = nil
	if city, err := momentContextCity(in, momentLocationsSnapshot{}); err != nil || city != "" {
		t.Fatal("omitted private context acquired new authority", err)
	}
	clear := ""
	in.ActivityID = &clear
	if city, err := momentContextCity(in, momentLocationsSnapshot{}); err != nil || city != "" {
		t.Fatal("explicit cleanup needed new external authority", err)
	}
	link := content.MomentLocationInput{ID: "b1700000-0000-4000-8000-000000000001", Kind: "PLACE", PlaceID: "b1700000-0000-4000-8000-000000000015", Precision: "place"}
	old := []momentLocationRow{{input: link, position: 0, status: "ACTIVE"}}
	if !momentContextLocationsUnchanged(old, []content.MomentLocationInput{link}) {
		t.Fatal("exact private location did not retain original context")
	}
	changed := link
	changed.Precision = "city"
	if momentContextLocationsUnchanged(old, []content.MomentLocationInput{changed}) || momentContextLocationsUnchanged(old, nil) {
		t.Fatal("changed or removed location bypassed no-geography protection")
	}
}

type mc144Counters struct{ moments, audits, outbox, history, links int }

func mc144Count(t *testing.T, f *savedPageNativeFixture) mc144Counters {
	t.Helper()
	var c mc144Counters
	e := f.pool.QueryRow(f.ctx, `SELECT
 (SELECT count(*) FROM moments WHERE author_account_id=$1),
 (SELECT count(*) FROM audit_events WHERE actor_account_id=$1 AND resource_type='moment' AND purpose='personal_content'),
 (SELECT count(*) FROM agent_domain_outbox WHERE subject_id=$1),
 (SELECT count(*) FROM human_moment_control_operations WHERE actor_account_id=$1),
 (SELECT count(*) FROM moment_activity_links l JOIN moments m ON m.id=l.moment_id WHERE m.author_account_id=$1)+(SELECT count(*) FROM moment_community_links l JOIN moments m ON m.id=l.moment_id WHERE m.author_account_id=$1)+(SELECT count(*) FROM moment_organization_links l JOIN moments m ON m.id=l.moment_id WHERE m.author_account_id=$1)`, f.actor.ID).Scan(&c.moments, &c.audits, &c.outbox, &c.history, &c.links)
	if e != nil {
		t.Fatal(e)
	}
	return c
}
func mc144NoEffect(t *testing.T, f *savedPageNativeFixture, before mc144Counters) {
	t.Helper()
	if got := mc144Count(t, f); got != before {
		t.Fatal("retired Moment operation changed native effects", before, got)
	}
}
func mc144Bound(t *testing.T, f *savedPageNativeFixture) context.Context {
	t.Helper()
	frame, e := f.store.CaptureHumanControlSession(f.ctx, f.digest, f.actor)
	if e != nil {
		t.Fatal(e)
	}
	return identity.WithHumanControlFrame(f.ctx, f.digest, f.actor, frame)
}
func mc144Outer(t *testing.T, f *savedPageNativeFixture, query string, args ...any) (pgx.Tx, int) {
	t.Helper()
	tx, e := f.pool.Begin(f.ctx)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { _ = tx.Rollback(context.Background()) })
	var pid int
	if e = tx.QueryRow(f.ctx, `SELECT pg_backend_pid()`).Scan(&pid); e != nil {
		t.Fatal(e)
	}
	if _, e = tx.Exec(f.ctx, query, args...); e != nil {
		t.Fatal(e)
	}
	return tx, pid
}
func mc144Result(t *testing.T, done <-chan error, want error) {
	t.Helper()
	select {
	case e := <-done:
		if want == nil && e != nil || want != nil && !errors.Is(e, want) {
			t.Fatal("original Moment frame/source result", e)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("actual Moment writer did not resume")
	}
}

func TestMomentControlNativeCanonicalEffectsAndOriginalHistory(t *testing.T) {
	f := savedPageNative(t)
	_, place := f.bookmark(t)
	const community = "b1700000-0000-4000-8000-000000000030"
	const activity = "b1700000-0000-4000-8000-000000000017"
	principal, org := f.id(t), f.id(t)
	f.exec(t, `INSERT INTO accounts(id,account_type)VALUES($1,'organization')`, principal)
	f.exec(t, `INSERT INTO organizations(id,account_id,organization_type,name)VALUES($1,$2,'club','仅隔离Moment关联组织144')`, org, principal)
	f.exec(t, `INSERT INTO organization_memberships(organization_id,user_account_id,role,status)VALUES($1,$2,'owner','active')`, org, f.actor.ID)
	f.exec(t, `INSERT INTO community_memberships(community_id,user_account_id,role,status)VALUES($1,$2,'member','active')`, community, f.actor.ID)
	in := mc144Input(t, f)
	in.Input.PlaceID = place
	in.Input.LocationPrecision = "place"
	in.Input.ActivityID = momentString(activity)
	in.Input.CommunityID = momentString(community)
	in.Input.OrganizationID = momentString(org)
	created, approved := mc144Write(t, f, in)
	var ownCity *string
	var contextCity string
	var locationCount int
	if e := f.pool.QueryRow(f.ctx, `SELECT m.city_id,l.city_id,(SELECT count(*) FROM moment_location_links ml WHERE ml.moment_id=m.id AND ml.status='ACTIVE' AND ml.kind='PLACE' AND ml.place_id=$3) FROM moments m JOIN moment_activity_links l ON l.moment_id=m.id AND l.activity_id=$2 WHERE m.id=$1`, created.MomentID, activity, place).Scan(&ownCity, &contextCity, &locationCount); e != nil || ownCity != nil || contextCity != "aberdeen-gb" || locationCount != 1 {
		t.Fatal("approved context City was lost or promoted into Moment geography", e)
	}
	if created.MomentID == in.OperationID || created.AppliedRevision != 1 {
		t.Fatal("CREATE reused op as result identity")
	}
	first := mc144Count(t, f)
	if first.moments != 1 || first.audits != 1 || first.outbox != 0 || first.history != 1 || first.links != 3 {
		t.Fatal("ordinary Person must not need/create Agent", first)
	}
	otherCity := "isolated-context181-" + f.id(t)
	f.exec(t, `INSERT INTO cities(id,name,region,country_code,time_zone,publication_status,source_label,source_ref,maintainer_label)VALUES($1,'仅合成关联边界城市181','isolated','GB','Europe/London','published','LOCAL_SYNTHETIC_FIXTURE','https://example.invalid/context181','仅合成维护')`, otherCity)
	crossCity := mc144Input(t, f)
	crossCity.Action, crossCity.MomentID, crossCity.ExpectedRevision = "UPDATE", created.MomentID, 1
	crossCity.Input.CityID = otherCity // Omitted Activity is not a re-selection in this City.
	crossCity, _ = mc144Preview(t, f, crossCity)
	if _, e := f.store.CommitMomentControl(f.ctx, f.digest, f.actor, crossCity); !errors.Is(e, content.ErrConflict) {
		t.Fatal("ordinary approved edit moved an omitted Activity across City", e)
	}
	if _, e := f.store.UpdateMomentDraft(f.ctx, f.actor.ID, created.MomentID, 1, *crossCity.Input); !errors.Is(e, content.ErrConflict) {
		t.Fatal("legacy draft writer bypassed omitted Activity City", e)
	}
	mc144NoEffect(t, f, first)
	var ordinaryAgents int
	if e := f.pool.QueryRow(f.ctx, `SELECT count(*) FROM agents WHERE principal_account_id=$1`, f.actor.ID).Scan(&ordinaryAgents); e != nil || ordinaryAgents != 0 {
		t.Fatal("ordinary fixture acquired an Agent", ordinaryAgents, e)
	}
	replay, e := f.store.CommitMomentControl(f.ctx, f.digest, f.actor, approved)
	if e != nil || content.MomentControlHash(replay) != content.MomentControlHash(created) {
		t.Fatal("original CREATE fact changed", e)
	}
	mc144NoEffect(t, f, first)
	changed := approved
	copy := *approved.Input
	copy.Title = "不同内容不可借原批准"
	changed.Input = &copy
	if _, e = f.store.CommitMomentControl(f.ctx, f.digest, f.actor, changed); !errors.Is(e, identity.ErrControlChanged) {
		t.Fatal("rebound original payload accepted", e)
	}
	mc144NoEffect(t, f, first)
	peer, _ := orgHumanNativePerson(t, f)
	if _, e = f.store.ReadMomentControlOperation(f.ctx, peer.SessionDigest, identity.Actor{ID: peer.AccountID, AccountType: "person"}, in.OperationID); !errors.Is(e, content.ErrNotFound) {
		t.Fatal("cross-owner tiny fact leaked", e)
	}
	// Optional metadata captures the original event exactly once without
	// becoming an ordinary person's write permission or running a consumer.
	var agent string
	if e = f.pool.QueryRow(f.ctx, `INSERT INTO agents(agent_type,principal_account_id)VALUES('personal',$1)RETURNING id`, f.actor.ID).Scan(&agent); e != nil {
		t.Fatal(e)
	}
	if _, e = f.store.EnsureAgentProfile(f.ctx, agent, actorref.PrincipalRef{Type: actorref.Person, ID: f.actor.ID}); e != nil {
		t.Fatal(e)
	}
	update := mc144Input(t, f)
	update.Action = "UPDATE"
	update.MomentID = created.MomentID
	update.ExpectedRevision = 1
	update.Input.PlaceID = place
	update.Input.Title = "仅编辑本人正文144"
	updated, _ := mc144Write(t, f, update)
	current, e := f.store.GetHumanMoment(f.ctx, f.digest, f.actor, created.MomentID)
	if e != nil || current.Revision != 2 || len(current.ActivityIDs) != 1 || current.ActivityIDs[0] != activity || current.CommunityID != community || current.OrganizationID != org {
		t.Fatal("omitted contexts failed to preserve own selectors", e)
	}
	f.exec(t, `UPDATE community_memberships SET status='left' WHERE community_id=$1 AND user_account_id=$2`, community, f.actor.ID)
	f.exec(t, `UPDATE organization_memberships SET status='removed' WHERE organization_id=$1 AND user_account_id=$2`, org, f.actor.ID)
	clear := update
	clear.OperationID = f.id(t)
	clear.ExpectedRevision = updated.AppliedRevision
	cv := *update.Input
	cv.ActivityID = momentString("")
	clear.Input = &cv
	removed, _ := mc144Write(t, f, clear)
	current, e = f.store.GetHumanMoment(f.ctx, f.digest, f.actor, created.MomentID)
	if e != nil || len(current.ActivityIDs) != 0 || current.CommunityID != community || current.OrganizationID != org {
		t.Fatal("empty is not omitted; private old links remain own data", e)
	}
	denied := clear
	denied.OperationID = f.id(t)
	denied.ExpectedRevision = removed.AppliedRevision
	dv := *clear.Input
	dv.CommunityID = momentString(community)
	denied.Input = &dv
	before := mc144Count(t, f)
	if _, e = f.store.PreviewMomentControl(f.ctx, f.digest, f.actor, denied); !errors.Is(e, identity.ErrControlChanged) {
		t.Fatal("explicit unavailable context reselect admitted", e)
	}
	mc144NoEffect(t, f, before)
	withdrawn, _ := mc144Write(t, f, content.MomentControlInput{OperationID: f.id(t), Action: "WITHDRAW", MomentID: created.MomentID, ExpectedRevision: removed.AppliedRevision})
	if withdrawn.AppliedRevision != 4 {
		t.Fatal("terminal canonical revision")
	}
	c := mc144Count(t, f)
	if c.moments != 1 || c.audits != 4 || c.outbox != 3 || c.history != 4 || c.links != 2 {
		t.Fatal("canonical effects not once", c)
	}
	var kinds int
	if e = f.pool.QueryRow(f.ctx, `SELECT count(DISTINCT event_type) FROM agent_domain_outbox WHERE subject_id=$1`, f.actor.ID).Scan(&kinds); e != nil || kinds != 2 {
		t.Fatal("original update/withdraw outbox kinds missing", kinds, e)
	}
	own, e := New(f.pool, false).ReadMomentControlOperation(f.ctx, f.digest, f.actor, created.OperationID)
	if e != nil || content.MomentControlHash(own) != content.MomentControlHash(created) {
		t.Fatal("current target/membership hid immutable own fact", e)
	}
	raw, _ := json.Marshal(own)
	if strings.Contains(string(raw), "PRIVATE_MOMENT") || strings.Contains(string(raw), "title") || strings.Contains(string(raw), "coordinates") {
		t.Fatal("tiny history copied private/external content")
	}
	suc, e := f.store.StopMomentControlOperation(f.ctx, f.digest, f.actor, created.OperationID)
	if e != nil || suc.Status != "SUCCEEDED" || suc.MomentID != created.MomentID {
		t.Fatal("stop erased original success", e)
	}
	mc144NoEffect(t, f, c)
	late, _ := mc144Preview(t, f, mc144Input(t, f))
	stopped, e := f.store.StopMomentControlOperation(f.ctx, f.digest, f.actor, late.OperationID)
	if e != nil || stopped.Status != "STOPPED" || stopped.AppliedRevision != 0 {
		t.Fatal("original zero-effect close", e)
	}
	afterStop := mc144Count(t, f)
	v, e := f.store.CommitMomentControl(f.ctx, f.digest, f.actor, late)
	if e != nil || v.Status != "STOPPED" {
		t.Fatal("late CREATE crossed tombstone", e)
	}
	mc144NoEffect(t, f, afterStop)
	for _, q := range []string{`UPDATE human_moment_control_operations SET committed_at=clock_timestamp() WHERE actor_account_id=$1`, `DELETE FROM human_moment_control_operations WHERE actor_account_id=$1`} {
		if _, e = f.pool.Exec(f.ctx, q, f.actor.ID); e == nil {
			t.Fatal("owned immutable history was mutable")
		}
	}
	mc144NoEffect(t, f, afterStop)
	down, e := os.ReadFile(filepath.Join("..", "..", "migrations", "144_moment_draft_control.down.sql"))
	if e != nil {
		t.Fatal(e)
	}
	conn, e := f.pool.Acquire(f.ctx)
	if e != nil {
		t.Fatal(e)
	}
	_, downErr := conn.Exec(f.ctx, string(down))
	_, rollbackErr := conn.Exec(f.ctx, `ROLLBACK`)
	conn.Release()
	if downErr == nil || rollbackErr != nil {
		t.Fatal("history-bearing downgrade admitted or connection not restored", downErr, rollbackErr)
	}
	mc144NoEffect(t, f, afterStop)
	// Three event kinds are covered on a second, metadata-eligible draft.
	second, _ := mc144Write(t, f, mc144Input(t, f))
	max := content.MomentControlMaxRevision
	f.exec(t, `UPDATE moments SET revision=$2 WHERE id=$1`, second.MomentID, max)
	maxUpdate := mc144Input(t, f)
	maxUpdate.Action = "UPDATE"
	maxUpdate.MomentID = second.MomentID
	maxUpdate.ExpectedRevision = max
	if _, e = f.store.PreviewMomentControl(f.ctx, f.digest, f.actor, maxUpdate); !errors.Is(e, identity.ErrControlInvalid) {
		t.Fatal("nonterminal unsafe revision admitted", e)
	}
	maxOut, _ := mc144Write(t, f, content.MomentControlInput{OperationID: f.id(t), Action: "WITHDRAW", MomentID: second.MomentID, ExpectedRevision: max})
	if maxOut.AppliedRevision != max {
		t.Fatal("max cleanup overflow")
	}
	if e = f.pool.QueryRow(f.ctx, `SELECT count(DISTINCT event_type) FROM agent_domain_outbox WHERE subject_id=$1`, f.actor.ID).Scan(&kinds); e != nil || kinds != 3 {
		t.Fatal("canonical create/update/withdraw event capture incomplete", kinds, e)
	}
	t.Run("stopFirstActualLateProducer", func(t *testing.T) { mc144StopCompetition(t, f) })
	t.Run("optionalMetadataWaitOriginalSession", func(t *testing.T) {
		pending, _ := mc144Preview(t, f, mc144Input(t, f))
		ctx := mc144Bound(t, f)
		before := mc144Count(t, f)
		outer, pid := mc144Outer(t, f, `SELECT agent_id FROM agent_profiles WHERE agent_id=$1 FOR UPDATE`, agent)
		done := make(chan error, 1)
		go func() { _, e := f.store.CommitMomentControl(ctx, f.digest, f.actor, pending); done <- e }()
		hcNativeWaitPID(t, f, pid)
		f.exec(t, `UPDATE sessions SET revoked_at=clock_timestamp() WHERE id=$1`, f.session)
		f.exec(t, `UPDATE sessions SET revoked_at=NULL WHERE id=$1`, f.session)
		if e := outer.Commit(f.ctx); e != nil {
			t.Fatal(e)
		}
		mc144Result(t, done, identity.ErrControlChanged)
		mc144NoEffect(t, f, before)
	})
}

func mc144StopCompetition(t *testing.T, f *savedPageNativeFixture) {
	in, _ := mc144Preview(t, f, mc144Input(t, f))
	ctx := mc144Bound(t, f)
	before := mc144Count(t, f)
	outer, pid := mc144Outer(t, f, `SELECT id FROM sessions WHERE id=$1 FOR UPDATE`, f.session)
	type result struct {
		r content.MomentControlReceipt
		e error
	}
	stops := make(chan result, 1)
	go func() {
		r, e := f.store.StopMomentControlOperation(ctx, f.digest, f.actor, in.OperationID)
		stops <- result{r, e}
	}()
	stopPID := share143BlockedPID(t, f, pid)
	done := make(chan result, 1)
	go func() { r, e := f.store.CommitMomentControl(ctx, f.digest, f.actor, in); done <- result{r, e} }()
	hcNativeWaitPID(t, f, stopPID)
	if e := outer.Commit(f.ctx); e != nil {
		t.Fatal(e)
	}
	for _, ch := range []<-chan result{stops, done} {
		select {
		case r := <-ch:
			if r.e != nil || r.r.Status != "STOPPED" {
				t.Fatal("actual late producer crossed committed stop", r.e, r.r.Status)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("STOP/producer lock cycle")
		}
	}
	after := mc144Count(t, f)
	before.history++
	if after != before {
		t.Fatal("actual STOP race changed canonical effects", before, after)
	}
}

func TestMomentControlNativeOriginalFrameSourceAfterWait(t *testing.T) {
	f := savedPageNative(t)
	t.Run("principalABA", func(t *testing.T) {
		in, _ := mc144Preview(t, f, mc144Input(t, f))
		ctx := mc144Bound(t, f)
		before := mc144Count(t, f)
		var generation int64
		if e := f.pool.QueryRow(f.ctx, `SELECT authorization_generation FROM sessions WHERE id=$1`, f.session).Scan(&generation); e != nil {
			t.Fatal(e)
		}
		outer, pid := mc144Outer(t, f, `SELECT id FROM accounts WHERE id=$1 FOR UPDATE`, f.actor.ID)
		done := make(chan error, 1)
		go func() { _, e := f.store.CommitMomentControl(ctx, f.digest, f.actor, in); done <- e }()
		hcNativeWaitPID(t, f, pid)
		if _, e := outer.Exec(f.ctx, `UPDATE accounts SET status='suspended' WHERE id=$1`, f.actor.ID); e != nil {
			t.Fatal(e)
		}
		if _, e := outer.Exec(f.ctx, `UPDATE accounts SET status='active' WHERE id=$1`, f.actor.ID); e != nil {
			t.Fatal(e)
		}
		if e := outer.Commit(f.ctx); e != nil {
			t.Fatal(e)
		}
		var current int64
		if e := f.pool.QueryRow(f.ctx, `SELECT authorization_generation FROM sessions WHERE id=$1`, f.session).Scan(&current); e != nil || current != generation {
			t.Fatal("principal ABA oracle accidentally changed Session", e)
		}
		mc144Result(t, done, identity.ErrControlChanged)
		mc144NoEffect(t, f, before)
	})
	created, _ := mc144Write(t, f, mc144Input(t, f))
	for _, name := range []string{"SessionABA", "sameContentSourceABA"} {
		t.Run(name, func(t *testing.T) {
			in := mc144Input(t, f)
			in.Action = "UPDATE"
			in.MomentID = created.MomentID
			in.ExpectedRevision = 1
			in, _ = mc144Preview(t, f, in)
			ctx := mc144Bound(t, f)
			before := mc144Count(t, f)
			outer, pid := mc144Outer(t, f, `SELECT id FROM moments WHERE id=$1 FOR UPDATE`, created.MomentID)
			done := make(chan error, 1)
			go func() { _, e := f.store.CommitMomentControl(ctx, f.digest, f.actor, in); done <- e }()
			hcNativeWaitPID(t, f, pid)
			if name == "SessionABA" {
				f.exec(t, `UPDATE sessions SET revoked_at=clock_timestamp() WHERE id=$1`, f.session)
				f.exec(t, `UPDATE sessions SET revoked_at=NULL WHERE id=$1`, f.session)
			} else {
				if _, e := outer.Exec(f.ctx, `UPDATE moments SET title=title WHERE id=$1`, created.MomentID); e != nil {
					t.Fatal(e)
				}
			}
			if e := outer.Commit(f.ctx); e != nil {
				t.Fatal(e)
			}
			mc144Result(t, done, identity.ErrControlChanged)
			mc144NoEffect(t, f, before)
		})
	}
	t.Run("membershipABA", func(t *testing.T) {
		const community = "b1700000-0000-4000-8000-000000000030"
		f.exec(t, `INSERT INTO community_memberships(community_id,user_account_id,role,status)VALUES($1,$2,'member','active')`, community, f.actor.ID)
		in := mc144Input(t, f)
		in.Input.CommunityID = momentString(community)
		in, _ = mc144Preview(t, f, in)
		ctx := mc144Bound(t, f)
		before := mc144Count(t, f)
		outer, pid := mc144Outer(t, f, `SELECT id FROM accounts WHERE id=$1 FOR UPDATE`, f.actor.ID)
		done := make(chan error, 1)
		go func() { _, e := f.store.CommitMomentControl(ctx, f.digest, f.actor, in); done <- e }()
		hcNativeWaitPID(t, f, pid)
		f.exec(t, `UPDATE community_memberships SET status='left' WHERE community_id=$1 AND user_account_id=$2`, community, f.actor.ID)
		f.exec(t, `UPDATE community_memberships SET status='active' WHERE community_id=$1 AND user_account_id=$2`, community, f.actor.ID)
		if e := outer.Commit(f.ctx); e != nil {
			t.Fatal(e)
		}
		mc144Result(t, done, identity.ErrControlChanged)
		mc144NoEffect(t, f, before)
	})
	t.Run("sourceBlockABA", func(t *testing.T) {
		const activity = "b1700000-0000-4000-8000-000000000017"
		var host string
		if e := f.pool.QueryRow(f.ctx, `SELECT host_account_id FROM activities WHERE id=$1`, activity).Scan(&host); e != nil || host == f.actor.ID {
			t.Fatal("independent current Activity host needed", e)
		}
		in := mc144Input(t, f)
		in.Input.ActivityID = momentString(activity)
		in, _ = mc144Preview(t, f, in)
		ctx := mc144Bound(t, f)
		before := mc144Count(t, f)
		var blockVersion int64
		if e := f.pool.QueryRow(f.ctx, `SELECT coalesce((SELECT version FROM person_block_control_epochs WHERE owner_account_id=$1 AND target_account_id=$2),0)`, f.actor.ID, host).Scan(&blockVersion); e != nil {
			t.Fatal(e)
		}
		outer, pid := mc144Outer(t, f, `SELECT id FROM accounts WHERE id=$1 FOR UPDATE`, f.actor.ID)
		done := make(chan error, 1)
		go func() { _, e := f.store.CommitMomentControl(ctx, f.digest, f.actor, in); done <- e }()
		hcNativeWaitPID(t, f, pid)
		// This transaction already owns the account row. A pool-side INSERT
		// would wait for its FK lock instead of exercising the writer's source
		// wait. The real INSERT/DELETE triggers must persist the pair epoch.
		if _, e := outer.Exec(f.ctx, `INSERT INTO account_blocks(blocker_account_id,blocked_account_id)VALUES($1,$2)`, f.actor.ID, host); e != nil {
			t.Fatal(e)
		}
		if _, e := outer.Exec(f.ctx, `DELETE FROM account_blocks WHERE blocker_account_id=$1 AND blocked_account_id=$2`, f.actor.ID, host); e != nil {
			t.Fatal(e)
		}
		if e := outer.Commit(f.ctx); e != nil {
			t.Fatal(e)
		}
		var latestVersion int64
		var blockExists bool
		if e := f.pool.QueryRow(f.ctx, `SELECT version,EXISTS(SELECT 1 FROM account_blocks WHERE blocker_account_id=$1 AND blocked_account_id=$2) FROM person_block_control_epochs WHERE owner_account_id=$1 AND target_account_id=$2`, f.actor.ID, host).Scan(&latestVersion, &blockExists); e != nil || latestVersion != blockVersion+2 || blockExists {
			t.Fatal("actual block ABA did not retain its native epoch", e)
		}
		mc144Result(t, done, identity.ErrControlChanged)
		mc144NoEffect(t, f, before)
	})
	t.Run("ordinaryIdleRenewal", func(t *testing.T) {
		in := mc144Input(t, f)
		in.Action = "UPDATE"
		in.MomentID = created.MomentID
		in.ExpectedRevision = 1
		in, _ = mc144Preview(t, f, in)
		ctx := mc144Bound(t, f)
		outer, pid := mc144Outer(t, f, `SELECT id FROM moments WHERE id=$1 FOR UPDATE`, created.MomentID)
		done := make(chan error, 1)
		go func() { _, e := f.store.CommitMomentControl(ctx, f.digest, f.actor, in); done <- e }()
		hcNativeWaitPID(t, f, pid)
		f.exec(t, `UPDATE sessions SET idle_expires_at=clock_timestamp()+interval '40 minutes' WHERE id=$1`, f.session)
		if e := outer.Commit(f.ctx); e != nil {
			t.Fatal(e)
		}
		mc144Result(t, done, nil)
	})
	t.Run("previewSourceAfterFinalSessionWait", func(t *testing.T) {
		const activity = "b1700000-0000-4000-8000-000000000017"
		in := mc144Input(t, f)
		in.Action = "UPDATE"
		in.MomentID = created.MomentID
		in.ExpectedRevision = 2
		in.Input.ActivityID = momentString(activity)
		_, p := mc144Preview(t, f, in)
		outer, pid := mc144Outer(t, f, `SELECT id FROM sessions WHERE id=$1 FOR UPDATE`, f.session)
		done := make(chan error, 1)
		go func() { done <- p.ResponseProof.RevalidateHumanControl(f.ctx) }()
		hcNativeWaitPID(t, f, pid)
		// Location reads hold the City row FOR SHARE while the proof waits
		// for the Session. Mutating that City here would make the test's
		// three connections wait on one another. An independently visible
		// Activity is also part of the captured source and is not that lock.
		mutationCtx, cancel := context.WithTimeout(f.ctx, 2*time.Second)
		defer cancel()
		if tag, e := f.pool.Exec(mutationCtx, `UPDATE activities SET title=title||' · source changed' WHERE id=$1`, activity); e != nil || tag.RowsAffected() != 1 {
			t.Fatal("independent source mutation before final Session release", e)
		}
		if e := outer.Commit(f.ctx); e != nil {
			t.Fatal(e)
		}
		mc144Result(t, done, identity.ErrControlChanged)
	})
	t.Run("ownReaderOriginalGenerationAfterWait", func(t *testing.T) {
		for _, list := range []bool{false, true} {
			ctx := mc144Bound(t, f)
			outer, pid := mc144Outer(t, f, `SELECT id FROM moments WHERE id=$1 FOR UPDATE`, created.MomentID)
			done := make(chan error, 1)
			go func() {
				var e error
				if list {
					_, e = f.store.ListHumanMoments(ctx, f.digest, f.actor)
				} else {
					_, e = f.store.GetHumanMoment(ctx, f.digest, f.actor, created.MomentID)
				}
				done <- e
			}()
			hcNativeWaitPID(t, f, pid)
			f.exec(t, `UPDATE sessions SET revoked_at=clock_timestamp() WHERE id=$1`, f.session)
			f.exec(t, `UPDATE sessions SET revoked_at=NULL WHERE id=$1`, f.session)
			if e := outer.Commit(f.ctx); e != nil {
				t.Fatal(e)
			}
			mc144Result(t, done, identity.ErrControlChanged)
		}
	})
	t.Run("sourceDeadlineAfterTentativeEffects", func(t *testing.T) {
		var agent string
		if e := f.pool.QueryRow(f.ctx, `INSERT INTO agents(agent_type,principal_account_id)VALUES('personal',$1)RETURNING id`, f.actor.ID).Scan(&agent); e != nil {
			t.Fatal(e)
		}
		if _, e := f.store.EnsureAgentProfile(f.ctx, agent, actorref.PrincipalRef{Type: actorref.Person, ID: f.actor.ID}); e != nil {
			t.Fatal(e)
		}
		_, place := f.bookmark(t)
		var deadline time.Time
		if e := f.pool.QueryRow(f.ctx, `UPDATE places SET expires_at=clock_timestamp()+interval '6 seconds' WHERE id=$1 RETURNING expires_at`, place).Scan(&deadline); e != nil {
			t.Fatal(e)
		}
		in := mc144Input(t, f)
		in.Input.PlaceID = place
		in.Input.LocationPrecision = "place"
		in, p := mc144Preview(t, f, in)
		if p.ExpiresAt.After(deadline) {
			t.Fatal("actual source deadline renewed")
		}
		ctx := mc144Bound(t, f)
		before := mc144Count(t, f)
		outer, pid := mc144Outer(t, f, `SELECT id FROM sessions WHERE id=$1 FOR UPDATE`, f.session)
		done := make(chan error, 1)
		go func() { _, e := f.store.CommitMomentControl(ctx, f.digest, f.actor, in); done <- e }()
		hcNativeWaitPID(t, f, pid)
		time.Sleep(time.Until(p.ExpiresAt) + 75*time.Millisecond)
		if e := outer.Commit(f.ctx); e != nil {
			t.Fatal(e)
		}
		mc144Result(t, done, identity.ErrControlChanged)
		mc144NoEffect(t, f, before)
	})
}

func mc144RetainedPlace(t *testing.T, f *savedPageNativeFixture, place string, shortLease int) string {
	t.Helper()
	permission, candidate := f.id(t), f.id(t)
	reviewer, _ := orgHumanNativePerson(t, f)
	f.exec(t, `INSERT INTO city_editor_memberships(city_id,account_id,role)VALUES('aberdeen-gb',$1,'contributor'),('aberdeen-gb',$2,'reviewer')`, f.actor.ID, reviewer.AccountID)
	tx, e := f.pool.Begin(f.ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer tx.Rollback(context.Background())
	url := "https://example.invalid/isolated-moment144/" + place
	exec := func(q string, args ...any) {
		t.Helper()
		if _, e := tx.Exec(f.ctx, q, args...); e != nil {
			t.Fatal("actual independently reviewed114 fixture", e)
		}
	}
	exec(`INSERT INTO place_source_permissions(id,candidate_id,state,acquisition,origin_provider,source_url,license_url,license_version,attribution,acquired_at,retention_until,declared_by)VALUES($1,$2,'CONFIRMED','FIRST_PARTY','BIRDTIE_FIRST_PARTY',$3,'https://example.invalid/isolated-license144','isolated-v1','仅明确合成本机资料，不是实际许可',clock_timestamp()-interval '1 minute',clock_timestamp()+interval '2 hours',$4)`, permission, candidate, url, f.actor.ID)
	exec(`INSERT INTO place_candidates(id,city_id,submitted_by,name,category_code,latitude,longitude,location_precision,source_label,source_url,rights_note,expires_at,permission_id,field_claims)
 SELECT $1,p.city_id,$2,p.name,p.category_code,p.latitude,p.longitude,p.location_precision,'LOCAL_SYNTHETIC_FIXTURE',$3,'仅隔离合成第一方地点和独立人工审核',clock_timestamp()+interval '1 hour',$4,jsonb_build_array(jsonb_build_object('fieldKey','name','value',p.name),jsonb_build_object('fieldKey','coordinates','value',jsonb_build_object('coordinateSystem',p.coordinate_system,'latitude',p.latitude,'longitude',p.longitude))) FROM places p WHERE p.id=$5`, candidate, f.actor.ID, url, permission, place)
	exec(`UPDATE place_source_permissions SET reviewed_by=$2,reviewed_at=clock_timestamp(),updated_at=clock_timestamp() WHERE id=$1`, permission, reviewer.AccountID)
	exec(`UPDATE place_candidates SET status='published',reviewed_by=$2,reviewed_at=clock_timestamp(),resolved_place_id=$3 WHERE id=$1`, candidate, reviewer.AccountID, place)
	exec(`INSERT INTO place_sources(place_id,candidate_id,source_label,source_url,rights_note,reviewer_account_id,verified_at,expires_at,permission_id)VALUES($1,$2,'LOCAL_SYNTHETIC_FIXTURE',$3,'仅本机合成权利资料',$4,clock_timestamp(),clock_timestamp()+interval '1 hour',$5)`, place, candidate, url, reviewer.AccountID, permission)
	exec(`WITH stamp AS MATERIALIZED(SELECT clock_timestamp() n) INSERT INTO place_field_evidence(place_id,source_candidate_id,field_key,value,observed_at,verified_at,valid_from,valid_to,refresh_due,permission_id,permission_version,conflict_state)
	SELECT p.id,$2,k.kind,CASE WHEN k.kind='name' THEN to_jsonb(p.name) ELSE jsonb_build_object('coordinateSystem',p.coordinate_system,'latitude',p.latitude,'longitude',p.longitude) END,stamp.n-interval '1 minute',stamp.n-interval '1 second',stamp.n-interval '1 minute',stamp.n+make_interval(secs=>CASE WHEN $4::int>0 THEN $4::int ELSE 3600 END),stamp.n+make_interval(secs=>CASE WHEN $4::int>0 THEN $4::int ELSE 1800 END),$3,1,'NONE' FROM places p CROSS JOIN(VALUES('name'),('coordinates'))k(kind) CROSS JOIN stamp WHERE p.id=$1`, place, candidate, permission, shortLease)
	exec(`UPDATE places SET source_ref=$2 WHERE id=$1`, place, url)
	if e = tx.Commit(f.ctx); e != nil {
		t.Fatal(e)
	}
	var current bool
	if e = f.pool.QueryRow(f.ctx, `SELECT `+currentRetainedPlaceVisibilitySQL("p", "clock_timestamp()")+` FROM places p WHERE p.id=$1`, place).Scan(&current); e != nil || !current {
		t.Fatal("real114 reviewed fixture not visible", e)
	}
	return permission
}
func TestMomentControlNativeRetainedSelectorAndCleanup(t *testing.T) {
	f := savedPageNative(t)
	_, place := f.bookmark(t)
	permission := mc144RetainedPlace(t, f, place, 0)
	in := mc144Input(t, f)
	in.Input.PlaceID = place
	in.Input.LocationPrecision = "place"
	created, _ := mc144Write(t, f, in)
	f.exec(t, `UPDATE place_source_permissions SET state='PROHIBITED',version=version+1,revoked_at=clock_timestamp(),updated_at=clock_timestamp() WHERE id=$1`, permission)
	before := mc144Count(t, f)
	newDraft := mc144Input(t, f)
	newDraft.Input.PlaceID, newDraft.Input.LocationPrecision = place, "place"
	if _, e := f.store.PreviewMomentControl(f.ctx, f.digest, f.actor, newDraft); !errors.Is(e, identity.ErrControlChanged) {
		t.Fatal("revoked114 source admitted a new draft selector", e)
	}
	var retainedID string
	if e := f.pool.QueryRow(f.ctx, `SELECT id FROM moment_location_links WHERE moment_id=$1 AND status='ACTIVE' AND kind='PLACE' AND place_id=$2`, created.MomentID, place).Scan(&retainedID); e != nil {
		t.Fatal(e)
	}
	// 160 preserves the exact existing private selector, without renewing
	// its public name, coordinates, or ability to choose a new relation.
	for _, precision := range []string{"place", "city"} {
		v := mc144Input(t, f)
		v.Action, v.MomentID, v.ExpectedRevision = "UPDATE", created.MomentID, 1
		v.Input.CityID, v.Input.PlaceID, v.Input.LocationPrecision = "", "", "none"
		id := retainedID
		if precision == "place" {
			id = f.id(t) // A fresh relation is not the retained private selector.
		}
		locations := []content.MomentLocationInput{{ID: id, Kind: "PLACE", PlaceID: place, Precision: precision}}
		v.Input.Locations = &locations
		if _, e := f.store.PreviewMomentControl(f.ctx, f.digest, f.actor, v); !errors.Is(e, identity.ErrControlChanged) {
			t.Fatal("revoked114 source admitted a new relation or changed precision", precision, e)
		}
	}
	mc144NoEffect(t, f, before)
	own, e := f.store.GetHumanMoment(f.ctx, f.digest, f.actor, created.MomentID)
	if e != nil || own.Body != in.Input.Body || own.PlaceID != place {
		t.Fatal("source revoke erased person's own material", e)
	}
	edit := mc144Input(t, f)
	edit.Action = "UPDATE"
	edit.MomentID = created.MomentID
	edit.ExpectedRevision = 1
	edit.Input.PlaceID = place
	edit.Input.Title = "本人旧稿在来源撤回后仍可编辑"
	edit.Input.LocationPrecision = "place"
	approved, p := mc144Preview(t, f, edit)
	if !p.PreservedPlaceUnavailable || len(p.Locations) != 1 || p.Locations[0].ID != retainedID || p.Locations[0].Available || p.Locations[0].Anchor != nil || p.Locations[0].PlaceName != "" || p.Locations[0].CityName != "" {
		t.Fatal("unavailable own selector presented as new public authority")
	}
	updated, e := f.store.CommitMomentControl(f.ctx, f.digest, f.actor, approved)
	if e != nil || updated.AppliedRevision != 2 {
		t.Fatal("private text preservation rejected", e)
	}
	own, e = f.store.GetHumanMoment(f.ctx, f.digest, f.actor, created.MomentID)
	if e != nil || own.CityID != "" || own.PlaceID != place || own.LocationPrecision != "place" || own.Title != edit.Input.Title {
		t.Fatal("preserved own selector/precision mismatch", e)
	}
	wrongCity := edit
	wrongCity.OperationID = f.id(t)
	wrongCity.ExpectedRevision = 2
	copy := *edit.Input
	copy.CityID = "isolated-moment144-" + f.id(t)
	f.exec(t, `INSERT INTO cities(id,name,region,country_code,time_zone,publication_status,source_label,source_ref,maintainer_label)VALUES($1,'仅合成另一城市144','isolated','GB','Europe/London','published','LOCAL_SYNTHETIC_FIXTURE','https://example.invalid/city144','仅合成维护')`, copy.CityID)
	wrongCity.Input = &copy
	before = mc144Count(t, f)
	_, preserved := mc144Preview(t, f, wrongCity)
	if !preserved.PreservedPlaceUnavailable || len(preserved.Locations) != 1 || preserved.Locations[0].ID != retainedID || preserved.Locations[0].Available || preserved.Locations[0].CityID != "" || preserved.Locations[0].Anchor != nil {
		t.Fatal("compatibility City input promoted an unavailable Place into new geography")
	}
	mc144NoEffect(t, f, before)
	max := content.MomentControlMaxRevision
	f.exec(t, `UPDATE moments SET revision=$2 WHERE id=$1`, created.MomentID, max)
	withdrawal, _ := mc144Write(t, f, content.MomentControlInput{OperationID: f.id(t), Action: "WITHDRAW", MomentID: created.MomentID, ExpectedRevision: max})
	if withdrawal.AppliedRevision != max {
		t.Fatal("expired own selector blocked max terminal cleanup")
	}
	history, e := f.store.ReadMomentControlOperation(f.ctx, f.digest, f.actor, created.OperationID)
	if e != nil || history.Status != "SUCCEEDED" || history.AppliedRevision != 1 || history.MomentID != created.MomentID {
		t.Fatal("retirement/current target hid original tiny fact", e)
	}
	t.Run("actualFieldEvidenceExpiryAfterEffect", func(t *testing.T) {
		g := savedPageNative(t)
		_, point := g.bookmark(t)
		var agent string
		if e := g.pool.QueryRow(g.ctx, `INSERT INTO agents(agent_type,principal_account_id)VALUES('personal',$1)RETURNING id`, g.actor.ID).Scan(&agent); e != nil {
			t.Fatal(e)
		}
		if _, e := g.store.EnsureAgentProfile(g.ctx, agent, actorref.PrincipalRef{Type: actorref.Person, ID: g.actor.ID}); e != nil {
			t.Fatal(e)
		}
		mc144RetainedPlace(t, g, point, 6)
		pending := mc144Input(t, g)
		pending.Input.PlaceID = point
		pending.Input.LocationPrecision = "place"
		pending, preview := mc144Preview(t, g, pending)
		var fieldDeadline time.Time
		if e := g.pool.QueryRow(g.ctx, `SELECT min(least(valid_to,refresh_due)) FROM place_field_evidence WHERE place_id=$1`, point).Scan(&fieldDeadline); e != nil || preview.ExpiresAt.After(fieldDeadline) {
			t.Fatal("original114 field lease renewed", e)
		}
		ctx := mc144Bound(t, g)
		before := mc144Count(t, g)
		outer, pid := mc144Outer(t, g, `SELECT id FROM sessions WHERE id=$1 FOR UPDATE`, g.session)
		done := make(chan error, 1)
		go func() { _, e := g.store.CommitMomentControl(ctx, g.digest, g.actor, pending); done <- e }()
		hcNativeWaitPID(t, g, pid)
		time.Sleep(time.Until(preview.ExpiresAt) + 75*time.Millisecond)
		if e := outer.Commit(g.ctx); e != nil {
			t.Fatal(e)
		}
		mc144Result(t, done, identity.ErrControlChanged)
		mc144NoEffect(t, g, before)
	})
}
