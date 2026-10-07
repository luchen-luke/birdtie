package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/birdtie/birdtie/apps/api/internal/activitypublish"
	arp "github.com/birdtie/birdtie/apps/api/internal/agentresultprojection"
	"github.com/birdtie/birdtie/apps/api/internal/agentworkspace"
	"github.com/birdtie/birdtie/apps/api/internal/community"
	ea "github.com/birdtie/birdtie/apps/api/internal/entityaction"
	"github.com/birdtie/birdtie/apps/api/internal/foundation"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	"github.com/birdtie/birdtie/apps/api/internal/intent"
	"github.com/birdtie/birdtie/apps/api/internal/socialintent"
	"github.com/jackc/pgx/v5/pgxpool"
	"strings"
	"testing"
	"time"
)

func TestAgentResultProjectionNativePortIsImplemented(t *testing.T) {
	if _, ok := any(New(nil, false)).(arp.NativeStore); !ok {
		t.Fatal("seven-type DTO exists but the actual PostgreSQL Store has no current-source projection/receipt producer")
	}
}

func TestAgentResultProjectionNativeSQLShape(t *testing.T) {
	f := contextBuilderNative(t)
	b := f.place.private.base
	conn, e := b.pool.Acquire(b.ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer conn.Release()
	if _, e = conn.Conn().Prepare(b.ctx, "native_result_projection_shape", resultProjectionSQL()); e != nil {
		t.Fatal("actual PG prepare", e)
	}
}

func TestAgentResultProjectionNativePublicActivityAndReceipt(t *testing.T) {
	f := contextBuilderNative(t)
	b := f.place.private.base
	b.exec(`UPDATE places SET coordinate_system='wgs84',location_precision='point',latitude=0,longitude=0 WHERE id=$1`, f.place.place)
	raw, e := json.Marshal(agentworkspace.SanitizeTaskForResponse(f.task))
	if e != nil {
		t.Fatal(e)
	}
	a := arp.Access{Actor: identity.Actor{ID: b.person.ID, AccountType: "person"}, SessionDigest: f.place.private.owner.SessionDigest, TaskID: f.task.ID, ExpectedTask: raw}
	q := arp.Query{CityID: f.place.city, Kind: "activity", Category: "badminton", TimePreference: "anytime", CompareIDs: []string{}}
	r, e := b.store.ReadAgentResultProjection(b.ctx, a, q)
	if e != nil {
		t.Fatal("actual native projection", e)
	}
	if !r.Valid() || len(r.Items) != 2 {
		t.Fatal("authorized public/invited sources", r.Items)
	}
	found := false
	for _, item := range r.Items {
		if item.Entity.ID == f.public {
			found = true
			if item.Detail.ID != f.public || item.Share.ID != f.public || item.Anchor == nil || item.Anchor.PlaceID != f.place.place {
				t.Fatal("original public refs/anchor", item)
			}
		}
	}
	if !found || len(r.PublicCommercialRefs) != 1 || r.PublicCommercialRefs[0] != (arp.Ref{Type: "activity", ID: f.public}) {
		t.Fatal("private invited activity became promotional target", r.PublicCommercialRefs)
	}
	wire, _ := json.Marshal(r.Items)
	if strings.Contains(string(wire), "UNREQUESTED_ACTIVITY_BODY_CANARY") {
		t.Fatal("non-public/body source escaped")
	}
	if e = b.store.RevalidateAgentResultProjection(b.ctx, a, q, r); e != nil {
		t.Fatal("same actual source", e)
	}
	b.exec(`UPDATE activities SET title=title WHERE id=$1`, f.public)
	if e = b.store.RevalidateAgentResultProjection(b.ctx, a, q, r); !errors.Is(e, arp.ErrChanged) {
		t.Fatal("source xmin ABA accepted", e)
	}
}

func resultNativeRequest(t *testing.T, f *contextBuilderNativeFixture, kind string) (arp.Access, arp.Query) {
	t.Helper()
	b := f.place.private.base
	operations := map[string]string{"activity": agentworkspace.FindActivity, "place": agentworkspace.FindPlace, "person": agentworkspace.FindPerson, "community": agentworkspace.FindCommunity, "organization": agentworkspace.FindOrganization, "business": agentworkspace.FindBusiness, "opportunity": agentworkspace.FindOpportunity}
	f.task.Intent = operations[kind]
	f.task.Filters = map[string]string{"currentQuery": "本人明确查询合成" + kind, "timePreference": "anytime"}
	var e error
	f.task, e = b.store.UpdateTask(b.ctx, f.task)
	if e != nil {
		t.Fatal(e)
	}
	raw, _ := json.Marshal(agentworkspace.SanitizeTaskForResponse(f.task))
	return arp.Access{Actor: identity.Actor{ID: b.person.ID, AccountType: "person"}, SessionDigest: f.place.private.owner.SessionDigest, TaskID: f.task.ID, ExpectedTask: raw}, arp.Query{CityID: f.place.city, Kind: kind, TimePreference: "anytime", CompareIDs: []string{}}
}

func resultNativeOwnIntent(t *testing.T, f *contextBuilderNativeFixture) string {
	t.Helper()
	b := f.place.private.base
	i, e := b.store.CreateSocialIntentDraft(b.ctx, b.person.ID, socialintent.DraftInput{Type: "FIND_ACTIVITY", Title: "PRIVATE_INTENT_BODY_MUST_NOT_APPEAR", Constraints: json.RawMessage(`{"category":"badminton","placeId":"` + f.place.place + `"}`), Audience: "PRIVATE", Modality: "IN_PERSON", ExpiresAt: time.Now().UTC().Add(time.Hour)})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = b.store.ActivateSocialIntent(b.ctx, b.person.ID, i.ID); e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() {
		if _, e := b.pool.Exec(context.Background(), `DELETE FROM social_intents WHERE id=$1`, i.ID); e != nil {
			t.Error(e)
		}
	})
	return i.ID
}

