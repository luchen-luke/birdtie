package postgres

import (
	"context"
	"errors"
	"fmt"
	"os"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/activitypublish"
	"github.com/birdtie/birdtie/apps/api/internal/agentevent"
	"github.com/birdtie/birdtie/apps/api/internal/agentmemory"
	"github.com/birdtie/birdtie/apps/api/internal/agentorganizationevidence"
	"github.com/birdtie/birdtie/apps/api/internal/agentorganizationmemory"
	"github.com/birdtie/birdtie/apps/api/internal/organization"
	"github.com/jackc/pgx/v5/pgconn"
)

type organizationEvidenceFixture struct {
	f                                      *orgMemoryFixture
	mid, admin, faq, activity, place, city string
	activityInput                          activitypublish.Input
}

func newOrganizationEvidenceFixture(t *testing.T) *organizationEvidenceFixture {
	t.Helper()
	f := newOrgMemoryFixture(t)
	b := f.p.base
	r := &organizationEvidenceFixture{f: f, mid: agentMemoryID(t, f.p), admin: agentMemoryID(t, f.p)}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		for _, item := range []struct {
			sql string
			arg any
		}{{`DELETE FROM admin_audit_events WHERE actor_account_id=ANY($1::uuid[])`, b.accounts}, {`DELETE FROM activities WHERE id=$1`, r.activity}, {`DELETE FROM places WHERE id=$1`, r.place}, {`DELETE FROM cities WHERE id=$1`, r.city}, {`DELETE FROM organization_faqs WHERE id=$1`, r.faq}} {
			if id, ok := item.arg.(string); ok && id == "" {
				continue
			}
			if _, e := b.pool.Exec(ctx, item.sql, item.arg); e != nil {
				t.Error("owned evidence source cleanup", e)
			}
		}
	})
	// Explicitly disposable verification state only, not a real organization verification.
	b.exec(`UPDATE organizations SET visibility='public',verification_status='verified' WHERE id=$1`, f.org.ID)
	for _, id := range []string{r.mid, r.admin} {
		if _, e := b.store.PutOrganizationMemory(b.ctx, f.access, id, orgMemoryTestInput(agentmemory.OrgFAQ, id)); e != nil {
			t.Fatal(e)
		}
	}
	faq, e := b.store.CreateFAQ(b.ctx, b.person.ID, f.org.ID, organization.FAQInput{Question: "合成来源问答 " + f.org.ID, Answer: "合成私密正文不得复制到 evidence", Published: true})
	if e != nil {
		t.Fatal(e)
	}
	r.faq = faq.ID
	r.city = "age053-" + f.org.ID
	if _, e = b.pool.Exec(b.ctx, `INSERT INTO cities(id,name,region,country_code,time_zone,publication_status,source_label,source_ref,maintainer_label) SELECT $1,name,region,country_code,time_zone,publication_status,source_label,source_ref,maintainer_label FROM cities WHERE id='aberdeen-gb'`, r.city); e != nil {
		t.Fatal(e)
	}
	if e = b.pool.QueryRow(b.ctx, `INSERT INTO places(id,city_id,name,category_code,publication_status,source_label,source_ref,maintainer_label) VALUES(gen_random_uuid(),$1,'合成 AGE053 地点','synthetic-source','published','合成验收','disposable://organization-evidence','合成验收') RETURNING id`, r.city).Scan(&r.place); e != nil {
		t.Fatal(e)
	}
	r.activityInput = activitypublish.Input{CityID: r.city, PlaceID: r.place, Title: "合成 AGE053 组织活动", Summary: "合成不是真实到场", StartsAt: time.Now().Add(time.Hour), EndsAt: time.Now().Add(2 * time.Hour), TimeZone: "Europe/London", CategoryCode: "synthetic-source", Visibility: "public"}
	a, e := b.store.CreateDraft(b.ctx, b.person.ID, f.org.ID, r.activityInput)
	if e != nil {
		t.Fatal(e)
	}
	r.activity = a.ID
	if _, e = b.store.PublishActivity(b.ctx, b.person.ID, f.org.ID, a.ID); e != nil {
		t.Fatal(e)
	}
	return r
}
func (r *organizationEvidenceFixture) source(k agentevent.SourceType) string {
	switch k {
	case agentorganizationevidence.Profile:
		return r.f.org.ID
	case agentorganizationevidence.Activity:
		return r.activity
	case agentorganizationevidence.PublicContent:
		return r.faq
	default:
		return r.admin
	}
}
func (r *organizationEvidenceFixture) put(t *testing.T, k agentevent.SourceType) agentmemory.Evidence {
	t.Helper()
	f := r.f
	e, err := f.p.base.store.PutOrganizationMemoryEvidence(f.p.base.ctx, f.access, r.mid, agentMemoryID(t, f.p), agentmemory.EvidenceReferenceInput{ExpectedMemoryVersion: 1, SourceType: k, SourceID: r.source(k)})
	if err != nil {
		t.Fatal(err)
	}
	return e
}
func orgEvidenceRows(t *testing.T, r *organizationEvidenceFixture) string {
	t.Helper()
	var raw string
	b := r.f.p.base
	if e := b.pool.QueryRow(b.ctx, `SELECT coalesce(jsonb_agg(to_jsonb(e) ORDER BY id),'[]')::text FROM agent_memory_evidence e WHERE memory_id=$1`, r.mid).Scan(&raw); e != nil {
		t.Fatal(e)
	}
	return raw
}
func TestOrganizationEvidenceNativeAllSourcesCASAndTombstone(t *testing.T) {
	r := newOrganizationEvidenceFixture(t)
	f := r.f
	b := f.p.base
	for _, k := range []agentevent.SourceType{agentorganizationevidence.Profile, agentorganizationevidence.Activity, agentorganizationevidence.AdminInput, agentorganizationevidence.PublicContent} {
		t.Run(string(k), func(t *testing.T) {
			first := r.put(t, k)
			if first.OwnerID != f.org.AccountID || first.OwnerType != "ORGANIZATION" || first.Source.Owner.ID != f.org.AccountID || agentorganizationevidence.ValidateEvidence(first) != nil {
				t.Fatal("wrong native principal")
			}
			before := orgEvidenceRows(t, r)
			retry, e := b.store.PutOrganizationMemoryEvidence(b.ctx, f.access, r.mid, first.ID, agentmemory.EvidenceReferenceInput{ExpectedMemoryVersion: 1, SourceType: k, SourceID: r.source(k)})
			if e != nil || !reflect.DeepEqual(first, retry) || orgEvidenceRows(t, r) != before {
				t.Fatal("retry changed evidence", e)
			}
			var record agentmemory.Evidence
			record, e = b.store.PutOrganizationMemoryEvidence(b.ctx, f.access, r.mid, agentMemoryID(t, f.p), agentmemory.EvidenceReferenceInput{ExpectedMemoryVersion: 1, SourceType: k, SourceID: r.source(k)})
			if !errors.Is(e, agentmemory.ErrConflict) || record.ID != "" {
				t.Fatal("duplicate source allowed", e)
			}
			removed, e := b.store.RemoveOrganizationMemoryEvidence(b.ctx, f.access, r.mid, first.ID, 1)
			if e != nil || removed.Status != agentmemory.EvidenceRemoved || removed.Version != 2 || removed.Source != nil {
				t.Fatal("tombstone", e)
			}
			retry, e = b.store.RemoveOrganizationMemoryEvidence(b.ctx, f.access, r.mid, first.ID, 1)
			if e != nil || !reflect.DeepEqual(removed, retry) {
				t.Fatal("delete retry", e)
			}
			if _, e = b.store.PutOrganizationMemoryEvidence(b.ctx, f.access, r.mid, first.ID, agentmemory.EvidenceReferenceInput{ExpectedMemoryVersion: 1, SourceType: k, SourceID: r.source(k)}); !errors.Is(e, agentmemory.ErrConflict) {
				t.Fatal("revived tombstone", e)
			}
		})
	}
	p, e := b.store.ReadOrganizationMemoryProvenance(b.ctx, f.access, r.mid)
	if e != nil || len(p.Evidence) != 0 || p.OrganizationAccountID != f.org.AccountID || p.Declaration != agentorganizationevidence.Declaration {
		t.Fatal("empty truthful provenance", e)
	}
}
func TestOrganizationEvidenceNativeSourceVersionChangeAndWithdrawal(t *testing.T) {
	cases := []string{"profile_edit", "profile_hidden", "profile_unverified", "faq_edit", "faq_unpublish", "faq_delete", "activity_edit", "activity_cancel", "activity_expiry", "activity_city_expiry", "admin_edit", "admin_delete", "admin_expiry", "source_block"}
	for _, change := range cases {
		t.Run(change, func(t *testing.T) {
			r := newOrganizationEvidenceFixture(t)
			f := r.f
			b := f.p.base
			k := agentorganizationevidence.Profile
			switch {
			case strings.HasPrefix(change, "faq"):
				k = agentorganizationevidence.PublicContent
			case strings.HasPrefix(change, "activity"):
				k = agentorganizationevidence.Activity
			case strings.HasPrefix(change, "admin"):
				k = agentorganizationevidence.AdminInput
			}
			old := r.put(t, k)
			switch change {
			case "profile_edit":
				if _, e := b.store.UpdateProfile(b.ctx, b.person.ID, f.org.ID, organization.ProfileInput{Name: "合成新版", Description: "更改的源正文", OfficialLinks: []string{}}); e != nil {
					t.Fatal(e)
				}
			case "profile_hidden":
				b.exec(`UPDATE organizations SET visibility='private' WHERE id=$1`, f.org.ID)
			case "profile_unverified":
				b.exec(`UPDATE organizations SET verification_status='unverified' WHERE id=$1`, f.org.ID)
			case "faq_edit", "faq_unpublish":
				if _, e := b.store.UpdateFAQ(b.ctx, b.person.ID, f.org.ID, r.faq, organization.FAQInput{Question: "合成新版 " + f.org.ID, Answer: "新版正文", Published: change == "faq_edit"}); e != nil {
					t.Fatal(e)
				}
			case "faq_delete":
				if e := b.store.DeleteFAQ(b.ctx, b.person.ID, f.org.ID, r.faq); e != nil {
					t.Fatal(e)
				}
			case "activity_edit":
				r.activityInput.Title = "合成新版活动"
				if _, e := b.store.UpdateActivity(b.ctx, b.person.ID, f.org.ID, r.activity, r.activityInput); e != nil {
					t.Fatal(e)
				}
			case "activity_cancel":
				if _, e := b.store.CancelActivity(b.ctx, b.person.ID, f.org.ID, r.activity); e != nil {
					t.Fatal(e)
				}
			case "activity_expiry":
				b.exec(`UPDATE activities SET expires_at=clock_timestamp()-interval '1 second' WHERE id=$1`, r.activity)
			case "activity_city_expiry":
				b.exec(`UPDATE cities SET expires_at=clock_timestamp()-interval '1 second' WHERE id=$1`, r.city)
			case "admin_edit":
				in := orgMemoryTestInput(agentmemory.OrgFAQ, r.admin)
				in.ExpectedVersion = 1
				in.Summary = "合成新版管理员源"
				if _, e := b.store.PutOrganizationMemory(b.ctx, f.access, r.admin, in); e != nil {
					t.Fatal(e)
				}
			case "admin_delete":
				if _, e := b.store.DeleteOrganizationMemory(b.ctx, f.access, r.admin, 1); e != nil {
					t.Fatal(e)
				}
			case "admin_expiry":
				b.exec(`UPDATE agent_memories SET version=version+1,valid_from=clock_timestamp()-interval '2 hours',valid_until=clock_timestamp()-interval '1 hour',updated_at=clock_timestamp() WHERE id=$1`, r.admin)
			case "source_block":
				b.exec(`INSERT INTO account_blocks(blocker_account_id,blocked_account_id) VALUES($1,$2)`, b.person.ID, f.org.AccountID)
			}
			p, e := b.store.ReadOrganizationMemoryProvenance(b.ctx, f.access, r.mid)
			if e != nil || len(p.Evidence) != 0 {
				t.Fatal("stale source released", change, e)
			}
			var removed agentmemory.Evidence
			removed, e = scanOrganizationEvidence(b.pool.QueryRow(b.ctx, `SELECT `+memoryEvidenceColumns+` FROM agent_memory_evidence WHERE id=$1`, old.ID))
			if e != nil || removed.Status != agentmemory.EvidenceRemoved || removed.Source != nil {
				t.Fatal("withdrawn address retained", e)
			}
			if _, e = b.store.PutOrganizationMemoryEvidence(b.ctx, f.access, r.mid, old.ID, agentmemory.EvidenceReferenceInput{ExpectedMemoryVersion: 1, SourceType: k, SourceID: r.source(k)}); e == nil {
				t.Fatal("stale Evidence revived")
			}
		})
	}
}
func TestOrganizationEvidenceNativeBoundaryAndCrossSubjects(t *testing.T) {
	for _, kind := range []string{"anonymous", "wrong_actor", "nonmember", "member", "moderator", "removed", "org_token", "business_token", "wrong_org_entity", "memory_other_org", "personal_memory", "source_other_org", "self", "announcement", "unknown", "expected_memory_version", "agent_suspended", "metadata_missing", "session_revoked"} {
		t.Run(kind, func(t *testing.T) {
			r := newOrganizationEvidenceFixture(t)
			f := r.f
			b := f.p.base
			a := f.access
			mid := r.mid
			input := agentmemory.EvidenceReferenceInput{ExpectedMemoryVersion: 1, SourceType: agentorganizationevidence.AdminInput, SourceID: r.admin}
			expected := agentmemory.ErrForbidden
			switch kind {
			case "anonymous":
				a.SessionDigest = [32]byte{}
			case "wrong_actor":
				a.ActingPersonID = b.other.ID
			case "nonmember":
				a.ActingPersonID = b.other.ID
				a.SessionDigest = f.p.peer.SessionDigest
			case "member", "moderator", "removed":
				b.exec(`UPDATE organization_memberships SET role=$3,status=$4 WHERE organization_id=$1 AND user_account_id=$2`, f.org.ID, b.person.ID, map[string]string{"member": "member", "moderator": "moderator", "removed": "owner"}[kind], map[string]string{"member": "active", "moderator": "active", "removed": "removed"}[kind])
			case "org_token":
				a.SessionDigest = f.p.org.SessionDigest
				a.ActingPersonID = b.org.ID
			case "business_token":
				a.SessionDigest = f.p.biz.SessionDigest
				a.ActingPersonID = b.business.ID
			case "wrong_org_entity":
				a.OrganizationID = f.org.AccountID
			case "memory_other_org":
				mid = agentMemoryID(t, f.p)
				expected = agentmemory.ErrNotFound
			case "personal_memory":
				mid = agentMemoryID(t, f.p)
				if _, e := b.store.PutOwnMemory(b.ctx, f.p.owner, mid, agentMemoryInput("cross-evidence")); e != nil {
					t.Fatal(e)
				}
				expected = agentmemory.ErrNotFound
			case "source_other_org":
				input.SourceType = agentorganizationevidence.Profile
				input.SourceID = b.orgID
			case "self":
				input.SourceID = r.mid
				expected = agentmemory.ErrInvalid
			case "announcement":
				input.SourceType = agentorganizationevidence.Announcement
				expected = agentmemory.ErrForbidden
			case "unknown":
				input.SourceType = "PRIVATE_URL"
				expected = agentmemory.ErrInvalid
			case "expected_memory_version":
				input.ExpectedMemoryVersion = 2
				expected = agentmemory.ErrConflict
			case "agent_suspended":
				b.exec(`UPDATE agents SET status='suspended' WHERE principal_account_id=$1`, f.org.AccountID)
			case "metadata_missing":
				b.exec(`DELETE FROM agent_profiles WHERE owner_id=$1`, f.org.AccountID)
			case "session_revoked":
				b.exec(`UPDATE sessions SET revoked_at=clock_timestamp() WHERE token_sha256=$1`, a.SessionDigest[:])
			}
			before := orgEvidenceRows(t, r)
			rec, e := b.store.PutOrganizationMemoryEvidence(b.ctx, a, mid, agentMemoryID(t, f.p), input)
			if !errors.Is(e, expected) || rec.ID != "" || orgEvidenceRows(t, r) != before {
				t.Fatal("boundary write accepted", kind, e, expected)
			}
		})
	}
}
func TestOrganizationEvidenceNativeAdminRoleAndReconnection(t *testing.T) {
	r := newOrganizationEvidenceFixture(t)
	f := r.f
	b := f.p.base
	inv, e := b.store.InviteMember(b.ctx, b.person.ID, f.org.ID, b.other.ID, "member")
	if e != nil {
		t.Fatal(e)
	}
	if _, e = b.store.AcceptInvitation(b.ctx, b.other.ID, inv.ID); e != nil {
		t.Fatal(e)
	}
	if _, e = b.store.ChangeMemberRole(b.ctx, b.person.ID, f.org.ID, inv.ID, "admin"); e != nil {
		t.Fatal(e)
	}
	a := agentorganizationmemory.Access{SessionDigest: f.p.peer.SessionDigest, ActingPersonID: b.other.ID, OrganizationID: f.org.ID}
	id := agentMemoryID(t, f.p)
	rec, e := b.store.PutOrganizationMemoryEvidence(b.ctx, a, r.mid, id, agentmemory.EvidenceReferenceInput{ExpectedMemoryVersion: 1, SourceType: agentorganizationevidence.AdminInput, SourceID: r.admin})
	if e != nil {
		t.Fatal(e)
	}
	reopened := New(b.pool, false)
	p, e := reopened.ReadOrganizationMemoryProvenance(b.ctx, a, r.mid)
	if e != nil || len(p.Evidence) != 1 || !reflect.DeepEqual(p.Evidence[0], rec) {
		t.Fatal("persisted evidence reconnect", e)
	}
	if e = b.store.RevokeMember(b.ctx, b.person.ID, f.org.ID, inv.ID); e != nil {
		t.Fatal(e)
	}
	if _, e = reopened.ReadOrganizationMemoryProvenance(b.ctx, a, r.mid); !errors.Is(e, agentmemory.ErrForbidden) {
		t.Fatal("revoked admin read", e)
	}
}
func TestOrganizationEvidenceNativeMemoryVersionInvalidation(t *testing.T) {
	for _, op := range []string{"edit", "delete"} {
		t.Run(op, func(t *testing.T) {
			r := newOrganizationEvidenceFixture(t)
			f := r.f
			b := f.p.base
			old := r.put(t, agentorganizationevidence.AdminInput)
			if op == "edit" {
				in := orgMemoryTestInput(agentmemory.OrgFAQ, r.mid)
				in.ExpectedVersion = 1
				in.Summary = "合成新版目标声明"
				if _, e := b.store.PutOrganizationMemory(b.ctx, f.access, r.mid, in); e != nil {
					t.Fatal(e)
				}
			} else {
				if _, e := b.store.DeleteOrganizationMemory(b.ctx, f.access, r.mid, 1); e != nil {
					t.Fatal(e)
				}
			}
			var status string
			var version int64
			var source *string
			if e := b.pool.QueryRow(b.ctx, `SELECT status,version,source_type FROM agent_memory_evidence WHERE id=$1`, old.ID).Scan(&status, &version, &source); e != nil || status != "REMOVED" || version != 2 || source != nil {
				t.Fatal("same transaction invalidation", e)
			}
			if op == "edit" {
				p, e := b.store.ReadOrganizationMemoryProvenance(b.ctx, f.access, r.mid)
				if e != nil || p.MemoryVersion != 2 || len(p.Evidence) != 0 {
					t.Fatal(e)
				}
			} else {
				if _, e := b.store.ReadOrganizationMemoryProvenance(b.ctx, f.access, r.mid); !errors.Is(e, agentmemory.ErrNotFound) {
					t.Fatal(e)
				}
			}
		})
	}
}
func TestOrganizationEvidenceNativeConcurrentExactRetry(t *testing.T) {
	r := newOrganizationEvidenceFixture(t)
	f := r.f
	b := f.p.base
	id := agentMemoryID(t, f.p)
	var wg sync.WaitGroup
	errs := make(chan error, 6)
	for i := 0; i < 6; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, e := b.store.PutOrganizationMemoryEvidence(b.ctx, f.access, r.mid, id, agentmemory.EvidenceReferenceInput{ExpectedMemoryVersion: 1, SourceType: agentorganizationevidence.AdminInput, SourceID: r.admin})
			errs <- e
		}()
	}
	wg.Wait()
	close(errs)
	for e := range errs {
		if e != nil {
			t.Fatal(e)
		}
	}
	var count int
	if e := b.pool.QueryRow(b.ctx, `SELECT count(*) FROM agent_memory_evidence WHERE memory_id=$1`, r.mid).Scan(&count); e != nil || count != 1 {
		t.Fatal("concurrent retry duplicated", e)
	}
}
func TestOrganizationEvidenceNativeActualWaitExpiryAndCancellation(t *testing.T) {
	for _, op := range []string{"put", "read", "delete"} {
		for _, change := range []string{"absolute", "idle", "target_expiry", "cancel"} {
			t.Run(op+"_"+change, func(t *testing.T) {
				r := newOrganizationEvidenceFixture(t)
				f := r.f
				b := f.p.base
				rec := r.put(t, agentorganizationevidence.AdminInput)
				lock, e := b.pool.Begin(b.ctx)
				if e != nil {
					t.Fatal(e)
				}
				defer lock.Rollback(context.Background())
				if _, e = lock.Exec(b.ctx, `SELECT id FROM agent_memories WHERE id=$1 FOR UPDATE`, r.mid); e != nil {
					t.Fatal(e)
				}
				var deadline *time.Time
				if change != "cancel" {
					var at time.Time
					q := `WITH stamp AS MATERIALIZED(SELECT clock_timestamp() at) UPDATE sessions SET created_at=stamp.at-interval '1 hour',idle_expires_at=stamp.at+interval '500 milliseconds',expires_at=stamp.at+interval '1 hour' FROM stamp WHERE token_sha256=$1 RETURNING idle_expires_at`
					arg := any(f.access.SessionDigest[:])
					if change == "absolute" {
						q = strings.Replace(q, "expires_at=stamp.at+interval '1 hour'", "expires_at=stamp.at+interval '500 milliseconds'", 1)
					}
					if change == "target_expiry" {
						q = `WITH stamp AS MATERIALIZED(SELECT clock_timestamp() at) UPDATE agent_memories SET version=version+1,valid_until=stamp.at+interval '500 milliseconds',updated_at=stamp.at FROM stamp WHERE id=$1 RETURNING valid_until`
						arg = r.mid
					}
					target := b.pool
					if change == "target_expiry" {
						if e = lock.QueryRow(b.ctx, q, arg).Scan(&at); e != nil {
							t.Fatal(e)
						}
					} else {
						if e = target.QueryRow(b.ctx, q, arg).Scan(&at); e != nil {
							t.Fatal(e)
						}
					}
					deadline = &at
				}
				before := orgEvidenceRows(t, r)
				ctx, cancel := context.WithCancel(b.ctx)
				defer cancel()
				done := make(chan error, 1)
				go func() {
					var e error
					switch op {
					case "put":
						_, e = b.store.PutOrganizationMemoryEvidence(ctx, f.access, r.mid, agentMemoryID(t, f.p), agentmemory.EvidenceReferenceInput{ExpectedMemoryVersion: 1, SourceType: agentorganizationevidence.AdminInput, SourceID: r.admin})
					case "read":
						_, e = b.store.ReadOrganizationMemoryProvenance(ctx, f.access, r.mid)
					case "delete":
						_, e = b.store.RemoveOrganizationMemoryEvidence(ctx, f.access, r.mid, rec.ID, 1)
					}
					done <- e
				}()
				orgMemoryNativeWait(t, f, lock.Conn().PgConn().PID(), deadline)
				if change == "cancel" {
					cancel()
				}
				if e = lock.Commit(b.ctx); e != nil {
					t.Fatal(e)
				}
				select {
				case e := <-done:
					if change == "target_expiry" && op == "delete" {
						if e != nil && !errors.Is(e, agentmemory.ErrConflict) {
							t.Fatal("expired deletion must remain manageable", e)
						}
					} else if e == nil {
						t.Fatal("wait accepted invalid current boundary")
					}
				case <-time.After(5 * time.Second):
					t.Fatal("wait never completed")
				}
				if change != "target_expiry" && before != orgEvidenceRows(t, r) {
					t.Fatal("failed wait wrote evidence")
				}
			})
		}
	}
}
func TestOrganizationEvidenceNativeFAQWriterNoInverseLock(t *testing.T) {
	r := newOrganizationEvidenceFixture(t)
	f := r.f
	b := f.p.base
	old := r.put(t, agentorganizationevidence.PublicContent)
	// Actual native writer runs while its source row is held. Evidence must not
	// take that source row after the Organization lock (which audit FK needs).
	lock, e := b.pool.Begin(b.ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer lock.Rollback(context.Background())
	if _, e = lock.Exec(b.ctx, `SELECT id FROM organization_faqs WHERE id=$1 FOR UPDATE`, r.faq); e != nil {
		t.Fatal(e)
	}
	writer := make(chan error, 1)
	go func() {
		_, e := b.store.UpdateFAQ(b.ctx, b.person.ID, f.org.ID, r.faq, organization.FAQInput{Question: "合成并发新版 " + f.org.ID, Answer: "新版", Published: true})
		writer <- e
	}()
	orgMemoryNativeWait(t, f, lock.Conn().PgConn().PID(), nil)
	read := make(chan error, 1)
	go func() { _, e := b.store.ReadOrganizationMemoryProvenance(b.ctx, f.access, r.mid); read <- e }()
	select {
	case e := <-read:
		if e != nil {
			t.Fatal("read blocked on source row", e)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("inverse Organization/source lock")
	}
	if e = lock.Commit(b.ctx); e != nil {
		t.Fatal(e)
	}
	if e = <-writer; e != nil {
		t.Fatal(e)
	}
	p, e := b.store.ReadOrganizationMemoryProvenance(b.ctx, f.access, r.mid)
	if e != nil || len(p.Evidence) != 0 {
		t.Fatal("committed native writer not revalidated", e)
	}
	var status string
	if e = b.pool.QueryRow(b.ctx, `SELECT status FROM agent_memory_evidence WHERE id=$1`, old.ID).Scan(&status); e != nil || status != "REMOVED" {
		t.Fatal(fmt.Sprint(status), e)
	}
}

func TestOrganizationEvidenceNativeDownRefusesCurrentAndRemovedHistory(t *testing.T) {
	r := newOrganizationEvidenceFixture(t)
	f := r.f
	b := f.p.base
	rec := r.put(t, agentorganizationevidence.AdminInput)
	raw, e := os.ReadFile("../../migrations/073_organization_memory_evidence.down.sql")
	if e != nil {
		t.Fatal(e)
	}
	for _, state := range []string{"CURRENT", "REMOVED"} {
		t.Run(state, func(t *testing.T) {
			if state == "REMOVED" {
				if _, e := b.store.RemoveOrganizationMemoryEvidence(b.ctx, f.access, r.mid, rec.ID, 1); e != nil {
					t.Fatal(e)
				}
			}
			before := orgEvidenceRows(t, r)
			var shape string
			if e := b.pool.QueryRow(b.ctx, `SELECT pg_get_constraintdef(oid) FROM pg_constraint WHERE conrelid='agent_memory_evidence'::regclass AND conname='memory_evidence_shape'`).Scan(&shape); e != nil {
				t.Fatal(e)
			}
			conn, e := b.pool.Acquire(b.ctx)
			if e != nil {
				t.Fatal(e)
			}
			_, e = conn.Exec(b.ctx, string(raw))
			_, rollback := conn.Exec(b.ctx, `ROLLBACK`)
			conn.Release()
			if e == nil || !strings.Contains(e.Error(), "073 down refuses Organization Evidence") || rollback != nil {
				t.Fatal("down erased organization history", e, rollback)
			}
			var after string
			if e := b.pool.QueryRow(b.ctx, `SELECT pg_get_constraintdef(oid) FROM pg_constraint WHERE conrelid='agent_memory_evidence'::regclass AND conname='memory_evidence_shape'`).Scan(&after); e != nil || after != shape || before != orgEvidenceRows(t, r) {
				t.Fatal("failed down not atomic", e)
			}
		})
	}
}
func TestOrganizationEvidenceNativePersonShapeAndOldPathUnchanged(t *testing.T) {
	f := memoryEvidenceNativeFixture(t)
	b := f.native.base.private.base
	for _, kind := range []agentevent.SourceType{agentevent.MomentSource, agentevent.ParticipationSource, agentevent.SavedPlaceSource} {
		rec := f.attach(t, kind)
		if agentmemory.ValidateEvidence(rec) != nil || agentorganizationevidence.ValidateEvidence(rec) == nil {
			t.Fatal("owner validator drift")
		}
	}
	p := f.provenance(t)
	if len(p.Evidence) != 3 {
		t.Fatal("old personal sources lost")
	}
	for _, bad := range []string{"ORGANIZATION_PROFILE", "ORGANIZATION_ACTIVITY", "ORGANIZATION_ADMIN_INPUT", "ORGANIZATION_PUBLIC_FAQ", "ORGANIZATION_ANNOUNCEMENT"} {
		t.Run(bad, func(t *testing.T) {
			id := agentMemoryID(t, f.native.base.private)
			_, e := b.pool.Exec(b.ctx, `INSERT INTO agent_memory_evidence(id,memory_id,memory_version,agent_id,owner_type,owner_id,source_type,source_id,source_version_kind,source_revision,signal_type,weight,event_time) VALUES($1,$2,1,$3,'PERSON',$4,$5,$1,'REVISION',1,'MANUAL_REFERENCE',1,$6)`, id, f.memory.ID, f.memory.AgentID, f.memory.OwnerID, bad, f.memory.CreatedAt)
			var pgErr *pgconn.PgError
			if e == nil || !errors.As(e, &pgErr) || pgErr.ConstraintName != "memory_evidence_shape" {
				t.Fatal("PERSON admitted Organization shape")
			}
		})
	}
}

func TestOrganizationEvidenceNativeOrganizationShapeRejectsNullAndForeignKinds(t *testing.T) {
	r := newOrganizationEvidenceFixture(t)
	f := r.f
	b := f.p.base
	var agent string
	var event time.Time
	if e := b.pool.QueryRow(b.ctx, `SELECT agent_id,created_at FROM agent_memories WHERE id=$1`, r.mid).Scan(&agent, &event); e != nil {
		t.Fatal(e)
	}
	for _, name := range []string{"moment", "participation", "saved", "announcement", "unknown", "null_kind", "null_revision", "zero_revision", "digest_revision", "digest_null_token", "digest_bad_token", "revision_token", "null_signal", "wrong_signal", "wrong_weight", "null_source", "self"} {
		t.Run(name, func(t *testing.T) {
			var kind any = string(agentorganizationevidence.AdminInput)
			var source any = r.admin
			var vkind any = "REVISION"
			var revision any = int64(1)
			var token any
			var signal any = "MANUAL_REFERENCE"
			weight := float64(1)
			switch name {
			case "moment":
				kind = "MOMENT"
			case "participation":
				kind = "ACTIVITY_PARTICIPATION"
			case "saved":
				kind = "SAVED_PLACE"
			case "announcement":
				kind = string(agentorganizationevidence.Announcement)
				revision = int64(0)
			case "unknown":
				kind = "UNKNOWN"
			case "null_kind":
				kind = nil
			case "null_revision":
				revision = nil
			case "zero_revision":
				revision = int64(0)
			case "digest_revision":
				kind = string(agentorganizationevidence.Profile)
				vkind = "UPDATED_AT_DIGEST"
				token = strings.Repeat("a", 64)
			case "digest_null_token":
				kind = string(agentorganizationevidence.Profile)
				vkind = "UPDATED_AT_DIGEST"
				revision = nil
			case "digest_bad_token":
				kind = string(agentorganizationevidence.Profile)
				vkind = "UPDATED_AT_DIGEST"
				revision = nil
				token = strings.Repeat("A", 64)
			case "revision_token":
				token = strings.Repeat("a", 64)
			case "null_signal":
				signal = nil
			case "wrong_signal":
				signal = "VERIFIED"
			case "wrong_weight":
				weight = .9
			case "null_source":
				source = nil
			case "self":
				source = r.mid
			}
			_, e := b.pool.Exec(b.ctx, `INSERT INTO agent_memory_evidence(id,memory_id,memory_version,agent_id,owner_type,owner_id,source_type,source_id,source_version_kind,source_revision,source_token,signal_type,weight,event_time) VALUES($1,$2,1,$3,'ORGANIZATION',$4,$5,$6,$7,$8,$9,$10,$11,$12)`, agentMemoryID(t, f.p), r.mid, agent, f.org.AccountID, kind, source, vkind, revision, token, signal, weight, event)
			var pgErr *pgconn.PgError
			if e == nil || !errors.As(e, &pgErr) || pgErr.ConstraintName != "memory_evidence_shape" {
				t.Fatal("organization shape not rejected by owner-specific constraint", e)
			}
		})
	}
}
func TestOrganizationEvidenceNativeActualCrossOrganizationMemoryAndSource(t *testing.T) {
	r := newOrganizationEvidenceFixture(t)
	f := r.f
	b := f.p.base
	other, e := b.store.CreateOrganization(b.ctx, b.person.ID, organization.CreateInput{OrganizationType: "club", Name: "合成另一个真实原生组织实体"})
	if e != nil {
		t.Fatal(e)
	}
	b.accounts = append(b.accounts, other.AccountID)
	a := f.access
	a.OrganizationID = other.ID
	otherMemory := agentMemoryID(t, f.p)
	if _, e = b.store.PutOrganizationMemory(b.ctx, a, otherMemory, orgMemoryTestInput(agentmemory.OrgPolicy, "other")); e != nil {
		t.Fatal(e)
	}
	for _, name := range []string{"target", "source", "principal_is_entity"} {
		t.Run(name, func(t *testing.T) {
			access := f.access
			mid, source := r.mid, r.admin
			expected := agentmemory.ErrForbidden
			switch name {
			case "target":
				mid = otherMemory
				expected = agentmemory.ErrNotFound
			case "source":
				source = otherMemory
			case "principal_is_entity":
				access.OrganizationID = f.org.AccountID
			}
			rec, e := b.store.PutOrganizationMemoryEvidence(b.ctx, access, mid, agentMemoryID(t, f.p), agentmemory.EvidenceReferenceInput{ExpectedMemoryVersion: 1, SourceType: agentorganizationevidence.AdminInput, SourceID: source})
			if !errors.Is(e, expected) || rec.ID != "" {
				t.Fatal("actual cross-org bridge", e)
			}
		})
	}
}
func TestOrganizationEvidenceNativeSourceNaturalExpiryDuringTargetWait(t *testing.T) {
	for _, op := range []string{"put", "read"} {
		t.Run(op, func(t *testing.T) {
			r := newOrganizationEvidenceFixture(t)
			f := r.f
			b := f.p.base
			r.put(t, agentorganizationevidence.AdminInput)
			lock, e := b.pool.Begin(b.ctx)
			if e != nil {
				t.Fatal(e)
			}
			defer lock.Rollback(context.Background())
			if _, e = lock.Exec(b.ctx, `SELECT id FROM agent_memories WHERE id=$1 FOR UPDATE`, r.mid); e != nil {
				t.Fatal(e)
			}
			var deadline time.Time
			if e = b.pool.QueryRow(b.ctx, `WITH stamp AS MATERIALIZED(SELECT clock_timestamp() at) UPDATE agent_memories SET version=version+1,valid_until=stamp.at+interval '500 milliseconds',updated_at=stamp.at FROM stamp WHERE id=$1 RETURNING valid_until`, r.admin).Scan(&deadline); e != nil {
				t.Fatal(e)
			}
			type result struct {
				err   error
				count int
			}
			done := make(chan result, 1)
			go func() {
				if op == "put" {
					_, e := b.store.PutOrganizationMemoryEvidence(b.ctx, f.access, r.mid, agentMemoryID(t, f.p), agentmemory.EvidenceReferenceInput{ExpectedMemoryVersion: 1, SourceType: agentorganizationevidence.AdminInput, SourceID: r.admin})
					done <- result{err: e}
				} else {
					p, e := b.store.ReadOrganizationMemoryProvenance(b.ctx, f.access, r.mid)
					done <- result{err: e, count: len(p.Evidence)}
				}
			}()
			orgMemoryNativeWait(t, f, lock.Conn().PgConn().PID(), &deadline)
			if e = lock.Commit(b.ctx); e != nil {
				t.Fatal(e)
			}
			select {
			case actual := <-done:
				if op == "put" && !errors.Is(actual.err, agentmemory.ErrForbidden) {
					t.Fatal("expired source put", actual.err)
				}
				if op == "read" && (actual.err != nil || actual.count != 0) {
					t.Fatal("expired source read", actual)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("expired source deadlock")
			}
		})
	}
}

func TestOrganizationEvidenceNativeBoundedCurrentSourcesAndCapacityRecovery(t *testing.T) {
	r := newOrganizationEvidenceFixture(t)
	f := r.f
	b := f.p.base
	ids := []string{}
	for i := 0; i < 101; i++ {
		source := r.admin
		if i > 0 {
			source = agentMemoryID(t, f.p)
			if _, e := b.store.PutOrganizationMemory(b.ctx, f.access, source, orgMemoryTestInput(agentmemory.OrgFAQ, fmt.Sprintf("limit-%03d", i))); e != nil {
				t.Fatal(e)
			}
		}
		id := agentMemoryID(t, f.p)
		_, e := b.store.PutOrganizationMemoryEvidence(b.ctx, f.access, r.mid, id, agentmemory.EvidenceReferenceInput{ExpectedMemoryVersion: 1, SourceType: agentorganizationevidence.AdminInput, SourceID: source})
		if i < 100 {
			if e != nil {
				t.Fatal(i, e)
			}
			ids = append(ids, id)
		} else {
			if !errors.Is(e, agentmemory.ErrConflict) {
				t.Fatal("101st source admitted", e)
			}
			p, e := b.store.ReadOrganizationMemoryProvenance(b.ctx, f.access, r.mid)
			if e != nil || len(p.Evidence) != 100 {
				t.Fatal("bounded native provenance", len(p.Evidence), e)
			}
			if _, e = b.store.RemoveOrganizationMemoryEvidence(b.ctx, f.access, r.mid, ids[0], 1); e != nil {
				t.Fatal(e)
			}
			if _, e = b.store.PutOrganizationMemoryEvidence(b.ctx, f.access, r.mid, id, agentmemory.EvidenceReferenceInput{ExpectedMemoryVersion: 1, SourceType: agentorganizationevidence.AdminInput, SourceID: source}); e != nil {
				t.Fatal("capacity did not recover from tombstone", e)
			}
		}
	}
	var count int
	if e := b.pool.QueryRow(b.ctx, `SELECT count(*) FROM agent_memory_evidence WHERE memory_id=$1 AND status='CURRENT'`, r.mid).Scan(&count); e != nil || count != 100 {
		t.Fatal(count, e)
	}
}
func TestOrganizationEvidenceNativeActivityWriterNoInverseLock(t *testing.T) {
	r := newOrganizationEvidenceFixture(t)
	f := r.f
	b := f.p.base
	r.put(t, agentorganizationevidence.Activity)
	lock, e := b.pool.Begin(b.ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer lock.Rollback(context.Background())
	if _, e = lock.Exec(b.ctx, `SELECT id FROM activities WHERE id=$1 FOR UPDATE`, r.activity); e != nil {
		t.Fatal(e)
	}
	writer := make(chan error, 1)
	in := r.activityInput
	in.Title = "合成并发活动新版"
	go func() { _, e := b.store.UpdateActivity(b.ctx, b.person.ID, f.org.ID, r.activity, in); writer <- e }()
	orgMemoryNativeWait(t, f, lock.Conn().PgConn().PID(), nil)
	read := make(chan error, 1)
	go func() { _, e := b.store.ReadOrganizationMemoryProvenance(b.ctx, f.access, r.mid); read <- e }()
	select {
	case e := <-read:
		if e != nil {
			t.Fatal(e)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("inverse Organization/Activity lock")
	}
	if e = lock.Commit(b.ctx); e != nil {
		t.Fatal(e)
	}
	select {
	case e := <-writer:
		if e != nil {
			t.Fatal(e)
		}
	case <-time.After(4 * time.Second):
		t.Fatal("native Activity writer stuck")
	}
	p, e := b.store.ReadOrganizationMemoryProvenance(b.ctx, f.access, r.mid)
	if e != nil || len(p.Evidence) != 0 {
		t.Fatal("native activity revision not invalidated", e)
	}
}
