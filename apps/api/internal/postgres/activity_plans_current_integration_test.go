package postgres

import (
	"context"
	"errors"
	"github.com/birdtie/birdtie/apps/api/internal/activityplan"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	"testing"
	"time"
)

func TestPlansCurrentNativeOnlineStableIDsAndExpiryNeutral(t *testing.T) {
	f := participationDisclosureNative(t)
	b := f.f
	b.exec(`UPDATE activities SET modality='online',physical_place_status='not_applicable' WHERE id=$1`, f.activity)
	id, e := b.s.PlanActivity(b.ctx, b.a.WorkspacePrincipal.ID, f.activity)
	if e != nil {
		t.Fatal(e)
	}
	a := activityplan.CurrentAccess{OwnerID: b.a.WorkspacePrincipal.ID, SessionDigest: b.a.SessionDigest}
	plans, check, e := b.s.ListActivityPlansCurrent(b.ctx, a)
	if e != nil {
		t.Fatal(e)
	}
	if len(plans) != 1 || plans[0].ID != id || plans[0].ActivityID != f.activity || plans[0].Modality != "online" || plans[0].PhysicalPlaceStatus != "not_applicable" || plans[0].PlaceName != "" || plans[0].StartsAt == nil || plans[0].TimeZone != "Europe/London" || check(b.ctx) != nil {
		t.Fatal("native online reminder facts")
	}
	joined, validate, e := b.s.ListParticipationsCurrent(b.ctx, a)
	if e != nil || len(joined) != 1 || joined[0].ID != f.p || joined[0].Modality != "online" || joined[0].Status != "going" || validate(b.ctx) != nil {
		t.Fatal("original participation", e)
	}
	// Ordinary Plans remain available without any active Agent or Profile.
	b.exec(`UPDATE agents SET status='retired' WHERE principal_account_id=$1`, a.OwnerID)
	if _, check, e = b.s.ListActivityPlansCurrent(b.ctx, a); e != nil || check(b.ctx) != nil {
		t.Fatal("Agent prerequisite added", e)
	}
	b.exec(`UPDATE activities SET expires_at=clock_timestamp()-interval '1 second' WHERE id=$1`, f.activity)
	if !errors.Is(validate(b.ctx), activityplan.ErrChanged) {
		t.Fatal("old source leaked after expiry")
	}
	plans, check, e = b.s.ListActivityPlansCurrent(b.ctx, a)
	if e != nil || len(plans) != 1 || plans[0].ID != id || plans[0].Available || plans[0].Title != "" || plans[0].CityID != "" || plans[0].StartsAt != nil || plans[0].Modality != "" || plans[0].PlaceName != "" || check(b.ctx) != nil {
		t.Fatal("hidden stable reminder neutrality", e)
	}
	joined, validate, e = b.s.ListParticipationsCurrent(b.ctx, a)
	if e != nil || len(joined) != 1 || joined[0].ID != f.p || joined[0].Available || joined[0].Title != "" || joined[0].StartsAt != nil || joined[0].Modality != "" || validate(b.ctx) != nil {
		t.Fatal("expired native RSVP neutral", e)
	}
}

func TestPlansCurrentNativeCurrentSessionSourceABAAndDeadline(t *testing.T) {
	for _, kind := range []string{"source_aba", "city_aba", "session_revoked", "city_natural_expiry", "activity_natural_expiry", "cross_owner"} {
		t.Run(kind, func(t *testing.T) {
			f := participationDisclosureNative(t)
			b := f.f
			if _, e := b.s.PlanActivity(b.ctx, b.a.WorkspacePrincipal.ID, f.activity); e != nil {
				t.Fatal(e)
			}
			a := activityplan.CurrentAccess{OwnerID: b.a.WorkspacePrincipal.ID, SessionDigest: b.a.SessionDigest}
			var deadline time.Time
			if kind == "city_natural_expiry" {
				if e := b.row(`UPDATE cities SET expires_at=clock_timestamp()+interval '600 milliseconds' WHERE id='aberdeen-gb' RETURNING expires_at`).Scan(&deadline); e != nil {
					t.Fatal(e)
				}
			}
			if kind == "activity_natural_expiry" {
				if e := b.row(`UPDATE activities SET expires_at=clock_timestamp()+interval '600 milliseconds' WHERE id=$1 RETURNING expires_at`, f.activity).Scan(&deadline); e != nil {
					t.Fatal(e)
				}
			}
			plans, check, e := b.s.ListActivityPlansCurrent(b.ctx, a)
			if e != nil || len(plans) != 1 || !plans[0].Available {
				t.Fatal("actual native initial frame", e)
			}
			want := activityplan.ErrChanged
			switch kind {
			case "source_aba":
				b.exec(`UPDATE activities SET publication_status='hidden' WHERE id=$1`, f.activity)
				b.exec(`UPDATE activities SET publication_status='published' WHERE id=$1`, f.activity)
			case "city_aba":
				b.exec(`UPDATE cities SET publication_status='hidden' WHERE id='aberdeen-gb'`)
				b.exec(`UPDATE cities SET publication_status='published' WHERE id='aberdeen-gb'`)
			case "session_revoked":
				b.exec(`UPDATE sessions SET revoked_at=clock_timestamp() WHERE id=$1`, b.session)
				want = identity.ErrUnauthorized
			case "cross_owner":
				a.OwnerID = b.other.WorkspacePrincipal.ID
				out, _, err := b.s.ListActivityPlansCurrent(b.ctx, a)
				if !errors.Is(err, identity.ErrUnauthorized) || out != nil {
					t.Fatal("cross-owner source", err)
				}
				return
			default:
				for {
					var passed bool
					if e := b.row(`SELECT clock_timestamp()>=$1`, deadline).Scan(&passed); e != nil {
						t.Fatal(e)
					}
					if passed {
						break
					}
					time.Sleep(10 * time.Millisecond)
				}
			}
			if e = check(b.ctx); !errors.Is(e, want) {
				t.Fatal("late native validation", kind, e, want)
			}
		})
	}
}