func TestAgentResultProjectionNativeSevenOriginalDomainRefs(t *testing.T) {
	for _, kind := range []string{"person", "activity", "place", "community", "organization", "business", "opportunity"} {
		t.Run(kind, func(t *testing.T) {
			f := contextBuilderNative(t)
			b := f.place.private.base
			id := ""
			switch kind {
			case "activity":
				id = f.public
			case "place":
				id = f.place.place
			case "person":
				b.exec(`UPDATE user_profiles SET visibility='public' WHERE account_id=$1`, b.other.ID)
				i, e := b.store.SubmitIntent(b.ctx, b.other.ID, f.place.city, intent.Input{Confirmed: true, Topic: "合成公开 badminton", Details: "不可复制未请求的详情", AvailableFrom: time.Now().Add(-time.Minute), AvailableUntil: time.Now().Add(time.Hour), TimeZone: "Europe/London", CoarseAreaLabel: "本人明确公开范围", ExpiresAt: time.Now().Add(time.Hour)})
				if e != nil {
					t.Fatal(e)
				}
				id = b.other.ID
				t.Cleanup(func() {
					if _, e := b.pool.Exec(context.Background(), `DELETE FROM intents WHERE id=$1`, i.ID); e != nil {
						t.Error(e)
					}
				})
			case "community":
				c, e := b.store.CreateSocialCommunity(b.ctx, b.other.ID, community.SocialInput{Name: "合成原生社区", Summary: "原公开简介", CityID: f.place.city, Visibility: "public", JoinPolicy: "open"})
				if e != nil {
					t.Fatal(e)
				}
				id = c.ID
				t.Cleanup(func() {
					for _, q := range []string{`DELETE FROM communities WHERE id=$1`} {
						if _, e := b.pool.Exec(context.Background(), q, id); e != nil {
							t.Error(e)
						}
					}
				})
			case "organization":
				id = b.orgID
				b.exec(`UPDATE organizations SET visibility='public',verification_status='verified' WHERE id=$1`, id)
				b.exec(`INSERT INTO organization_map_locations(organization_id,city_id,latitude,longitude,visibility,review_status,submitted_by,reviewed_by,reviewed_at) VALUES($1,$2,0,0,'public','approved',$3,$4,clock_timestamp())`, id, f.place.city, b.person.ID, b.other.ID)
				t.Cleanup(func() {
					if _, e := b.pool.Exec(context.Background(), `DELETE FROM organization_map_locations WHERE organization_id=$1`, id); e != nil {
						t.Error(e)
					}
				})
			case "business":
				biz, businessID, publish := supplierNativeFixture(t)
				id = businessID
				a, e := biz.store.CreateSocialDraft(biz.ctx, biz.people[0], activitypublish.Input{Organizer: activitypublish.Organizer{Type: "BUSINESS", ID: id}, CityID: f.place.city, Title: "合成无地点线上商家活动", Summary: "明确公开", StartsAt: time.Now().Add(time.Hour), EndsAt: time.Now().Add(2 * time.Hour), TimeZone: "Europe/London", Visibility: "public", Modality: "online", PhysicalPlaceStatus: "not_applicable"})
				if e != nil {
					t.Fatal(e)
				}
				if _, e = biz.store.PublishSocialActivity(biz.ctx, biz.people[0], a.ID); e != nil {
					t.Fatal(e)
				}
				t.Cleanup(func() {
					if _, e := biz.pool.Exec(context.Background(), `DELETE FROM activities WHERE id=$1`, a.ID); e != nil {
						t.Error(e)
					}
				})
				if _, e = biz.store.ChangeBusinessPublicPermission(biz.ctx, biz.who(0, id), publish); e != nil {
					t.Fatal(e)
				}
			case "opportunity":
				id = resultNativeOwnIntent(t, f) + ":" + f.private
			}
			a, q := resultNativeRequest(t, f, kind)
			r, e := b.store.ReadAgentResultProjection(b.ctx, a, q)
			if e != nil {
				t.Fatal("native seven", kind, e)
			}
			found := false
			for _, item := range r.Items {
				if item.Entity.ID != id {
					continue
				}
				found = true
				if len(item.Actions) != 6 || item.ActionsValidUntil == nil || item.ActionsValidUntil.After(r.ObservedAt.Add(30*time.Second)) {
					t.Fatal("actual native action contract missing/unbounded", kind)
				}
				for _, action := range item.Actions {
					wanted := item.Entity
					if kind == "opportunity" {
						wanted = *item.Detail
					}
					if action.Target.Type != wanted.Type || action.Target.ID != wanted.ID {
						t.Fatal("action substituted original target", kind, action.Kind)
					}
					if kind == "opportunity" && wanted.ID == f.private && action.Kind == ea.Save && action.State != ea.Unavailable {
						t.Fatal("private opportunity widened Save")
					}
				}
				if kind == "business" && item.Summary != "明确批准才可公开的合成介绍" {
					t.Fatal("exact072 approved profile not consumed", item)
				}
				if item.Entity.Type != kind || item.Detail == nil || item.Share == nil {
					t.Fatal("stable typed domain ref", item)
				}
				if kind == "opportunity" {
					if item.Scope != arp.SelfPrivate || item.Detail.ID != f.private || item.Detail.Type != "activity" || *item.Share != *item.Detail {
						t.Fatal("private candidate became public/new activity", item)
					}
				} else if item.Detail.ID != id || *item.Share != item.Entity {
					t.Fatal("detail/share ref diverged", item)
				}
			}
			if !found {
				t.Fatal("missing original source", kind, id, r.Items)
			}
			wire, _ := json.Marshal(r.Items)
			for _, private := range []string{"PRIVATE_RIGHTS_CANARY", "PRIVATE_SOURCE_CANARY", "PRIVATE_INTENT_BODY_MUST_NOT_APPEAR", "不可复制未请求的详情", "UNREQUESTED_ACTIVITY_BODY_CANARY"} {
				if strings.Contains(string(wire), private) {
					t.Fatal("private field escaped", private)
				}
			}
			if e = b.store.RevalidateAgentResultProjection(b.ctx, a, q, r); e != nil {
				t.Fatal("original native source not stable", e)
			}
			switch kind {
			case "person":
				b.exec(`UPDATE user_profiles SET bio=bio WHERE account_id=$1`, id)
			case "activity":
				b.exec(`UPDATE activities SET title=title WHERE id=$1`, id)
			case "place":
				b.exec(`UPDATE places SET summary=summary WHERE id=$1`, id)
			case "community":
				b.exec(`UPDATE communities SET summary=summary WHERE id=$1`, id)
			case "organization":
				b.exec(`UPDATE organizations SET description=description WHERE id=$1`, id)
			case "business":
				b.exec(`UPDATE business_memberships SET updated_at=updated_at WHERE business_id=$1`, id)
			case "opportunity":
				b.exec(`UPDATE social_intents SET updated_at=updated_at WHERE id=$1`, strings.Split(id, ":")[0])
			}
			if e = b.store.RevalidateAgentResultProjection(b.ctx, a, q, r); !errors.Is(e, arp.ErrChanged) {
				t.Fatal("concrete original source ABA accepted", kind, e)
			}
			// Initial reads enforce source visibility too, rather than merely
			// detecting a change relative to an already issued receipt.
			switch kind {
			case "person", "community":
				b.exec(`INSERT INTO account_blocks(blocker_account_id,blocked_account_id) VALUES($1,$2)`, b.other.ID, b.person.ID)
				t.Cleanup(func() {
					b.pool.Exec(context.Background(), `DELETE FROM account_blocks WHERE blocker_account_id=$1 AND blocked_account_id=$2`, b.other.ID, b.person.ID)
				})
			case "activity":
				b.exec(`UPDATE activities SET publication_status='hidden' WHERE id=$1`, id)
			case "place":
				b.exec(`UPDATE places SET publication_status='hidden' WHERE id=$1`, id)
			case "organization":
				b.exec(`UPDATE organization_map_locations SET review_status='rejected' WHERE organization_id=$1`, id)
			case "business":
				b.exec(`UPDATE business_public_profile_permissions SET state='revoked',version=version+1 WHERE business_id=$1`, id)
			case "opportunity":
				b.exec(`UPDATE activity_invitations SET status='revoked' WHERE activity_id=$1 AND invitee_account_id=$2`, f.private, b.person.ID)
			}
			current, e := b.store.ReadAgentResultProjection(b.ctx, a, q)
			if e != nil {
				t.Fatal("current initial ACL projection", kind, e)
			}
			for _, item := range current.Items {
				if item.Entity.ID == id && (kind != "business" || item.Summary != "") {
					t.Fatal("hidden, blocked, revoked or unpublished source remained exposed", kind, item)
				}
			}
		})
	}
}

