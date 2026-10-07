package postgres

import (
	"bytes"
	"context"
	"errors"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/activitypublish"
	"github.com/birdtie/birdtie/apps/api/internal/agentevent"
	"github.com/birdtie/birdtie/apps/api/internal/agentprofile"
	"github.com/birdtie/birdtie/apps/api/internal/community"
	"github.com/birdtie/birdtie/apps/api/internal/content"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
)

type enrichmentFixture struct {
	base                    *agentEventFixture
	sources                 map[agentevent.Type]string
	activities, communities []string
	place                   string
}

func enrichmentNativeFixture(t *testing.T) *enrichmentFixture {
	t.Helper()
	if os.Getenv("BIRDTIE_DATABASE_URL") == "" {
		t.Skip("set BIRDTIE_DATABASE_URL for actual PostgreSQL enrichment verification")
	}
	if os.Getenv("BIRDTIE_DISPOSABLE_DB") != "1" {
		t.Fatal("enrichment tests require a real explicitly disposable PostgreSQL database")
	}
	f := &enrichmentFixture{base: eventIntegrationFixture(t), sources: map[agentevent.Type]string{}}
	b := f.base.private.base
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		for _, q := range []string{
			`DELETE FROM saved_items WHERE owner_account_id=ANY($1::uuid[])`,
			`DELETE FROM activity_participations WHERE participant_account_id=ANY($1::uuid[])`,
			`DELETE FROM audit_events WHERE actor_account_id=ANY($1::uuid[])`,
			`DELETE FROM activities WHERE created_by_account_id=ANY($1::uuid[])`,
			`DELETE FROM communities WHERE owner_account_id=ANY($1::uuid[])`,
			`DELETE FROM places WHERE maintainer_account_id=ANY($1::uuid[])`,
		} {
			if _, err := b.pool.Exec(ctx, q, b.accounts); err != nil {
				t.Errorf("owned enrichment source cleanup: %v", err)
			}
		}
		var count int
		if err := b.pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM saved_items WHERE owner_account_id=ANY($1::uuid[]))+
		 (SELECT count(*) FROM activity_participations WHERE participant_account_id=ANY($1::uuid[]))+
		 (SELECT count(*) FROM communities WHERE owner_account_id=ANY($1::uuid[]))+
		 (SELECT count(*) FROM activities WHERE created_by_account_id=ANY($1::uuid[]))+
		 (SELECT count(*) FROM places WHERE maintainer_account_id=ANY($1::uuid[]))`, b.accounts).Scan(&count); err != nil || count != 0 {
			t.Errorf("owned enrichment fixture residue=%d error=%v", count, err)
		}
	})
	f.sources[agentevent.MomentCreated] = f.base.moment.ID
	f.sources[agentevent.UserQuery] = f.base.task.ID
	for _, kind := range []agentevent.Type{agentevent.MomentUpdated, agentevent.MomentDeleted} {
		m, err := b.store.CreateMomentDraft(b.ctx, b.person.ID, content.MomentInput{CityID: "aberdeen-gb", Title: "合成064私密Moment", Body: "合成064不能泄漏正文", TimePrecision: "unknown", LocationPrecision: "city"})
		if err != nil {
			t.Fatal(err)
		}
		f.sources[kind] = m.ID
		if kind == agentevent.MomentUpdated {
			m, err = b.store.UpdateMomentDraft(b.ctx, b.person.ID, m.ID, m.Revision, content.MomentInput{CityID: "aberdeen-gb", Title: "合成064已编辑私密Moment", Body: "合成064编辑正文", TimePrecision: "unknown", LocationPrecision: "city"})
		} else {
			err = b.store.WithdrawMoment(b.ctx, b.person.ID, m.ID, m.Revision)
		}
		if err != nil {
			t.Fatal("native Moment update/withdraw failed", err)
		}
	}
	for _, kind := range []agentevent.Type{agentevent.ActivityJoined, agentevent.ActivityLeft} {
		a, err := b.store.CreateSocialDraft(b.ctx, b.other.ID, activitypublish.Input{Organizer: activitypublish.Organizer{Type: "PERSON", ID: b.other.ID}, CityID: "aberdeen-gb", Title: "合成064参与源活动", Summary: "明确本地合成数据", StartsAt: time.Now().Add(48 * time.Hour), EndsAt: time.Now().Add(50 * time.Hour), TimeZone: "Europe/London", Visibility: "public"})
		if err != nil {
			t.Fatal("native Activity draft", err)
		}
		f.activities = append(f.activities, a.ID)
		if _, err = b.store.PublishSocialActivity(b.ctx, b.other.ID, a.ID); err != nil {
			t.Fatal("native Activity publish", err)
		}
		p, _, err := b.store.JoinActivity(b.ctx, b.person.ID, a.ID)
		if err != nil {
			t.Fatal("native RSVP join", err)
		}
		if kind == agentevent.ActivityLeft {
			p, err = b.store.CancelParticipation(b.ctx, b.person.ID, a.ID)
			if err != nil {
				t.Fatal("native RSVP cancel", err)
			}
		}
		f.sources[kind] = p.ID
	}
	// Directory targets are explicitly synthetic, fixture-owned local rows. The
	// saved source itself is created through the actual native Save operation.
	if err := b.pool.QueryRow(b.ctx, `INSERT INTO places(id,city_id,name,category_code,publication_status,source_label,source_ref,maintainer_label,maintainer_account_id)
	 VALUES(gen_random_uuid(),'aberdeen-gb','合成064地点','sports','published','LOCAL_SYNTHETIC_FIXTURE','local:test:AGE064','测试维护者',$1) RETURNING id`, b.other.ID).Scan(&f.place); err != nil {
		t.Fatal(err)
	}
	saved, err := b.store.Save(b.ctx, b.person.ID, "place", f.place)
	if err != nil {
		t.Fatal("native place save", err)
	}
	f.sources[agentevent.PlaceSaved] = saved
	for _, kind := range []agentevent.Type{agentevent.CommunityJoined, agentevent.CommunityLeft} {
		c, err := b.store.CreateSocialCommunity(b.ctx, b.other.ID, community.SocialInput{Name: "合成064社区", Summary: "合成064社区私密说明", Visibility: "private", JoinPolicy: "open", CityID: "aberdeen-gb"})
		if err != nil {
			t.Fatal("native Community create", err)
		}
		f.communities = append(f.communities, c.ID)
		m, err := b.store.JoinSocialCommunity(b.ctx, b.person.ID, c.ID)
		if err != nil {
			t.Fatal("native Community join", err)
		}
		f.sources[kind] = m.ID
		if kind == agentevent.CommunityLeft {
			if err = b.store.LeaveSocialCommunity(b.ctx, b.person.ID, c.ID); err != nil {
				t.Fatal("native Community leave", err)
			}
		}
	}
	if _, err := b.store.UpdateOwnProfile(b.ctx, b.person.ID, identity.ProfileInput{DisplayName: "合成064私密资料", Bio: "合成064私密Bio", Visibility: "private"}); err != nil {
		t.Fatal("native own Profile edit", err)
	}
	f.sources[agentevent.ProfileUpdated] = b.person.ID
	savePrivateCanaries(t, f.base.private)
	f.sources[agentevent.PreferenceUpdated] = b.personID
	return f
}

func produceEnrichment(t *testing.T, f *enrichmentFixture, kind agentevent.Type) agentevent.Envelope {
	t.Helper()
	b := f.base.private.base
	e, err := b.store.ProduceAgentEnrichmentEvent(b.ctx, f.base.access, eventRequest(kind, f.sources[kind]))
	if err != nil {
		t.Fatalf("%s real current native producer: %v", kind, err)
	}
	return e
}

func denyEnrichment(t *testing.T, f *enrichmentFixture, access agentevent.Access, kind agentevent.Type, expected error) {
	t.Helper()
	b := f.base.private.base
	e, err := b.store.ProduceAgentEnrichmentEvent(b.ctx, access, eventRequest(kind, f.sources[kind]))
	if !errors.Is(err, expected) || e != (agentevent.Envelope{}) {
		t.Fatalf("%s denied current native source: got %v expected %v", kind, err, expected)
	}
}

func TestAgentEnrichmentNativeCurrentSnapshotsIntegration(t *testing.T) {
	f := enrichmentNativeFixture(t)
	b := f.base.private.base
	for _, d := range agentevent.Catalog() {
		if d.Support != agentevent.CurrentNativeSource {
			continue
		}
		t.Run(string(d.Type), func(t *testing.T) {
			e := produceEnrichment(t, f, d.Type)
			if e.Source.ID != f.sources[d.Type] || e.Source.Owner != b.person || e.Subject != b.person || e.Tenant != b.person || e.Actor.ID != b.person.ID || e.AgentID != b.personID || e.ProcessingStatus != agentevent.Unavailable {
				t.Fatal("event not derived from current exact native identity")
			}
			if e.Source.Version.Kind != d.VersionKind {
				t.Fatal("invented source version variant")
			}
			var sourceAt time.Time
			var revision int64
			var q string
			switch d.Type {
			case agentevent.MomentCreated:
				q = `SELECT created_at,revision FROM moments WHERE id=$1`
			case agentevent.MomentUpdated, agentevent.MomentDeleted:
				q = `SELECT updated_at,revision FROM moments WHERE id=$1`
			case agentevent.UserQuery:
				q = `SELECT updated_at,0::bigint FROM agent_tasks WHERE id=$1`
			case agentevent.ActivityJoined, agentevent.ActivityLeft:
				q = `SELECT updated_at,0::bigint FROM activity_participations WHERE id=$1`
			case agentevent.PlaceSaved:
				q = `SELECT created_at,0::bigint FROM saved_items WHERE id=$1`
			case agentevent.CommunityJoined, agentevent.CommunityLeft:
				q = `SELECT updated_at,0::bigint FROM community_memberships WHERE id=$1`
			case agentevent.ProfileUpdated:
				q = `SELECT updated_at,0::bigint FROM user_profiles WHERE account_id=$1`
			case agentevent.PreferenceUpdated:
				q = `SELECT updated_at,written_profile_version FROM agent_private_profiles WHERE agent_id=$1`
			}
			if err := b.pool.QueryRow(b.ctx, q, e.Source.ID).Scan(&sourceAt, &revision); err != nil || !e.OccurredAt.Equal(sourceAt) || e.Source.Version.Revision != revision {
				t.Fatal("source clock/version not actual native", err)
			}
			wire, err := agentevent.Encode(e, e.ReceivedAt)
			if err != nil {
				t.Fatal(err)
			}
			for _, canary := range []string{"合成064", f.base.moment.Title, f.base.moment.Body, f.base.task.Query, f.base.task.Conversation[0].Text, "合成私密", "agentNotes", "display_name", "cancelled_at", "token_sha256", "latitude", "permission_override", "confirmed", "consent_epoch"} {
				if bytes.Contains(wire, []byte(canary)) {
					t.Fatal("body/native row/authority leaked into metadata")
				}
			}
			if err = b.store.RevalidateAgentEnrichmentEvent(b.ctx, f.base.access, e); err != nil {
				t.Fatal(err)
			}
			second := produceEnrichment(t, f, d.Type)
			if second.EventID != e.EventID || !second.ExpiresAt.Equal(e.ExpiresAt) {
				t.Fatal("stable native retry changed identity or source lifetime")
			}
			r := eventRequest(d.Type, e.Source.ID)
			r.LogicalOperationID = "72000000-0000-4000-8000-000000000067"
			third, err := b.store.ProduceAgentEnrichmentEvent(b.ctx, f.base.access, r)
			if err != nil || third.EventID == e.EventID {
				t.Fatal("independent operation incorrectly collapsed")
			}
			if err = b.store.RevalidateAgentEnrichmentEvent(b.ctx, agentevent.Access{SessionDigest: f.base.private.peer.SessionDigest}, e); !errors.Is(err, agentevent.ErrDenied) {
				t.Fatal("other Person replay accepted")
			}
		})
	}
	for _, kind := range []agentevent.Type{agentevent.ActivityCompleted, agentevent.PlaceVisited} {
		t.Run(string(kind), func(t *testing.T) {
			f.sources[kind] = f.sources[agentevent.ActivityJoined]
			denyEnrichment(t, f, f.base.access, kind, agentevent.ErrUnavailable)
		})
	}
}

func TestAgentEnrichmentCurrentIdentityBoundariesIntegration(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*enrichmentFixture)
		access func(*enrichmentFixture) agentevent.Access
	}{
		{"anonymous", func(*enrichmentFixture) {}, func(*enrichmentFixture) agentevent.Access { return agentevent.Access{} }},
		{"invalid_session", func(*enrichmentFixture) {}, func(*enrichmentFixture) agentevent.Access { return agentevent.Access{SessionDigest: [32]byte{11, 64}} }},
		{"other_person", func(*enrichmentFixture) {}, func(f *enrichmentFixture) agentevent.Access {
			return agentevent.Access{SessionDigest: f.base.private.peer.SessionDigest}
		}},
		{"organization", func(*enrichmentFixture) {}, func(f *enrichmentFixture) agentevent.Access {
			return agentevent.Access{SessionDigest: f.base.private.org.SessionDigest}
		}},
		{"business", func(*enrichmentFixture) {}, func(f *enrichmentFixture) agentevent.Access {
			return agentevent.Access{SessionDigest: f.base.private.biz.SessionDigest}
		}},
		{"revoked", func(f *enrichmentFixture) {
			f.base.private.base.exec(`UPDATE sessions SET revoked_at=now() WHERE id=$1`, f.base.private.ownerSession)
		}, nil},
		{"expired", func(f *enrichmentFixture) {
			f.base.private.base.exec(`UPDATE sessions SET created_at=now()-interval '2 hours',expires_at=now()-interval '1 hour',idle_expires_at=now()-interval '1 hour' WHERE id=$1`, f.base.private.ownerSession)
		}, nil},
		{"idle_expired", func(f *enrichmentFixture) {
			f.base.private.base.exec(`UPDATE sessions SET created_at=now()-interval '2 hours',idle_expires_at=now()-interval '1 hour' WHERE id=$1`, f.base.private.ownerSession)
		}, nil},
		{"dev_phone_disabled", func(f *enrichmentFixture) {
			f.base.private.base.exec(`UPDATE sessions SET authentication_method='dev_phone' WHERE id=$1`, f.base.private.ownerSession)
		}, nil},
		{"person_suspended", func(f *enrichmentFixture) {
			f.base.private.base.exec(`UPDATE accounts SET status='suspended' WHERE id=$1`, f.base.private.base.person.ID)
		}, nil},
		{"agent_retired", func(f *enrichmentFixture) {
			f.base.private.base.exec(`UPDATE agents SET status='retired' WHERE id=$1`, f.base.private.base.personID)
		}, nil},
		{"metadata_missing", func(f *enrichmentFixture) {
			f.base.private.base.exec(`DELETE FROM agent_profiles WHERE agent_id=$1`, f.base.private.base.personID)
		}, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := enrichmentNativeFixture(t)
			tc.mutate(f)
			a := f.base.access
			if tc.access != nil {
				a = tc.access(f)
			}
			for _, d := range agentevent.Catalog() {
				if d.Support == agentevent.CurrentNativeSource {
					t.Run(string(d.Type), func(t *testing.T) { denyEnrichment(t, f, a, d.Type, agentevent.ErrDenied) })
				}
			}
		})
	}
}

func TestAgentEnrichmentNativeVersionAndInvalidationIntegration(t *testing.T) {
	t.Run("moment_edit_withdraw_no_old_body", func(t *testing.T) {
		f := enrichmentNativeFixture(t)
		b := f.base.private.base
		e := produceEnrichment(t, f, agentevent.MomentUpdated)
		if _, err := b.store.UpdateMomentDraft(b.ctx, b.person.ID, e.Source.ID, e.Source.Version.Revision, content.MomentInput{CityID: "aberdeen-gb", Title: "合成064版本变化", Body: "合成064下一版正文", TimePrecision: "unknown", LocationPrecision: "city"}); err != nil {
			t.Fatal(err)
		}
		if err := b.store.RevalidateAgentEnrichmentEvent(b.ctx, f.base.access, e); !errors.Is(err, agentevent.ErrDenied) {
			t.Fatal("old native Moment revision accepted")
		}
		current := produceEnrichment(t, f, agentevent.MomentUpdated)
		if err := b.store.WithdrawMoment(b.ctx, b.person.ID, current.Source.ID, current.Source.Version.Revision); err != nil {
			t.Fatal(err)
		}
		denyEnrichment(t, f, f.base.access, agentevent.MomentUpdated, agentevent.ErrDenied)
		f.sources[agentevent.MomentDeleted] = e.Source.ID
		deleted := produceEnrichment(t, f, agentevent.MomentDeleted)
		if deleted.Purpose != agentevent.SourceInvalidation {
			t.Fatal("withdrawal became analysis input")
		}
		b.exec(`DELETE FROM moments WHERE id=$1`, e.Source.ID)
		if err := b.store.RevalidateAgentEnrichmentEvent(b.ctx, f.base.access, deleted); !errors.Is(err, agentevent.ErrDenied) {
			t.Fatal("purged source retained a invented receipt")
		}
	})
	t.Run("rsvp_cancel_rejoin", func(t *testing.T) {
		f := enrichmentNativeFixture(t)
		b := f.base.private.base
		e := produceEnrichment(t, f, agentevent.ActivityJoined)
		if _, err := b.store.CancelParticipation(b.ctx, b.person.ID, f.activities[0]); err != nil {
			t.Fatal(err)
		}
		if err := b.store.RevalidateAgentEnrichmentEvent(b.ctx, f.base.access, e); !errors.Is(err, agentevent.ErrDenied) {
			t.Fatal("old going state replayed")
		}
		f.sources[agentevent.ActivityLeft] = e.Source.ID
		left := produceEnrichment(t, f, agentevent.ActivityLeft)
		if _, _, err := b.store.JoinActivity(b.ctx, b.person.ID, f.activities[0]); err != nil {
			t.Fatal(err)
		}
		if err := b.store.RevalidateAgentEnrichmentEvent(b.ctx, f.base.access, left); !errors.Is(err, agentevent.ErrDenied) {
			t.Fatal("old cancellation replayed")
		}
		current := produceEnrichment(t, f, agentevent.ActivityJoined)
		if current.EventID == e.EventID {
			t.Fatal("rejoin reused old source fingerprint")
		}
	})
	t.Run("save_remove_not_visit", func(t *testing.T) {
		f := enrichmentNativeFixture(t)
		b := f.base.private.base
		e := produceEnrichment(t, f, agentevent.PlaceSaved)
		id, err := b.store.Save(b.ctx, b.person.ID, "place", f.place)
		if err != nil || id != e.Source.ID {
			t.Fatal("native duplicate save changed source")
		}
		if err = b.store.RemoveSaved(b.ctx, b.person.ID, id); err != nil {
			t.Fatal(err)
		}
		if err = b.store.RevalidateAgentEnrichmentEvent(b.ctx, f.base.access, e); !errors.Is(err, agentevent.ErrDenied) {
			t.Fatal("removed bookmark replayed")
		}
		id, err = b.store.Save(b.ctx, b.person.ID, "place", f.place)
		if err != nil || id == e.Source.ID {
			t.Fatal("new bookmark reused removed row")
		}
		f.sources[agentevent.PlaceSaved] = id
		produceEnrichment(t, f, agentevent.PlaceSaved)
		f.sources[agentevent.PlaceVisited] = id
		denyEnrichment(t, f, f.base.access, agentevent.PlaceVisited, agentevent.ErrUnavailable)
	})
	t.Run("community_role_snapshot_leave_rejoin", func(t *testing.T) {
		f := enrichmentNativeFixture(t)
		b := f.base.private.base
		e := produceEnrichment(t, f, agentevent.CommunityJoined)
		// The role is a current snapshot, not proof of another join transition.
		if _, err := b.store.ChangeSocialMemberRole(b.ctx, b.other.ID, f.communities[0], e.Source.ID, "admin"); err != nil {
			t.Fatal(err)
		}
		if err := b.store.RevalidateAgentEnrichmentEvent(b.ctx, f.base.access, e); !errors.Is(err, agentevent.ErrDenied) {
			t.Fatal("changed membership snapshot replayed")
		}
		if err := b.store.LeaveSocialCommunity(b.ctx, b.person.ID, f.communities[0]); err != nil {
			t.Fatal(err)
		}
		f.sources[agentevent.CommunityLeft] = e.Source.ID
		left := produceEnrichment(t, f, agentevent.CommunityLeft)
		if _, err := b.store.JoinSocialCommunity(b.ctx, b.person.ID, f.communities[0]); err != nil {
			t.Fatal(err)
		}
		if err := b.store.RevalidateAgentEnrichmentEvent(b.ctx, f.base.access, left); !errors.Is(err, agentevent.ErrDenied) {
			t.Fatal("old left state replayed")
		}
	})
	t.Run("profile_same_clock_content", func(t *testing.T) {
		f := enrichmentNativeFixture(t)
		b := f.base.private.base
		e := produceEnrichment(t, f, agentevent.ProfileUpdated)
		b.exec(`UPDATE user_profiles SET bio='合成064同时间不同正文' WHERE account_id=$1`, b.person.ID)
		if err := b.store.RevalidateAgentEnrichmentEvent(b.ctx, f.base.access, e); !errors.Is(err, agentevent.ErrDenied) {
			t.Fatal("same clock changed profile not invalidated")
		}
		current := produceEnrichment(t, f, agentevent.ProfileUpdated)
		if !current.OccurredAt.Equal(e.OccurredAt) || current.EventID == e.EventID {
			t.Fatal("profile digest did not bind exact current source")
		}
	})
	t.Run("preference_source_write_version_not_metadata", func(t *testing.T) {
		f := enrichmentNativeFixture(t)
		b := f.base.private.base
		e := produceEnrichment(t, f, agentevent.PreferenceUpdated)
		policy, policyErr := b.store.ReadOwnAgentProfileVisibility(b.ctx, f.base.private.owner)
		if policyErr != nil {
			t.Fatal(policyErr)
		}
		rules := agentprofile.DefaultFieldRules()
		rules[agentprofile.FieldBio] = agentprofile.FieldRule{Visibility: agentprofile.VisibilityAgentOnly}
		if _, policyErr = b.store.ReplaceOwnAgentProfileVisibility(b.ctx, f.base.private.owner, agentprofile.ReplaceVisibilityInput{ExpectedVersion: policy.Profile.ProfileVersion, Rules: rules}); policyErr != nil {
			t.Fatal("native field visibility metadata revision", policyErr)
		}
		current := produceEnrichment(t, f, agentevent.PreferenceUpdated)
		if current.Source.Version != e.Source.Version || current.EventID != e.EventID {
			t.Fatal("aggregate metadata substituted native preference write version")
		}
		r, err := b.store.ReadOwnAgentPrivateProfile(b.ctx, f.base.private.owner)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = b.store.ReplaceOwnAgentPrivateProfile(b.ctx, f.base.private.owner, agentprofile.ReplacePrivateInput{ExpectedVersion: r.Profile.ProfileVersion, Fields: agentprofile.PrivateFields{}}); err != nil {
			t.Fatal(err)
		}
		denyEnrichment(t, f, f.base.access, agentevent.PreferenceUpdated, agentevent.ErrDenied)
		if err = b.store.RevalidateAgentEnrichmentEvent(b.ctx, f.base.access, e); !errors.Is(err, agentevent.ErrDenied) {
			t.Fatal("cleared Preference invented empty receipt")
		}
	})
}

func TestAgentEnrichmentTargetAndStateGuardsIntegration(t *testing.T) {
	cases := []struct {
		name   string
		kind   agentevent.Type
		mutate func(*enrichmentFixture)
	}{
		{"initial_moment_not_update", agentevent.MomentUpdated, func(f *enrichmentFixture) { f.sources[agentevent.MomentUpdated] = f.base.moment.ID }},
		{"draft_not_deleted", agentevent.MomentDeleted, func(f *enrichmentFixture) { f.sources[agentevent.MomentDeleted] = f.base.moment.ID }},
		{"rsvp_pending_not_joined", agentevent.ActivityJoined, func(f *enrichmentFixture) {
			f.base.private.base.exec(`UPDATE activity_participations SET status='pending' WHERE id=$1`, f.sources[agentevent.ActivityJoined])
		}},
		{"going_not_left", agentevent.ActivityLeft, func(f *enrichmentFixture) { f.sources[agentevent.ActivityLeft] = f.sources[agentevent.ActivityJoined] }},
		{"activity_hidden", agentevent.ActivityJoined, func(f *enrichmentFixture) {
			f.base.private.base.exec(`UPDATE activities SET publication_status='hidden' WHERE id=$1`, f.activities[0])
		}},
		{"activity_expired", agentevent.ActivityJoined, func(f *enrichmentFixture) {
			f.base.private.base.exec(`UPDATE activities SET expires_at=now()-interval '1 second' WHERE id=$1`, f.activities[0])
		}},
		{"activity_cancelled", agentevent.ActivityJoined, func(f *enrichmentFixture) {
			if _, err := f.base.private.base.store.CancelSocialActivity(f.base.private.base.ctx, f.base.private.base.other.ID, f.activities[0]); err != nil {
				t.Fatal(err)
			}
		}},
		{"activity_completed_not_attendance", agentevent.ActivityJoined, func(f *enrichmentFixture) {
			f.base.private.base.exec(`UPDATE activities SET starts_at=now()-interval '2 days',ends_at=now()-interval '1 day' WHERE id=$1`, f.activities[0])
		}},
		{"host_blocked", agentevent.ActivityJoined, func(f *enrichmentFixture) {
			b := f.base.private.base
			b.exec(`INSERT INTO account_blocks(blocker_account_id,blocked_account_id) VALUES($1,$2)`, b.person.ID, b.other.ID)
		}},
		{"place_hidden", agentevent.PlaceSaved, func(f *enrichmentFixture) {
			f.base.private.base.exec(`UPDATE places SET publication_status='hidden' WHERE id=$1`, f.place)
		}},
		{"place_expired", agentevent.PlaceSaved, func(f *enrichmentFixture) {
			f.base.private.base.exec(`UPDATE places SET expires_at=now()-interval '1 second' WHERE id=$1`, f.place)
		}},
		{"activity_save_not_place", agentevent.PlaceSaved, func(f *enrichmentFixture) {
			b := f.base.private.base
			id, err := b.store.Save(b.ctx, b.person.ID, "activity", f.activities[0])
			if err != nil {
				t.Fatal(err)
			}
			f.sources[agentevent.PlaceSaved] = id
		}},
		{"community_pending", agentevent.CommunityJoined, func(f *enrichmentFixture) {
			f.base.private.base.exec(`UPDATE community_memberships SET status='pending' WHERE id=$1`, f.sources[agentevent.CommunityJoined])
		}},
		{"community_invited", agentevent.CommunityJoined, func(f *enrichmentFixture) {
			f.base.private.base.exec(`UPDATE community_memberships SET status='invited' WHERE id=$1`, f.sources[agentevent.CommunityJoined])
		}},
		{"community_active_not_left", agentevent.CommunityLeft, func(f *enrichmentFixture) {
			f.sources[agentevent.CommunityLeft] = f.sources[agentevent.CommunityJoined]
		}},
		{"community_archived", agentevent.CommunityJoined, func(f *enrichmentFixture) {
			if err := f.base.private.base.store.ArchiveSocialCommunity(f.base.private.base.ctx, f.base.private.base.other.ID, f.communities[0]); err != nil {
				t.Fatal(err)
			}
		}},
		{"community_rejected_not_left", agentevent.CommunityLeft, func(f *enrichmentFixture) {
			f.base.private.base.exec(`UPDATE community_memberships SET status='rejected' WHERE id=$1`, f.sources[agentevent.CommunityLeft])
		}},
		{"community_owner_block", agentevent.CommunityJoined, func(f *enrichmentFixture) {
			b := f.base.private.base
			b.exec(`INSERT INTO account_blocks(blocker_account_id,blocked_account_id) VALUES($1,$2)`, b.other.ID, b.person.ID)
		}},
		{"profile_wrong_person_id", agentevent.ProfileUpdated, func(f *enrichmentFixture) { f.sources[agentevent.ProfileUpdated] = f.base.private.base.other.ID }},
		{"preference_wrong_agent", agentevent.PreferenceUpdated, func(f *enrichmentFixture) { f.sources[agentevent.PreferenceUpdated] = f.base.private.base.otherID }},
		{"notes_are_not_preferences", agentevent.PreferenceUpdated, func(f *enrichmentFixture) {
			b := f.base.private.base
			r, err := b.store.ReadOwnAgentPrivateProfile(b.ctx, f.base.private.owner)
			if err != nil {
				t.Fatal(err)
			}
			_, err = b.store.ReplaceOwnAgentPrivateProfile(b.ctx, f.base.private.owner, agentprofile.ReplacePrivateInput{ExpectedVersion: r.Profile.ProfileVersion, Fields: agentprofile.PrivateFields{AgentNotes: "合成备注", Availability: "合成时间", PrivateCityHistory: "合成历史"}})
			if err != nil {
				t.Fatal(err)
			}
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := enrichmentNativeFixture(t)
			tc.mutate(f)
			denyEnrichment(t, f, f.base.access, tc.kind, agentevent.ErrDenied)
		})
	}
}

func TestAgentEnrichmentTTLReplayAndConcurrentRetryIntegration(t *testing.T) {
	f := enrichmentNativeFixture(t)
	b := f.base.private.base
	for _, kind := range []agentevent.Type{agentevent.ActivityJoined, agentevent.PlaceSaved, agentevent.ProfileUpdated} {
		t.Run(string(kind), func(t *testing.T) {
			e := produceEnrichment(t, f, kind)
			var wg sync.WaitGroup
			issues := make(chan error, 20)
			for i := 0; i < 20; i++ {
				wg.Add(1)
				go func() {
					defer wg.Done()
					v, err := b.store.ProduceAgentEnrichmentEvent(b.ctx, f.base.access, eventRequest(kind, f.sources[kind]))
					if err != nil {
						issues <- err
					} else if v.EventID != e.EventID || !v.ExpiresAt.Equal(e.ExpiresAt) {
						issues <- errors.New("duplicate current source changed identity/lifetime")
					}
				}()
			}
			wg.Wait()
			close(issues)
			for err := range issues {
				t.Error(err)
			}
			var query string
			switch kind {
			case agentevent.ActivityJoined:
				// The event TTL uses updated_at. Age that retained native source
				// without remapping immutable identity/creation or lowering078.
				const identityQuery = `SELECT jsonb_build_array(p.id,p.activity_id,p.participant_account_id,p.created_at)::text,COALESCE((to_jsonb(p)->>'disclosure_revision')::bigint,0) FROM activity_participations p WHERE id=$1`
				var before, after string
				var oldRevision, newRevision int64
				if err := b.pool.QueryRow(b.ctx, identityQuery, e.Source.ID).Scan(&before, &oldRevision); err != nil {
					t.Fatal(err)
				}
				b.exec(`UPDATE activity_participations SET updated_at=now()-interval '16 minutes' WHERE id=$1`, e.Source.ID)
				if err := b.pool.QueryRow(b.ctx, identityQuery, e.Source.ID).Scan(&after, &newRevision); err != nil {
					t.Fatal(err)
				}
				if before != after || (oldRevision > 0 && newRevision != oldRevision+1) {
					t.Fatal("source expiry remapped identity or failed to advance revision")
				}
				denyEnrichment(t, f, f.base.access, kind, agentevent.ErrExpired)
				return
			case agentevent.PlaceSaved:
				query = `UPDATE saved_items SET created_at=now()-interval '16 minutes' WHERE id=$1`
			case agentevent.ProfileUpdated:
				query = `UPDATE user_profiles SET updated_at=now()-interval '16 minutes' WHERE account_id=$1`
			}
			b.exec(query, e.Source.ID)
			denyEnrichment(t, f, f.base.access, kind, agentevent.ErrExpired)
		})
	}
	e := produceEnrichment(t, f, agentevent.PreferenceUpdated)
	b.exec(`UPDATE sessions SET revoked_at=now() WHERE id=$1`, f.base.private.ownerSession)
	if err := b.store.RevalidateAgentEnrichmentEvent(b.ctx, f.base.access, e); !errors.Is(err, agentevent.ErrDenied) {
		t.Fatal("post-revocation replay accepted")
	}
}

func TestAgentEnrichmentControlSnapshotsAndPreferenceFieldsIntegration(t *testing.T) {
	for _, tc := range []struct {
		name   string
		fields agentprofile.PrivateFields
	}{
		{"personal", agentprofile.PrivateFields{PersonalPreferences: []string{"合成个人偏好"}}},
		{"social", agentprofile.PrivateFields{SocialPreferences: []string{"合成社交偏好"}}},
		{"activities", agentprofile.PrivateFields{PreferredActivityTypes: []string{"合成活动偏好"}}},
		{"travel", agentprofile.PrivateFields{TravelPreferences: []string{"合成出行偏好"}}},
		{"interaction", agentprofile.PrivateFields{InteractionPreferences: []string{"合成交互偏好"}}},
		{"language", agentprofile.PrivateFields{LanguagePreferences: []string{"合成语言偏好"}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := enrichmentNativeFixture(t)
			b := f.base.private.base
			r, err := b.store.ReadOwnAgentPrivateProfile(b.ctx, f.base.private.owner)
			if err != nil {
				t.Fatal(err)
			}
			written, err := b.store.ReplaceOwnAgentPrivateProfile(b.ctx, f.base.private.owner, agentprofile.ReplacePrivateInput{ExpectedVersion: r.Profile.ProfileVersion, Fields: tc.fields})
			if err != nil {
				t.Fatal(err)
			}
			e := produceEnrichment(t, f, agentevent.PreferenceUpdated)
			if e.Source.Version.Revision != written.Profile.ProfileVersion {
				t.Fatal("explicit preference field not bound to actual native write")
			}
		})
	}
	t.Run("admin_removal_actor_is_current_producer", func(t *testing.T) {
		f := enrichmentNativeFixture(t)
		b := f.base.private.base
		if err := b.store.RemoveSocialMember(b.ctx, b.other.ID, f.communities[0], f.sources[agentevent.CommunityJoined]); err != nil {
			t.Fatal(err)
		}
		f.sources[agentevent.CommunityLeft] = f.sources[agentevent.CommunityJoined]
		e := produceEnrichment(t, f, agentevent.CommunityLeft)
		if e.Actor.ID != b.person.ID || e.Actor.ID == b.other.ID || e.Purpose != agentevent.SourceInvalidation {
			t.Fatal("current snapshot invented historical action actor")
		}
	})
	t.Run("left_control_retained_after_hidden_target", func(t *testing.T) {
		f := enrichmentNativeFixture(t)
		b := f.base.private.base
		b.exec(`UPDATE activities SET publication_status='hidden' WHERE id=$1`, f.activities[1])
		produceEnrichment(t, f, agentevent.ActivityLeft)
		if err := b.store.ArchiveSocialCommunity(b.ctx, b.other.ID, f.communities[1]); err != nil {
			t.Fatal(err)
		}
		produceEnrichment(t, f, agentevent.CommunityLeft)
		b.exec(`DELETE FROM community_memberships WHERE id=$1`, f.sources[agentevent.CommunityLeft])
		denyEnrichment(t, f, f.base.access, agentevent.CommunityLeft, agentevent.ErrDenied)
	})
	t.Run("hidden_current_active_member_allowed", func(t *testing.T) {
		f := enrichmentNativeFixture(t)
		b := f.base.private.base
		b.exec(`UPDATE communities SET visibility='hidden' WHERE id=$1`, f.communities[0])
		produceEnrichment(t, f, agentevent.CommunityJoined)
	})
	t.Run("pending_cancelled_is_not_attendance", func(t *testing.T) {
		f := enrichmentNativeFixture(t)
		b := f.base.private.base
		b.exec(`UPDATE activity_participations SET status='pending' WHERE id=$1`, f.sources[agentevent.ActivityJoined])
		if _, err := b.store.CancelParticipation(b.ctx, b.person.ID, f.activities[0]); err != nil {
			t.Fatal(err)
		}
		f.sources[agentevent.ActivityLeft] = f.sources[agentevent.ActivityJoined]
		e := produceEnrichment(t, f, agentevent.ActivityLeft)
		if d, _ := agentevent.Lookup(e.EventType); d.ReceiptSemantics != agentevent.CurrentRetainedState {
			t.Fatal("cancelled request represented attended activity")
		}
		f.sources[agentevent.ActivityCompleted] = e.Source.ID
		denyEnrichment(t, f, f.base.access, agentevent.ActivityCompleted, agentevent.ErrUnavailable)
	})
	t.Run("future_native_clock_rejected", func(t *testing.T) {
		f := enrichmentNativeFixture(t)
		b := f.base.private.base
		b.exec(`UPDATE user_profiles SET updated_at=now()+interval '1 hour' WHERE account_id=$1`, b.person.ID)
		denyEnrichment(t, f, f.base.access, agentevent.ProfileUpdated, agentevent.ErrInvalid)
	})
	t.Run("no_grant_no_nil_store_or_cancelled_context", func(t *testing.T) {
		f := enrichmentNativeFixture(t)
		b := f.base.private.base
		var unavailableStore *Store
		e, err := unavailableStore.ProduceAgentEnrichmentEvent(b.ctx, f.base.access, eventRequest(agentevent.PlaceSaved, f.sources[agentevent.PlaceSaved]))
		if !errors.Is(err, agentevent.ErrUnavailable) || e != (agentevent.Envelope{}) {
			t.Fatal("nil store became source")
		}
		ctx, cancel := context.WithCancel(b.ctx)
		cancel()
		e, err = b.store.ProduceAgentEnrichmentEvent(ctx, f.base.access, eventRequest(agentevent.PreferenceUpdated, f.sources[agentevent.PreferenceUpdated]))
		if !errors.Is(err, context.Canceled) || e != (agentevent.Envelope{}) {
			t.Fatal("cancelled context produced receipt")
		}
	})
}
