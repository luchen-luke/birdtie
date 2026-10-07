package postgres

import (
	"context"
	"errors"
	"github.com/birdtie/birdtie/apps/api/internal/agentmemory"
	"github.com/birdtie/birdtie/apps/api/internal/agentorganizationevidence"
	"github.com/birdtie/birdtie/apps/api/internal/agentorganizationmemory"
	"github.com/birdtie/birdtie/apps/api/internal/organizationannouncement"
	"github.com/jackc/pgx/v5/pgxpool"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

func announcementPublicTestAccess(f *orgMemoryFixture, viewer string) organizationannouncement.PublicAccess {
	if viewer == "" {
		return organizationannouncement.PublicAccess{}
	}
	return organizationannouncement.PublicAccess{ViewerID: viewer, SessionDigest: f.access.SessionDigest}
}
func announcementFixture(t *testing.T) (*orgMemoryFixture, string, organizationannouncement.DraftInput) {
	t.Helper()
	f := newOrgMemoryFixture(t)
	b := f.p.base
	b.exec(`UPDATE organizations SET visibility='public',verification_status='verified',updated_at=clock_timestamp() WHERE id=$1`, f.org.ID)
	return f, agentMemoryID(t, f.p), organizationannouncement.DraftInput{Title: "合成独立公告", Body: "合成管理员发布声明，不是 CSSA 核验、广播或真实活动。", ValidUntil: time.Now().Add(time.Hour).UTC().Truncate(time.Microsecond)}
}
func announcementPublished(t *testing.T, f *orgMemoryFixture, id string, in organizationannouncement.DraftInput) organizationannouncement.Record {
	t.Helper()
	b := f.p.base
	r, e := b.store.PutOrganizationAnnouncement(b.ctx, f.access, id, in)
	if e != nil {
		t.Fatal("draft", e)
	}
	p, e := b.store.PreviewOrganizationAnnouncement(b.ctx, f.access, id, r.Revision)
	if e != nil {
		t.Fatal("preview", e)
	}
	r, e = b.store.PublishOrganizationAnnouncement(b.ctx, f.access, id, organizationannouncement.PublishInput{ExpectedRevision: r.Revision, PreviewID: p.ID})
	if e != nil {
		t.Fatal("publish", e)
	}
	return r
}
func TestOrganizationAnnouncementNativeLifecycleEvidenceAndRestart(t *testing.T) {
	f, id, in := announcementFixture(t)
	b := f.p.base
	r, e := b.store.PutOrganizationAnnouncement(b.ctx, f.access, id, in)
	if e != nil {
		t.Fatal(e)
	}
	repeat, e := b.store.PutOrganizationAnnouncement(b.ctx, f.access, id, in)
	if e != nil || repeat.Revision != 1 {
		t.Fatal("draft retry", e)
	}
	if _, e = b.store.ReadPublicOrganizationAnnouncement(b.ctx, f.org.ID, id, organizationannouncement.PublicAccess{}); !errors.Is(e, agentmemory.ErrNotFound) {
		t.Fatal("draft leaked", e)
	}
	p, e := b.store.PreviewOrganizationAnnouncement(b.ctx, f.access, id, 1)
	if e != nil || p.Announcement.Body != in.Body || p.Consequence != organizationannouncement.PublicationConsequence {
		t.Fatal("concrete preview", e)
	}
	pub := organizationannouncement.PublishInput{ExpectedRevision: 1, PreviewID: p.ID}
	r, e = b.store.PublishOrganizationAnnouncement(b.ctx, f.access, id, pub)
	if e != nil || r.Revision != 2 {
		t.Fatal(e)
	}
	for i := 0; i < 2; i++ {
		r, e = b.store.PublishOrganizationAnnouncement(b.ctx, f.access, id, pub)
		if e != nil || r.Revision != 2 {
			t.Fatal("publish repeat", e)
		}
	}
	public, e := New(b.pool, false).ReadPublicOrganizationAnnouncement(b.ctx, f.org.ID, id, organizationannouncement.PublicAccess{})
	if e != nil || public.Revision != 2 || public.Body != in.Body {
		t.Fatal("restart public", e)
	}
	list, e := New(b.pool, false).ListOrganizationAnnouncements(b.ctx, f.access)
	if e != nil || len(list) != 1 {
		t.Fatal("restart managed", e)
	}
	mid := agentMemoryID(t, f.p)
	if _, e = b.store.PutOrganizationMemory(b.ctx, f.access, mid, orgMemoryTestInput(agentmemory.OrgAnnouncement, "separate-note")); e != nil {
		t.Fatal(e)
	}
	eid := agentMemoryID(t, f.p)
	ev, e := b.store.PutOrganizationMemoryEvidence(b.ctx, f.access, mid, eid, agentmemory.EvidenceReferenceInput{ExpectedMemoryVersion: 1, SourceType: agentorganizationevidence.Announcement, SourceID: id})
	if e != nil || ev.Source.Version.Revision != 2 {
		t.Fatal("real fifth source", e)
	}
	prov, e := b.store.ReadOrganizationMemoryProvenance(b.ctx, f.access, mid)
	if e != nil || len(prov.Evidence) != 1 || !strings.Contains(prov.Explanation, "组织公告") {
		t.Fatal("fifth source provenance", e)
	}
	in.ExpectedRevision = 2
	in.Body = "合成修改版需要重新人工批准"
	r, e = b.store.PutOrganizationAnnouncement(b.ctx, f.access, id, in)
	if e != nil || r.Revision != 3 || r.State != organizationannouncement.Draft {
		t.Fatal("edit revokes", e)
	}
	if _, e = b.store.ReadPublicOrganizationAnnouncement(b.ctx, f.org.ID, id, organizationannouncement.PublicAccess{}); !errors.Is(e, agentmemory.ErrNotFound) {
		t.Fatal("edit public leaked", e)
	}
	prov, e = b.store.ReadOrganizationMemoryProvenance(b.ctx, f.access, mid)
	if e != nil || len(prov.Evidence) != 0 {
		t.Fatal("stale revision evidence", e)
	}
	var count int
	b.pool.QueryRow(b.ctx, `SELECT count(*) FROM organization_announcement_evidence_controls WHERE evidence_id=$1`, eid).Scan(&count)
	if count != 1 {
		t.Fatal("removed source history lost")
	}
	if _, e = b.store.PublishOrganizationAnnouncement(b.ctx, f.access, id, pub); !errors.Is(e, agentmemory.ErrConflict) {
		t.Fatal("old preview reactivated", e)
	}
	p, e = b.store.PreviewOrganizationAnnouncement(b.ctx, f.access, id, 3)
	if e != nil {
		t.Fatal(e)
	}
	r, e = b.store.PublishOrganizationAnnouncement(b.ctx, f.access, id, organizationannouncement.PublishInput{ExpectedRevision: 3, PreviewID: p.ID})
	if e != nil {
		t.Fatal(e)
	}
	r, e = b.store.WithdrawOrganizationAnnouncement(b.ctx, f.access, id, 4)
	if e != nil || r.Revision != 5 {
		t.Fatal(e)
	}
	r, e = b.store.WithdrawOrganizationAnnouncement(b.ctx, f.access, id, 4)
	if e != nil || r.Revision != 5 {
		t.Fatal("withdraw retry", e)
	}
	if _, e = b.store.ReadPublicOrganizationAnnouncement(b.ctx, f.org.ID, id, organizationannouncement.PublicAccess{}); !errors.Is(e, agentmemory.ErrNotFound) {
		t.Fatal("withdraw leak", e)
	}
	in.ExpectedRevision = 5
	if _, e = b.store.PutOrganizationAnnouncement(b.ctx, f.access, id, in); !errors.Is(e, agentmemory.ErrConflict) {
		t.Fatal("withdrawn revived", e)
	}
	var raw string
	if e = b.pool.QueryRow(b.ctx, `SELECT jsonb_agg(to_jsonb(a))::text FROM organization_announcement_audit a WHERE announcement_id=$1`, id).Scan(&raw); e != nil || strings.Contains(raw, in.Body) || strings.Contains(raw, "session") {
		t.Fatal("audit content leak", e)
	}
	if _, e = b.pool.Exec(b.ctx, `DELETE FROM organization_announcements WHERE id=$1`, id); e == nil {
		t.Fatal("history deleted")
	}
}
func TestOrganizationAnnouncementNativePermissionsAndPreviewAuthority(t *testing.T) {
	for _, name := range []string{"member", "moderator", "removed", "cross_actor", "org_entity_as_principal", "org_token", "revoked", "old_session", "role_changed_then_restored", "account_changed_then_restored", "profile_changed", "org_context_changed", "idle_refreshed", "cancelled"} {
		t.Run(name, func(t *testing.T) {
			f, id, in := announcementFixture(t)
			b := f.p.base
			if _, e := b.store.PutOrganizationAnnouncement(b.ctx, f.access, id, in); e != nil {
				t.Fatal(e)
			}
			p, e := b.store.PreviewOrganizationAnnouncement(b.ctx, f.access, id, 1)
			if e != nil {
				t.Fatal(e)
			}
			a := f.access
			ctx := b.ctx
			wantFailure := true
			switch name {
			case "member", "moderator":
				b.exec(`UPDATE organization_memberships SET role=$3,updated_at=clock_timestamp() WHERE organization_id=$1 AND user_account_id=$2`, f.org.ID, b.person.ID, name)
			case "removed":
				b.exec(`UPDATE organization_memberships SET status='removed',updated_at=clock_timestamp() WHERE organization_id=$1 AND user_account_id=$2`, f.org.ID, b.person.ID)
			case "cross_actor":
				a.ActingPersonID = b.other.ID
			case "org_entity_as_principal":
				a.OrganizationID = f.org.AccountID
			case "org_token":
				a.ActingPersonID = f.org.AccountID
			case "revoked":
				b.exec(`UPDATE sessions SET revoked_at=clock_timestamp() WHERE id=$1`, f.p.ownerSession)
			case "old_session":
				b.exec(`UPDATE sessions SET created_at=created_at-interval '1 second' WHERE id=$1`, f.p.ownerSession)
			case "role_changed_then_restored":
				b.exec(`UPDATE organization_memberships SET role='admin',updated_at=clock_timestamp() WHERE organization_id=$1 AND user_account_id=$2`, f.org.ID, b.person.ID)
				b.exec(`UPDATE organization_memberships SET role='owner',updated_at=clock_timestamp() WHERE organization_id=$1 AND user_account_id=$2`, f.org.ID, b.person.ID)
			case "account_changed_then_restored":
				b.exec(`UPDATE accounts SET status='suspended',updated_at=clock_timestamp() WHERE id=$1`, b.person.ID)
				b.exec(`UPDATE accounts SET status='active',updated_at=clock_timestamp() WHERE id=$1`, b.person.ID)
			case "profile_changed":
				b.exec(`UPDATE agent_profiles SET profile_version=profile_version+1 WHERE owner_id=$1`, f.org.AccountID)
			case "org_context_changed":
				b.exec(`UPDATE organizations SET name=name||' changed',updated_at=clock_timestamp() WHERE id=$1`, f.org.ID)
			case "idle_refreshed":
				b.exec(`UPDATE sessions SET idle_expires_at=clock_timestamp()+interval '45 minutes' WHERE id=$1`, f.p.ownerSession)
				wantFailure = false
			case "cancelled":
				cancelCtx, cancel := context.WithCancel(b.ctx)
				cancel()
				ctx = cancelCtx
			}
			got, e := b.store.PublishOrganizationAnnouncement(ctx, a, id, organizationannouncement.PublishInput{ExpectedRevision: 1, PreviewID: p.ID})
			if wantFailure && e == nil {
				t.Fatal("old or unauthorized approval accepted", got)
			}
			if !wantFailure && (e != nil || got.State != organizationannouncement.Published) {
				t.Fatal("ordinary authentication idle refresh invalidated approval", e)
			}
			if wantFailure {
				r, e := b.store.ReadOrganizationAnnouncement(b.ctx, f.access, id)
				if e == nil && r.State != organizationannouncement.Draft {
					t.Fatal("failed approval wrote")
				}
			}
		})
	}
}
func TestOrganizationAnnouncementNativePublicCurrentContextAndBlocks(t *testing.T) {
	for _, name := range []string{"private", "unverified", "suspended", "principal_suspended", "agent_suspended", "metadata_missing", "context_changed_and_restored", "block", "wrong_org"} {
		t.Run(name, func(t *testing.T) {
			f, id, in := announcementFixture(t)
			b := f.p.base
			announcementPublished(t, f, id, in)
			org := f.org.ID
			viewer := ""
			switch name {
			case "private":
				b.exec(`UPDATE organizations SET visibility='private' WHERE id=$1`, org)
			case "unverified":
				b.exec(`UPDATE organizations SET verification_status='unverified' WHERE id=$1`, org)
			case "suspended":
				b.exec(`UPDATE organizations SET status='suspended' WHERE id=$1`, org)
			case "principal_suspended":
				b.exec(`UPDATE accounts SET status='suspended' WHERE id=$1`, f.org.AccountID)
			case "agent_suspended":
				b.exec(`UPDATE agents SET status='suspended' WHERE principal_account_id=$1`, f.org.AccountID)
			case "metadata_missing":
				b.exec(`DELETE FROM agent_profiles WHERE owner_id=$1`, f.org.AccountID)
			case "context_changed_and_restored":
				b.exec(`UPDATE organizations SET name=name||' changed',updated_at=clock_timestamp() WHERE id=$1`, org)
				b.exec(`UPDATE organizations SET name=$2,updated_at=clock_timestamp() WHERE id=$1`, org, f.org.Name)
			case "block":
				viewer = b.person.ID
				b.exec(`INSERT INTO account_blocks(blocker_account_id,blocked_account_id) VALUES($1,$2)`, viewer, f.org.AccountID)
			case "wrong_org":
				org = b.orgID
			}
			if got, e := b.store.ReadPublicOrganizationAnnouncement(b.ctx, org, id, announcementPublicTestAccess(f, viewer)); !errors.Is(e, agentmemory.ErrNotFound) || got.ID != "" {
				t.Fatal("private/current source leaked", e)
			}
		})
	}
}
func TestOrganizationAnnouncementNativeNaturalExpiryAndConcurrency(t *testing.T) {
	f, id, in := announcementFixture(t)
	b := f.p.base
	in.ValidUntil = time.Now().Add(900 * time.Millisecond).UTC().Truncate(time.Microsecond)
	r := announcementPublished(t, f, id, in)
	for {
		var now time.Time
		b.pool.QueryRow(b.ctx, `SELECT clock_timestamp()`).Scan(&now)
		if !now.Before(in.ValidUntil) {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if _, e := b.store.ReadPublicOrganizationAnnouncement(b.ctx, f.org.ID, id, organizationannouncement.PublicAccess{}); !errors.Is(e, agentmemory.ErrNotFound) {
		t.Fatal("expiry leaked", e)
	}
	if _, e := b.store.ReadOrganizationAnnouncement(b.ctx, f.access, id); e != nil {
		t.Fatal("expired management inaccessible", e)
	}
	if _, e := b.store.WithdrawOrganizationAnnouncement(b.ctx, f.access, id, r.Revision); e != nil {
		t.Fatal("expired withdraw unavailable", e)
	}
	next := agentMemoryID(t, f.p)
	in.ValidUntil = time.Now().Add(time.Hour).UTC().Truncate(time.Microsecond)
	if _, e := b.store.PutOrganizationAnnouncement(b.ctx, f.access, next, in); e != nil {
		t.Fatal(e)
	}
	p, e := b.store.PreviewOrganizationAnnouncement(b.ctx, f.access, next, 1)
	if e != nil {
		t.Fatal(e)
	}
	var wg sync.WaitGroup
	errs := make(chan error, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, e := b.store.PublishOrganizationAnnouncement(b.ctx, f.access, next, organizationannouncement.PublishInput{ExpectedRevision: 1, PreviewID: p.ID})
			errs <- e
		}()
	}
	wg.Wait()
	close(errs)
	for e := range errs {
		if e != nil {
			t.Fatal("concurrent exact retry", e)
		}
	}
	var count int
	b.pool.QueryRow(b.ctx, `SELECT count(*) FROM organization_announcement_audit WHERE announcement_id=$1 AND action='PUBLISH'`, next).Scan(&count)
	if count != 1 {
		t.Fatal("duplicate publication audit")
	}
}
func TestOrganizationAnnouncementNativeDownRefusesHistory(t *testing.T) {
	f, id, in := announcementFixture(t)
	b := f.p.base
	announcementPublished(t, f, id, in)
	down, e := os.ReadFile("../../migrations/075_organization_announcements.down.sql")
	if e != nil {
		t.Fatal(e)
	}
	conn, e := b.pool.Acquire(b.ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer conn.Release()
	_, e = conn.Exec(b.ctx, string(down))
	_, rollback := conn.Exec(b.ctx, "ROLLBACK")
	if e == nil || !strings.Contains(e.Error(), "075 down refuses") || rollback != nil {
		t.Fatal("destructive down", e, rollback)
	}
	if _, e = b.store.ReadPublicOrganizationAnnouncement(b.ctx, f.org.ID, id, organizationannouncement.PublicAccess{}); e != nil {
		t.Fatal("failed down altered native", e)
	}
}

func TestOrganizationAnnouncementNativeExpiryDuringOrganizationWait(t *testing.T) {
	f, id, in := announcementFixture(t)
	b := f.p.base
	in.ValidUntil = time.Now().Add(900 * time.Millisecond).UTC().Truncate(time.Microsecond)
	if _, e := b.store.PutOrganizationAnnouncement(b.ctx, f.access, id, in); e != nil {
		t.Fatal(e)
	}
	p, e := b.store.PreviewOrganizationAnnouncement(b.ctx, f.access, id, 1)
	if e != nil {
		t.Fatal(e)
	}
	lock, e := b.pool.Begin(b.ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer lock.Rollback(context.Background())
	if _, e = lock.Exec(b.ctx, `SELECT id FROM organizations WHERE id=$1 FOR UPDATE`, f.org.ID); e != nil {
		t.Fatal(e)
	}
	done := make(chan error, 1)
	go func() {
		_, e := b.store.PublishOrganizationAnnouncement(b.ctx, f.access, id, organizationannouncement.PublishInput{ExpectedRevision: 1, PreviewID: p.ID})
		done <- e
	}()
	orgMemoryNativeWait(t, f, lock.Conn().PgConn().PID(), &p.ExpiresAt)
	if e = lock.Commit(b.ctx); e != nil {
		t.Fatal(e)
	}
	select {
	case e := <-done:
		if !errors.Is(e, agentmemory.ErrInvalid) {
			t.Fatal("expired preview after native lock", e)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("wait hung")
	}
	r, e := b.store.ReadOrganizationAnnouncement(b.ctx, f.access, id)
	if e != nil || r.State != organizationannouncement.Draft || r.Revision != 1 {
		t.Fatal("expired wait committed", e)
	}
}
func TestOrganizationAnnouncementNativeResourceBoundAndTombstoneControl(t *testing.T) {
	f, id, in := announcementFixture(t)
	b := f.p.base
	announcementPublished(t, f, id, in)
	mid := agentMemoryID(t, f.p)
	if _, e := b.store.PutOrganizationMemory(b.ctx, f.access, mid, orgMemoryTestInput(agentmemory.OrgAnnouncement, "control")); e != nil {
		t.Fatal(e)
	}
	eid := agentMemoryID(t, f.p)
	if _, e := b.store.PutOrganizationMemoryEvidence(b.ctx, f.access, mid, eid, agentmemory.EvidenceReferenceInput{ExpectedMemoryVersion: 1, SourceType: agentorganizationevidence.Announcement, SourceID: id}); e != nil {
		t.Fatal(e)
	}
	if _, e := b.store.RemoveOrganizationMemoryEvidence(b.ctx, f.access, mid, eid, 1); e != nil {
		t.Fatal(e)
	}
	if _, e := b.pool.Exec(b.ctx, `DELETE FROM organizations WHERE id=$1`, f.org.ID); e != nil {
		t.Fatal("true domain parent cascade", e)
	}
	var resources, controls int
	b.pool.QueryRow(b.ctx, `SELECT (SELECT count(*) FROM organization_announcements WHERE id=$1),(SELECT count(*) FROM organization_announcement_evidence_controls WHERE evidence_id=$2)`, id, eid).Scan(&resources, &controls)
	if resources != 0 || controls != 1 {
		t.Fatal("removed history was conflated with source entity")
	}
	down, e := os.ReadFile("../../migrations/075_organization_announcements.down.sql")
	if e != nil {
		t.Fatal(e)
	}
	conn, e := b.pool.Acquire(b.ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer conn.Release()
	_, e = conn.Exec(b.ctx, string(down))
	_, rollback := conn.Exec(b.ctx, "ROLLBACK")
	if e == nil || !strings.Contains(e.Error(), "075 down refuses") || rollback != nil {
		t.Fatal("REMOVED-source history destructively rolled back", e)
	}
}
func TestOrganizationAnnouncementNativeBoundedResources(t *testing.T) {
	f, _, in := announcementFixture(t)
	b := f.p.base
	for i := 0; i < organizationannouncement.MaxResources; i++ {
		id := agentMemoryID(t, f.p)
		if _, e := b.store.PutOrganizationAnnouncement(b.ctx, f.access, id, in); e != nil {
			t.Fatal("bounded create", i, e)
		}
	}
	if _, e := b.store.PutOrganizationAnnouncement(b.ctx, f.access, agentMemoryID(t, f.p), in); !errors.Is(e, agentorganizationmemory.ErrLimit) {
		t.Fatal("129th resource accepted", e)
	}
	all, e := b.store.ListOrganizationAnnouncements(b.ctx, f.access)
	if e != nil || len(all) != 128 {
		t.Fatal("bounded authoritative recovery", e)
	}
}

var _ agentorganizationmemory.Store = (*Store)(nil)

func TestOrganizationAnnouncementNativePublicTimezoneOldApproval(t *testing.T) {
	f, id, in := announcementFixture(t)
	b := f.p.base
	announcementPublished(t, f, id, in)
	var before string
	if e := b.pool.QueryRow(b.ctx, `SELECT publication_context::text FROM organization_announcements WHERE id=$1`, id).Scan(&before); e != nil {
		t.Fatal(e)
	}
	for _, zone := range []string{"Pacific/Honolulu", "Pacific/Auckland", "Europe/London"} {
		t.Run(zone, func(t *testing.T) {
			cfg := b.pool.Config()
			cfg.ConnConfig.RuntimeParams["timezone"] = zone
			cfg.MaxConns = 1
			cfg.MinConns = 0
			pool, e := pgxpool.NewWithConfig(b.ctx, cfg)
			if e != nil {
				t.Fatal(e)
			}
			defer pool.Close()
			var actual string
			if e = pool.QueryRow(b.ctx, `SHOW TIME ZONE`).Scan(&actual); e != nil || actual != zone {
				t.Fatal("not real non-UTC connection", e, actual)
			}
			store := New(pool, false)
			a := announcementPublicTestAccess(f, b.person.ID)
			first, e := store.ReadPublicOrganizationAnnouncement(b.ctx, f.org.ID, id, a)
			if e != nil || first.Revision != 2 || first.SessionSnapshot == "" {
				t.Fatal("old actual UTC human approval lost under new reader time zone", e)
			}
			a.ExpectedSessionSnapshot = first.SessionSnapshot
			final, e := store.ReadPublicOrganizationAnnouncement(b.ctx, f.org.ID, id, a)
			if e != nil || final.SessionSnapshot != first.SessionSnapshot {
				t.Fatal("time zone semantic session changed", e)
			}
		})
	}
	var after string
	if e := b.pool.QueryRow(b.ctx, `SELECT publication_context::text FROM organization_announcements WHERE id=$1`, id).Scan(&after); e != nil || before != after {
		t.Fatal("silently rewrote approved publication context", e)
	}
}