func TestAgentResultProjectionNativePrivateOpportunitySourceABA(t *testing.T) {
	for _, change := range []string{"activity", "host", "place", "invite"} {
		t.Run(change, func(t *testing.T) {
			f := contextBuilderNative(t)
			b := f.place.private.base
			own := resultNativeOwnIntent(t, f)
			a, q := resultNativeRequest(t, f, "opportunity")
			r, e := b.store.ReadAgentResultProjection(b.ctx, a, q)
			if e != nil {
				t.Fatal(e)
			}
			found := false
			for _, item := range r.Items {
				found = found || item.Entity.ID == own+":"+f.private
			}
			if !found {
				t.Fatal("real invited nonpublic opportunity prerequisite absent")
			}
			switch change {
			case "activity":
				b.exec(`UPDATE activities SET title=title WHERE id=$1`, f.private)
			case "host":
				b.exec(`UPDATE accounts SET status=status WHERE id=$1`, b.other.ID)
			case "place":
				b.exec(`UPDATE places SET summary=summary WHERE id=$1`, f.place.place)
			case "invite":
				b.exec(`UPDATE activity_invitations SET status=status WHERE activity_id=$1 AND invitee_account_id=$2`, f.private, b.person.ID)
			}
			if e = b.store.RevalidateAgentResultProjection(b.ctx, a, q, r); !errors.Is(e, arp.ErrChanged) {
				t.Fatal("private source same-value ABA accepted", change, e)
			}
		})
	}
}