func TestPlansCurrentNativeFinalRelationWaitRejectsSessionRevocation(t *testing.T) {
	f := participationDisclosureNative(t)
	b := f.f
	if _, e := b.s.PlanActivity(b.ctx, b.a.WorkspacePrincipal.ID, f.activity); e != nil {
		t.Fatal(e)
	}
	a := activityplan.CurrentAccess{OwnerID: b.a.WorkspacePrincipal.ID, SessionDigest: b.a.SessionDigest}
	_, check, e := b.s.ListActivityPlansCurrent(b.ctx, a)
	if e != nil {
		t.Fatal(e)
	}
	lock, e := b.pool.Begin(b.ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer lock.Rollback(context.Background())
	if _, e = lock.Exec(b.ctx, `LOCK TABLE activity_plans IN ACCESS EXCLUSIVE MODE`); e != nil {
		t.Fatal(e)
	}
	result := make(chan error, 1)
	ctx, cancel := context.WithCancel(b.ctx)
	defer cancel()
	go func() { result <- check(ctx) }()
	waiting := false
	for n := 0; n < 300; n++ {
		e = b.pool.QueryRow(b.ctx, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity s JOIN pg_locks l ON l.pid=s.pid
   WHERE s.datname=current_database() AND $1::int=ANY(pg_blocking_pids(s.pid)) AND NOT l.granted
   AND l.relation='activity_plans'::regclass AND s.query LIKE '%own_plans_current%')`, int(lock.Conn().PgConn().PID())).Scan(&waiting)
		if e != nil {
			t.Fatal(e)
		}
		if waiting {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !waiting {
		t.Fatal("exact owned plans current relation barrier absent")
	}
	b.exec(`UPDATE sessions SET revoked_at=clock_timestamp() WHERE id=$1`, b.session)
	if e = lock.Commit(b.ctx); e != nil {
		t.Fatal(e)
	}
	if e = <-result; !errors.Is(e, identity.ErrUnauthorized) {
		t.Fatal("final relation wait released old Session", e)
	}
}

func TestPlansCurrentNativePublishedPlaceFactsAndHiddenPlaceNeutral(t *testing.T) {
	f := participationDisclosureNative(t)
	b := f.f
	var id string
	if e := b.row(`INSERT INTO places(id,city_id,name,category_code,publication_status,source_label,source_ref,maintainer_label)
 VALUES(gen_random_uuid(),'aberdeen-gb','公开的合成球馆','sports','published','合成','local:PLN','合成') RETURNING id`).Scan(&id); e != nil {
		t.Fatal(e)
	}
	b.exec(`UPDATE activities SET place_id=$1,modality='in_person',physical_place_status='confirmed' WHERE id=$2`, id, f.activity)
	if _, e := b.s.PlanActivity(b.ctx, b.a.WorkspacePrincipal.ID, f.activity); e != nil {
		t.Fatal(e)
	}
	a := activityplan.CurrentAccess{OwnerID: b.a.WorkspacePrincipal.ID, SessionDigest: b.a.SessionDigest}
	v, check, e := b.s.ListActivityPlansCurrent(b.ctx, a)
	if e != nil || len(v) != 1 || v[0].PlaceID != id || v[0].PlaceName != "公开的合成球馆" || v[0].Modality != "in_person" || check(b.ctx) != nil {
		t.Fatal("public place original source facts", e)
	}
	b.exec(`UPDATE places SET publication_status='hidden',name='PRIVATE_PLACE_CANARY' WHERE id=$1`, id)
	if !errors.Is(check(b.ctx), activityplan.ErrChanged) {
		t.Fatal("old Place frame survived privacy change")
	}
	v, check, e = b.s.ListActivityPlansCurrent(b.ctx, a)
	if e != nil || len(v) != 1 || !v[0].Available || v[0].PlaceID != "" || v[0].PlaceName != "" || check(b.ctx) != nil {
		t.Fatal("private place leaked or original Activity lost", e)
	}
}
