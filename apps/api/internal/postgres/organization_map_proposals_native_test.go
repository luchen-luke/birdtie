package postgres

import (
	"context"
	"errors"
	org "github.com/birdtie/birdtie/apps/api/internal/organization"
	"testing"
	"time"
)

func TestOrganizationHumanMapNativeIndependentProposalAndPreservedPoint(t *testing.T) {
	f, id, owner := orgHumanNative(t)
	reviewer, se := orgHumanNativePerson(t, f)
	f.exec(t, `INSERT INTO city_editor_memberships(city_id,account_id,role,state)VALUES('aberdeen-gb',$1,'reviewer','active')`, reviewer.AccountID)
	f.exec(t, `INSERT INTO organization_map_locations(organization_id,city_id,latitude,longitude,visibility,review_status,submitted_by,reviewed_by,reviewed_at,revision)VALUES($1,'aberdeen-gb',57.1,-2.1,'public','approved',$2,$3,clock_timestamp(),7)`, id, owner.AccountID, reviewer.AccountID)
	var xmin string
	if e := f.pool.QueryRow(f.ctx, `SELECT xmin::text FROM organization_map_locations WHERE organization_id=$1`, id).Scan(&xmin); e != nil {
		t.Fatal(e)
	}
	base := org.ControlInput{Action: "PROPOSE", OperationID: f.id(t), ExpectedAuthorityVersion: orgHumanNativeVersion(t, f, id), CityID: "aberdeen-gb", Latitude: 57.21, Longitude: -2.11, ExpectedCanonicalRevision: 7}
	r := orgHumanNativeWrite(t, f, owner, id, base)
	assertOld := func() {
		t.Helper()
		var x string
		var lat float64
		if e := f.pool.QueryRow(f.ctx, `SELECT xmin::text,latitude FROM organization_map_locations WHERE organization_id=$1`, id).Scan(&x, &lat); e != nil || x != xmin || lat != 57.1 {
			t.Fatal("pending/rejected/withdrawn changed canonical bytes", e)
		}
	}
	assertOld()
	state, e := f.store.ReadMapControl(f.ctx, reviewer, id)
	if e != nil || state.Role != "reviewer" || len(state.Proposals) != 1 || !state.Proposals[0].CanApprove {
		t.Fatal("actual current independent reviewer", e)
	}
	selfReview := org.ControlInput{Action: "APPROVE_POINT", OperationID: f.id(t), ExpectedAuthorityVersion: base.ExpectedAuthorityVersion, ProposalID: r.ProposalID, ExpectedProposalVersion: 1, ExpectedCanonicalRevision: 7}
	f.exec(t, `INSERT INTO city_editor_memberships(city_id,account_id,role,state)VALUES('aberdeen-gb',$1,'reviewer','active')`, owner.AccountID)
	if _, e = f.store.PreviewOrganizationControl(f.ctx, owner, id, selfReview); !errors.Is(e, org.ErrForbidden) {
		t.Fatal("submitter self approved", e)
	}
	reject := selfReview
	reject.Action = "REJECT_POINT"
	reject.OperationID = f.id(t)
	orgHumanNativeWrite(t, f, reviewer, id, reject)
	assertOld()
	base.OperationID = f.id(t)
	second := orgHumanNativeWrite(t, f, owner, id, base)
	orgHumanNativeWrite(t, f, owner, id, org.ControlInput{Action: "WITHDRAW_POINT", OperationID: f.id(t), ExpectedAuthorityVersion: base.ExpectedAuthorityVersion, ProposalID: second.ProposalID, ExpectedProposalVersion: 1})
	assertOld()
	base.OperationID = f.id(t)
	third := orgHumanNativeWrite(t, f, owner, id, base)
	approve := selfReview
	approve.OperationID = f.id(t)
	approve.ProposalID = third.ProposalID
	r = orgHumanNativeWrite(t, f, reviewer, id, approve)
	if r.CanonicalRevision != 8 || r.ProposalVersion != 2 {
		t.Fatal("atomic approved revision", r)
	}
	pins, e := f.store.ListPublicMapPins(f.ctx, "aberdeen-gb")
	found := false
	for _, p := range pins {
		if p.ID == id && p.Latitude == 57.21 {
			found = true
		}
	}
	if e != nil || !found {
		t.Fatal("approved actual canonical public point", e)
	}
	history, e := f.store.StopOrganizationControlOperation(f.ctx, reviewer, id, r.OperationID, r.AuthorityVersion)
	if e != nil || history.Status != "SUCCEEDED" || history.CanonicalRevision != 8 {
		t.Fatal("stop erased original successful point", e)
	}
	base.OperationID = f.id(t)
	base.ExpectedCanonicalRevision = 8
	base.Latitude = 57.3
	fourth := orgHumanNativeWrite(t, f, owner, id, base)
	approve.OperationID = f.id(t)
	approve.ProposalID = fourth.ProposalID
	approve.ExpectedCanonicalRevision = 8
	orgHumanNativeWait(t, f, id, reviewer, se, approve)
	current, e := f.store.ReadMapControl(f.ctx, owner, id)
	if e != nil || current.Canonical == nil || current.Canonical.Revision != 8 || current.Canonical.Latitude != 57.21 {
		t.Fatal("Session ABA replaced old point", e)
	}
	fresh, e := f.store.ReadMembersControl(f.ctx, owner, id)
	if e != nil {
		t.Fatal(e)
	}
	f.exec(t, `UPDATE sessions SET idle_expires_at=clock_timestamp()+interval '50 minutes' WHERE id=$1`, f.session)
	if e = f.store.ValidateOrganizationControlResponse(f.ctx, owner, id, fresh.Proof, fresh.ExpiresAt); e != nil {
		t.Fatal("ordinary idle renewal retired source", e)
	}
	t.Run("responseLeaseExpiresDuringOriginalSessionWait", func(t *testing.T) {
		lock, e := f.pool.Begin(f.ctx)
		if e != nil {
			t.Fatal(e)
		}
		defer lock.Rollback(context.Background())
		if _, e = lock.Exec(f.ctx, `SELECT id FROM sessions WHERE id=$1 FOR UPDATE`, f.session); e != nil {
			t.Fatal(e)
		}
		until := time.Now().Add(500 * time.Millisecond)
		done := make(chan error, 1)
		go func() { done <- f.store.ValidateOrganizationControlResponse(f.ctx, owner, id, fresh.Proof, until) }()
		wait := false
		end := time.Now().Add(3 * time.Second)
		for time.Now().Before(end) {
			var n int
			if e = f.pool.QueryRow(f.ctx, `SELECT count(*) FROM pg_stat_activity WHERE datname=current_database() AND wait_event_type='Lock' AND query LIKE '%FOR SHARE OF s%'`).Scan(&n); e != nil {
				t.Fatal(e)
			}
			if n > 0 {
				wait = true
				break
			}
			time.Sleep(10 * time.Millisecond)
		}
		if !wait {
			t.Fatal("actual response Session wait missing")
		}
		time.Sleep(time.Until(until) + 20*time.Millisecond)
		if e = lock.Commit(f.ctx); e != nil {
			t.Fatal(e)
		}
		select {
		case e = <-done:
			if !errors.Is(e, org.ErrControlChanged) {
				t.Fatal("late response sent expired private lease", e)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("response wait did not retire")
		}
	})
	expiry := time.Now().UTC().Add(650 * time.Millisecond)
	f.exec(t, `UPDATE cities SET expires_at=$1 WHERE id='aberdeen-gb'`, expiry)
	approve.OperationID = f.id(t)
	p, e := f.store.PreviewOrganizationControl(f.ctx, reviewer, id, approve)
	if e != nil || p.ExpiresAt.After(expiry) {
		t.Fatal("city actual expiry lease", e)
	}
	ledger, e := f.pool.Begin(f.ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer ledger.Rollback(context.Background())
	if _, e = ledger.Exec(f.ctx, `LOCK TABLE organization_human_operations IN SHARE MODE`); e != nil {
		t.Fatal(e)
	}
	done := make(chan error, 1)
	go func() {
		_, err := f.store.ApplyOrganizationControl(f.ctx, reviewer, id, approve, p.Approval)
		done <- err
	}()
	wait := false
	end := time.Now().Add(3 * time.Second)
	for time.Now().Before(end) {
		var n int
		if e = f.pool.QueryRow(f.ctx, `SELECT count(*) FROM pg_stat_activity WHERE datname=current_database() AND wait_event_type='Lock' AND query LIKE 'INSERT INTO organization_human_operations%'`).Scan(&n); e != nil {
			t.Fatal(e)
		}
		if n > 0 {
			wait = true
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !wait {
		t.Fatal("actual original receipt wait missing")
	}
	time.Sleep(time.Until(expiry) + 20*time.Millisecond)
	if e = ledger.Commit(f.ctx); e != nil {
		t.Fatal(e)
	}
	select {
	case e = <-done:
		if !errors.Is(e, org.ErrControlChanged) {
			t.Fatal("expired city/confirmation committed during receipt wait", e)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("expired approval wait did not retire")
	}
	var revision int64
	var latitude float64
	if e = f.pool.QueryRow(f.ctx, `SELECT revision,latitude FROM organization_map_locations WHERE organization_id=$1`, id).Scan(&revision, &latitude); e != nil || revision != 8 || latitude != 57.21 {
		t.Fatal("expired transaction changed original approved point", e)
	}
	var count int
	if e = f.pool.QueryRow(f.ctx, `SELECT count(*) FROM organization_human_operations WHERE operation_id=$1`, approve.OperationID).Scan(&count); e != nil || count != 0 {
		t.Fatal("expired transaction published a success receipt", e)
	}
}