func TestAgentResultProjectionNativeTaskAndSealAreCurrent(t *testing.T) {
	f := contextBuilderNative(t)
	b := f.place.private.base
	f.task.Intent = agentworkspace.FindPlace
	f.task.Filters = map[string]string{"currentQuery": "查询公开地点", "timePreference": "anytime"}
	var updateErr error
	f.task, updateErr = b.store.UpdateTask(b.ctx, f.task)
	if updateErr != nil {
		t.Fatal(updateErr)
	}
	raw, _ := json.Marshal(agentworkspace.SanitizeTaskForResponse(f.task))
	a := arp.Access{Actor: identity.Actor{ID: b.person.ID, AccountType: "person"}, SessionDigest: f.place.private.owner.SessionDigest, TaskID: f.task.ID, ExpectedTask: raw}
	q := arp.Query{CityID: f.place.city, Kind: "place", TimePreference: "anytime", CompareIDs: []string{}}
	r, e := b.store.ReadAgentResultProjection(b.ctx, a, q)
	if e != nil {
		t.Fatal(e)
	}
	bad := r
	bad.Items = append([]arp.Item{}, r.Items...)
	bad.Items[0].Title = "forged"
	if e = b.store.RevalidateAgentResultProjection(b.ctx, a, q, bad); !errors.Is(e, arp.ErrDenied) {
		t.Fatal("unsealed view accepted", e)
	}
	bad = r
	bad.Places = append([]foundation.Place{}, r.Places...)
	bad.Places[0].AddressLabel = "forged compatibility address"
	if e = b.store.RevalidateAgentResultProjection(b.ctx, a, q, bad); !errors.Is(e, arp.ErrDenied) {
		t.Fatal("unsealed compatibility DTO accepted", e)
	}
	b.exec(`UPDATE agent_tasks SET updated_at=updated_at WHERE id=$1`, f.task.ID)
	if e = b.store.RevalidateAgentResultProjection(b.ctx, a, q, r); !errors.Is(e, arp.ErrChanged) {
		t.Fatal("task xmin changed", e)
	}
}

func resultSponsorRequest(t *testing.T, f *sponsorFixture) (arp.Access, arp.Query) {
	t.Helper()
	f.b.exec(t, `INSERT INTO agents(agent_type,principal_account_id) VALUES('personal',$1)`, f.b.people[0])
	t.Cleanup(func() {
		for _, q := range []string{`DELETE FROM agent_profiles WHERE owner_id=$1`, `DELETE FROM agents WHERE principal_account_id=$1`} {
			if _, e := f.b.pool.Exec(context.Background(), q, f.b.people[0]); e != nil {
				t.Error(e)
			}
		}
	})
	f.b.exec(t, `INSERT INTO city_contexts(city_id) VALUES($1) ON CONFLICT DO NOTHING`, f.city)
	t.Cleanup(func() {
		for _, q := range []string{`DELETE FROM contexts WHERE city_id=$1`, `DELETE FROM city_contexts WHERE city_id=$1`} {
			if _, e := f.b.pool.Exec(context.Background(), q, f.city); e != nil {
				t.Error(e)
			}
		}
	})
	task, e := f.b.store.SaveTask(f.b.ctx, agentworkspace.Task{PrincipalType: "person", PrincipalID: f.b.people[0], ActingUserID: f.b.people[0], CityID: f.city, Intent: agentworkspace.FindActivity, Query: "本人明确当前活动查询", Status: agentworkspace.TaskCompleted, Filters: map[string]string{"currentQuery": "本人明确当前活动查询", "timePreference": "anytime"}, Conversation: []agentworkspace.Message{}})
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() {
		if _, e := f.b.pool.Exec(context.Background(), `DELETE FROM agent_tasks WHERE id=$1`, task.ID); e != nil {
			t.Error(e)
		}
	})
	raw, _ := json.Marshal(agentworkspace.SanitizeTaskForResponse(task))
	return arp.Access{Actor: identity.Actor{ID: f.b.people[0], AccountType: "person"}, SessionDigest: f.access[0].SessionDigest, TaskID: task.ID, ExpectedTask: raw}, arp.Query{CityID: f.city, Kind: "activity", TimePreference: "anytime", CompareIDs: []string{}}
}
func TestAgentResultProjectionNativeCommercialTailAfterRealSessionWait(t *testing.T) {
	for _, change := range []string{"reviewGrantABA", "withdraw", "reviewExpiry"} {
		t.Run(change, func(t *testing.T) {
			f := newSponsorFixture(t, false)
			if change == "reviewExpiry" {
				f.b.exec(t, `UPDATE sponsored_opportunity_review_grants SET valid_until=clock_timestamp()+interval '1200 milliseconds' WHERE city_id=$1`, f.city)
			}
			f.review(t, f.submit(t, "ACTIVITY").ID)
			a, q := resultSponsorRequest(t, f)
			r, e := f.b.store.ReadAgentResultProjection(f.b.ctx, a, q)
			if e != nil {
				t.Fatal(e)
			}
			targets := f.targets()
			disclosure, e := f.b.store.ReadSponsoredOpportunities(f.b.ctx, f.access[0], targets)
			if e != nil || len(disclosure.SponsoredOpportunities) != 1 {
				t.Fatal("real approved sponsorship prerequisite", e, disclosure)
			}
			cfg := f.b.pool.Config().Copy()
			name := "result-final-" + f.business
			cfg.ConnConfig.RuntimeParams["application_name"] = name
			pool, e := pgxpool.NewWithConfig(f.b.ctx, cfg)
			if e != nil {
				t.Fatal(e)
			}
			defer pool.Close()
			store := New(pool, false)
			lock, e := f.b.pool.Begin(f.b.ctx)
			if e != nil {
				t.Fatal(e)
			}
			defer lock.Rollback(context.Background())
			if _, e = lock.Exec(f.b.ctx, `SELECT id FROM sessions WHERE token_sha256=$1 FOR UPDATE`, a.SessionDigest[:]); e != nil {
				t.Fatal(e)
			}
			done := make(chan error, 1)
			go func() { done <- store.RevalidateAgentResultProjection(f.b.ctx, a, q, r) }()
			until := time.Now().Add(4 * time.Second)
			waiting := false
			for time.Now().Before(until) {
				if e = f.b.pool.QueryRow(f.b.ctx, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE application_name=$1 AND wait_event_type='Lock')`, name).Scan(&waiting); e != nil {
					t.Fatal(e)
				}
				if waiting {
					break
				}
				time.Sleep(5 * time.Millisecond)
			}
			if !waiting {
				t.Fatal("native Session tail wait not observed")
			}
			switch change {
			case "reviewGrantABA":
				f.b.exec(t, `UPDATE sponsored_opportunity_review_grants SET state=state WHERE city_id=$1`, f.city)
			case "withdraw":
				f.b.exec(t, `UPDATE sponsored_opportunity_review_grants SET state='revoked' WHERE city_id=$1`, f.city)
			case "reviewExpiry":
				var expired bool
				for !expired {
					if e = f.b.pool.QueryRow(f.b.ctx, `SELECT clock_timestamp()>=valid_until FROM sponsored_opportunity_review_grants WHERE city_id=$1`, f.city).Scan(&expired); e != nil {
						t.Fatal(e)
					}
					if !expired {
						time.Sleep(5 * time.Millisecond)
					}
				}
			}
			if e = lock.Commit(f.b.ctx); e != nil {
				t.Fatal(e)
			}
			select {
			case e = <-done:
				if !errors.Is(e, arp.ErrChanged) {
					t.Fatal("stale commercial buffer would escape", change, e)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("native revalidation stuck")
			}
		})
	}
}
func TestAgentResultProjectionNativeBusinessVenueInitialACL(t *testing.T) {
	for _, change := range []string{"valid", "relationRevoked", "venueExpired", "candidateWithdrawn", "operatorHidden"} {
		t.Run(change, func(t *testing.T) {
			f := newSponsorFixture(t, true)
			f.b.exec(t, `UPDATE places SET latitude=0,longitude=0,coordinate_system='wgs84',location_precision='point' WHERE id=$1`, f.place)
			switch change {
			case "relationRevoked":
				f.b.exec(t, `UPDATE business_venue_relations SET status='revoked' WHERE business_id=$1 AND place_id=$2`, f.business, f.place)
			case "venueExpired":
				f.b.exec(t, `UPDATE venues SET expires_at=clock_timestamp()-interval '1 millisecond' WHERE place_id=$1`, f.place)
			case "candidateWithdrawn":
				f.b.exec(t, `UPDATE venue_candidates SET status='rejected' WHERE place_id=$1`, f.place)
			case "operatorHidden":
				f.b.exec(t, `UPDATE organizations SET status='suspended' WHERE id=$1`, f.operator)
			}
			a, q := resultSponsorRequest(t, f)
			r, e := f.b.store.ReadAgentResultProjection(f.b.ctx, a, q)
			if e != nil {
				t.Fatal(e)
			}
			found := false
			for _, item := range r.Items {
				if item.Entity.ID == f.activity {
					found = true
					if item.Anchor == nil || item.Anchor.PlaceID != f.place {
						t.Fatal("approved native Venue anchor absent", item)
					}
				}
			}
			if found != (change == "valid") {
				t.Fatal("initial native Venue source ignored", change, found)
			}
		})
	}
}

func TestAgentResultProjectionNativeFinalAfterPoolWaitAndSourceExpiry(t *testing.T) {
	f := contextBuilderNative(t)
	b := f.place.private.base
	a, q := resultNativeRequest(t, f, "activity")
	b.exec(`UPDATE activities SET expires_at=clock_timestamp()+interval '1200 milliseconds' WHERE id=$1`, f.private)
	r, e := b.store.ReadAgentResultProjection(b.ctx, a, q)
	if e != nil {
		t.Fatal(e)
	}
	cfg := b.pool.Config().Copy()
	cfg.MaxConns = 1
	cfg.MinConns = 0
	pool, e := pgxpool.NewWithConfig(b.ctx, cfg)
	if e != nil {
		t.Fatal(e)
	}
	defer pool.Close()
	held, e := pool.Acquire(b.ctx)
	if e != nil {
		t.Fatal(e)
	}
	released := false
	defer func() {
		if !released {
			held.Release()
		}
	}()
	store := New(pool, false)
	started := make(chan struct{})
	done := make(chan error, 1)
	go func() { close(started); done <- store.RevalidateAgentResultProjection(b.ctx, a, q, r) }()
	<-started
	select {
	case e = <-done:
		t.Fatal("expected native wait for the sole owned PG connection", e)
	case <-time.After(50 * time.Millisecond):
	}
	var expired bool
	for !expired {
		if e = b.pool.QueryRow(b.ctx, `SELECT clock_timestamp()>=expires_at FROM activities WHERE id=$1`, f.private).Scan(&expired); e != nil {
			t.Fatal(e)
		}
		if !expired {
			time.Sleep(5 * time.Millisecond)
		}
	}
	held.Release()
	released = true
	select {
	case e = <-done:
		if !errors.Is(e, arp.ErrChanged) {
			t.Fatal("pre-pool source clock reused", e)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("native pool wait did not end")
	}
}
func TestAgentResultProjectionNativeFinalCurrentSessionAfterLockExpiry(t *testing.T) {
	f := contextBuilderNative(t)
	b := f.place.private.base
	a, q := resultNativeRequest(t, f, "activity")
	b.exec(`WITH stamp AS MATERIALIZED(SELECT clock_timestamp()+interval '1000 milliseconds' AS at) UPDATE sessions SET expires_at=stamp.at,idle_expires_at=stamp.at FROM stamp WHERE token_sha256=$1`, a.SessionDigest[:])
	r, e := b.store.ReadAgentResultProjection(b.ctx, a, q)
	if e != nil {
		t.Fatal(e)
	}
	cfg := b.pool.Config().Copy()
	name := "result-session-expiry-" + a.TaskID
	cfg.ConnConfig.RuntimeParams["application_name"] = name
	pool, e := pgxpool.NewWithConfig(b.ctx, cfg)
	if e != nil {
		t.Fatal(e)
	}
	defer pool.Close()
	store := New(pool, false)
	lock, e := b.pool.Begin(b.ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer lock.Rollback(context.Background())
	if _, e = lock.Exec(b.ctx, `SELECT id FROM sessions WHERE token_sha256=$1 FOR UPDATE`, a.SessionDigest[:]); e != nil {
		t.Fatal(e)
	}
	done := make(chan error, 1)
	go func() { done <- store.RevalidateAgentResultProjection(b.ctx, a, q, r) }()
	until := time.Now().Add(4 * time.Second)
	waiting := false
	for time.Now().Before(until) {
		if e = b.pool.QueryRow(b.ctx, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE application_name=$1 AND wait_event_type='Lock')`, name).Scan(&waiting); e != nil {
			t.Fatal(e)
		}
		if waiting {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if !waiting {
		t.Fatal("actual Session lock wait not observed")
	}
	var expired bool
	for !expired {
		if e = b.pool.QueryRow(b.ctx, `SELECT clock_timestamp()>=expires_at FROM sessions WHERE token_sha256=$1`, a.SessionDigest[:]).Scan(&expired); e != nil {
			t.Fatal(e)
		}
		if !expired {
			time.Sleep(5 * time.Millisecond)
		}
	}
	if e = lock.Commit(b.ctx); e != nil {
		t.Fatal(e)
	}
	select {
	case e = <-done:
		if !errors.Is(e, identity.ErrUnauthorized) {
			t.Fatal("expired Session survived final lock wait", e)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("final Session guard stuck")
	}
}
