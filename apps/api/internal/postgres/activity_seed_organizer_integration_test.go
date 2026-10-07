package postgres

import (
	"context"
	"errors"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/actorref"
	"github.com/birdtie/birdtie/apps/api/internal/cityseed"
	"github.com/birdtie/birdtie/apps/api/internal/community"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestCityCandidateOrganizerNativePublication(t *testing.T) {
	if os.Getenv("BIRDTIE_DATABASE_URL") == "" {
		t.Skip("requires actual disposable native PostgreSQL")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, os.Getenv("BIRDTIE_DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	people, cleanup := ownedSocialPeopleFixture(t, ctx, pool, 3)
	defer cleanup()
	store := New(pool, false)
	var candidates, activities, communities, principals, cities []string
	exec := func(sql string, values ...any) {
		t.Helper()
		if _, e := pool.Exec(ctx, sql, values...); e != nil {
			t.Fatal(e)
		}
	}
	defer func() {
		for _, sql := range []string{
			`DELETE FROM inbox_items WHERE recipient_account_id=ANY($1::uuid[])`,
			`DELETE FROM audit_events WHERE actor_account_id=ANY($1::uuid[])`,
			`DELETE FROM city_editor_memberships WHERE account_id=ANY($1::uuid[])`,
		} {
			ownedSocialFixtureCleanup(t, ctx, pool, sql, people)
		}
		for _, sql := range []string{`DELETE FROM activity_sources WHERE candidate_id=ANY($1::uuid[])`, `DELETE FROM activity_candidates WHERE id=ANY($1::uuid[])`} {
			ownedSocialFixtureCleanup(t, ctx, pool, sql, candidates)
		}
		for _, sql := range []string{`DELETE FROM city_seed_activities WHERE activity_id=ANY($1::uuid[])`, `DELETE FROM activities WHERE id=ANY($1::uuid[])`} {
			ownedSocialFixtureCleanup(t, ctx, pool, sql, activities)
		}
		ownedSocialFixtureCleanup(t, ctx, pool, `DELETE FROM communities WHERE id=ANY($1::uuid[])`, communities)
		ownedSocialFixtureCleanup(t, ctx, pool, `DELETE FROM cities WHERE id=ANY($1::text[])`, cities)
		ownedSocialFixtureCleanup(t, ctx, pool, `DELETE FROM business_memberships WHERE business_id IN (SELECT id FROM businesses WHERE account_id=ANY($1::uuid[]))`, principals)
		ownedSocialFixtureCleanup(t, ctx, pool, `DELETE FROM businesses WHERE account_id=ANY($1::uuid[])`, principals)
		ownedSocialFixtureCleanup(t, ctx, pool, `DELETE FROM accounts WHERE id=ANY($1::uuid[])`, principals)
	}()
	exec(`INSERT INTO city_editor_memberships(city_id,account_id,role) VALUES('aberdeen-gb',$1,'contributor'),('aberdeen-gb',$2,'reviewer')`, people[0], people[1])
	makeInput := func(ref *actorref.ActorRef) cityseed.ActivityInput {
		return cityseed.ActivityInput{Organizer: ref, Title: "合成审核活动", Summary: "隔离测试", HostLabel: "明确提交的公开主办方标签", StartsAt: time.Now().UTC().Add(48 * time.Hour), EndsAt: time.Now().UTC().Add(49 * time.Hour), TimeZone: "Europe/London", SourceLabel: "合成授权来源", SourceURL: "https://example.invalid/local-test", RightsNote: "仅隔离验收数据，不作真实授权声明", ExpiresAt: time.Now().UTC().Add(96 * time.Hour)}
	}
	submit := func(ref *actorref.ActorRef) cityseed.ActivityCandidate {
		t.Helper()
		c, e := store.SubmitActivity(ctx, people[0], "aberdeen-gb", makeInput(ref))
		if e != nil {
			t.Fatal(e)
		}
		candidates = append(candidates, c.ID)
		return c
	}
	review := cityseed.ActivityReviewInput{Decision: "publish", Note: "合成隔离库审核，不表示真实活动授权"}
	t.Run("legacy_nil_is_pending_and_rejectable", func(t *testing.T) {
		c := submit(nil)
		if _, e := store.ReviewActivity(ctx, people[1], c.ID, review); !errors.Is(e, cityseed.ErrOrganizerRequired) {
			t.Fatalf("missing organizer: %v", e)
		}
		var state string
		if e := pool.QueryRow(ctx, `SELECT status FROM activity_candidates WHERE id=$1`, c.ID).Scan(&state); e != nil || state != "pending" {
			t.Fatal(state, e)
		}
		items, e := store.ListActivityCandidates(ctx, people[1], "aberdeen-gb")
		if e != nil {
			t.Fatal(e)
		}
		found := false
		for _, item := range items {
			if item.ID == c.ID {
				found = true
				if item.Organizer != nil {
					t.Fatal("inferred legacy organizer")
				}
			}
		}
		if !found {
			t.Fatal("legacy missing from pending list")
		}
		r, e := store.ReviewActivity(ctx, people[1], c.ID, cityseed.ActivityReviewInput{Decision: "reject", Note: review.Note})
		if e != nil || r.Status != "rejected" || r.Organizer != nil {
			t.Fatal(r, e)
		}
	})
	comm, e := store.CreateSocialCommunity(ctx, people[0], community.SocialInput{Name: "合成公开主办社群", Visibility: "public", JoinPolicy: "open", CityID: "aberdeen-gb"})
	if e != nil {
		t.Fatal(e)
	}
	communities = append(communities, comm.ID)
	var orgPrincipal, orgID, bizPrincipal, bizID string
	if e = pool.QueryRow(ctx, `INSERT INTO accounts(id,account_type) VALUES(gen_random_uuid(),'organization') RETURNING id`).Scan(&orgPrincipal); e != nil {
		t.Fatal(e)
	}
	principals = append(principals, orgPrincipal)
	if e = pool.QueryRow(ctx, `INSERT INTO organizations(account_id,organization_type,name,visibility) VALUES($1,'club','合成公开机构','public') RETURNING id`, orgPrincipal).Scan(&orgID); e != nil {
		t.Fatal(e)
	}
	exec(`INSERT INTO organization_memberships(organization_id,user_account_id,role) VALUES($1,$2,'owner')`, orgID, people[0])
	if e = pool.QueryRow(ctx, `INSERT INTO accounts(id,account_type) VALUES(gen_random_uuid(),'business') RETURNING id`).Scan(&bizPrincipal); e != nil {
		t.Fatal(e)
	}
	principals = append(principals, bizPrincipal)
	if e = pool.QueryRow(ctx, `INSERT INTO businesses(account_id,name,claim_status,claim_source_url,claim_reviewed_by,claim_reviewed_at) VALUES($1,'合成已审核商家','verified','https://example.invalid/claim',$2,clock_timestamp()) RETURNING id`, bizPrincipal, people[1]).Scan(&bizID); e != nil {
		t.Fatal(e)
	}
	exec(`INSERT INTO business_memberships(business_id,user_account_id,role) VALUES($1,$2,'owner')`, bizID, people[0])
	refs := []actorref.ActorRef{{Type: actorref.Person, ID: people[0]}, {Type: actorref.Community, ID: comm.ID}, {Type: actorref.Organization, ID: orgID}, {Type: actorref.Business, ID: bizID}}
	for _, ref := range refs {
		t.Run(string(ref.Type)+"_actual_submit_review_public_restart", func(t *testing.T) {
			c := submit(&ref)
			if c.Organizer == nil || !c.Organizer.Equal(ref) {
				t.Fatal("lost explicit selector", c.Organizer)
			}
			if _, e := store.ReviewActivity(ctx, people[0], c.ID, review); !errors.Is(e, cityseed.ErrForbidden) {
				t.Fatalf("submitter reviewer role forbidden: %v", e)
			}
			r, e := store.ReviewActivity(ctx, people[1], c.ID, review)
			if e != nil {
				t.Fatal(e)
			}
			activities = append(activities, r.ResolvedActivityID)
			if r.Status != "published" || r.Organizer == nil || !r.Organizer.Equal(ref) || r.ResolvedActivityID == "" {
				t.Fatal("wrong publication result", r)
			}
			public, e := store.GetActivity(ctx, r.ResolvedActivityID, people[2])
			if e != nil || public.Organizer.Type != string(ref.Type) || public.Organizer.ID != ref.ID {
				t.Fatal(public, e)
			}
			var host, source, rights, creator string
			var publishedAt *time.Time
			var target string
			if e = pool.QueryRow(ctx, `SELECT a.host_label,s.source_url,s.rights_note,a.created_by_account_id::text,a.host_account_id::text,a.published_at FROM activities a JOIN activity_sources s ON s.activity_id=a.id WHERE a.id=$1`, r.ResolvedActivityID).Scan(&host, &source, &rights, &creator, &target, &publishedAt); e != nil {
				t.Fatal(e)
			}
			if host != c.HostLabel || source != c.SourceURL || rights != c.RightsNote || creator != people[0] || target == people[1] || publishedAt == nil || publishedAt.IsZero() {
				t.Fatal("changed label/source or inferred reviewer host", host, creator, target)
			}
			if ref.Type == actorref.Organization && target != orgPrincipal || ref.Type == actorref.Business && target != bizPrincipal {
				t.Fatal("resource ID substituted for principal", target)
			}
			fresh, e := pgxpool.New(ctx, os.Getenv("BIRDTIE_DATABASE_URL"))
			if e != nil {
				t.Fatal(e)
			}
			defer fresh.Close()
			rebuilt := New(fresh, false)
			again, e := rebuilt.GetActivity(ctx, r.ResolvedActivityID, people[2])
			if e != nil || again.ID != r.ResolvedActivityID || again.Organizer.ID != ref.ID {
				t.Fatal(again, e)
			}
			if _, e = rebuilt.ReviewActivity(ctx, people[1], c.ID, review); !errors.Is(e, cityseed.ErrConflict) {
				t.Fatalf("duplicate review: %v", e)
			}
			var count int
			if e = pool.QueryRow(ctx, `SELECT count(*) FROM activity_sources WHERE candidate_id=$1`, c.ID).Scan(&count); e != nil || count != 1 {
				t.Fatal("duplicate publication", count, e)
			}
		})
	}
	t.Run("wrong_person_and_resource_namespace", func(t *testing.T) {
		for _, r := range []actorref.ActorRef{{Type: actorref.Person, ID: people[1]}, {Type: actorref.Organization, ID: orgPrincipal}, {Type: actorref.Business, ID: bizPrincipal}} {
			if _, e := store.SubmitActivity(ctx, people[0], "aberdeen-gb", makeInput(&r)); !errors.Is(e, cityseed.ErrOrganizerUnavailable) {
				t.Fatalf("unauthorized %+v: %v", r, e)
			}
		}
	})
	t.Run("current_manager_revoked_after_submit", func(t *testing.T) {
		ref := actorref.ActorRef{Type: actorref.Organization, ID: orgID}
		c := submit(&ref)
		exec(`UPDATE organization_memberships SET role='member',updated_at=clock_timestamp() WHERE organization_id=$1 AND user_account_id=$2`, orgID, people[0])
		defer exec(`UPDATE organization_memberships SET role='owner',updated_at=clock_timestamp() WHERE organization_id=$1 AND user_account_id=$2`, orgID, people[0])
		if _, e := store.ReviewActivity(ctx, people[1], c.ID, review); !errors.Is(e, cityseed.ErrOrganizerUnavailable) {
			t.Fatalf("revoked manager published: %v", e)
		}
	})
	t.Run("private_resource_not_published_by_city_reviewer", func(t *testing.T) {
		ref := actorref.ActorRef{Type: actorref.Community, ID: comm.ID}
		c := submit(&ref)
		exec(`UPDATE communities SET visibility='private' WHERE id=$1`, comm.ID)
		defer exec(`UPDATE communities SET visibility='public' WHERE id=$1`, comm.ID)
		if _, e := store.ReviewActivity(ctx, people[1], c.ID, review); !errors.Is(e, cityseed.ErrOrganizerUnavailable) {
			t.Fatalf("private community published: %v", e)
		}
	})
	t.Run("submitter_city_editor_withdrawn", func(t *testing.T) {
		ref := actorref.ActorRef{Type: actorref.Person, ID: people[0]}
		c := submit(&ref)
		exec(`UPDATE city_editor_memberships SET state='revoked',revoked_at=clock_timestamp() WHERE city_id='aberdeen-gb' AND account_id=$1`, people[0])
		defer exec(`UPDATE city_editor_memberships SET state='active',revoked_at=NULL WHERE city_id='aberdeen-gb' AND account_id=$1`, people[0])
		if _, e := store.ReviewActivity(ctx, people[1], c.ID, review); !errors.Is(e, cityseed.ErrOrganizerUnavailable) {
			t.Fatalf("withdrawn submission published: %v", e)
		}
	})
	t.Run("ordinary_native_members_cannot_choose_managed_host", func(t *testing.T) {
		// Keep each resource's real owner active; a member test must not disable
		// the existing last-owner invariant just to manufacture a fixture.
		exec(`INSERT INTO city_editor_memberships(city_id,account_id,role) VALUES('aberdeen-gb',$1,'contributor')`, people[2])
		for _, v := range []struct {
			ref           actorref.ActorRef
			table, target string
		}{{refs[1], "community_memberships", "community_id"}, {refs[2], "organization_memberships", "organization_id"}, {refs[3], "business_memberships", "business_id"}} {
			t.Run(string(v.ref.Type), func(t *testing.T) {
				exec(`INSERT INTO `+v.table+` (`+v.target+`,user_account_id,role) VALUES($1,$2,'member')`, v.ref.ID, people[2])
				if _, e := store.SubmitActivity(ctx, people[2], "aberdeen-gb", makeInput(&v.ref)); !errors.Is(e, cityseed.ErrOrganizerUnavailable) {
					t.Fatalf("ordinary member chose managed host: %v", e)
				}
			})
		}
	})
	t.Run("strict_native_binding_shape_and_immutability", func(t *testing.T) {
		c := submit(nil)
		for _, v := range []struct {
			name, sql string
			values    []any
		}{
			{"zero_targets", `INSERT INTO city_activity_candidate_organizers(candidate_id,selected_by) VALUES($1,$2)`, []any{c.ID, people[0]}},
			{"two_targets", `INSERT INTO city_activity_candidate_organizers(candidate_id,selected_by,person_account_id,community_id) VALUES($1,$2,$2,$3)`, []any{c.ID, people[0], comm.ID}},
			{"not_original_submitter", `INSERT INTO city_activity_candidate_organizers(candidate_id,selected_by,person_account_id) VALUES($1,$2,$2)`, []any{c.ID, people[1]}},
			{"not_explicit_self", `INSERT INTO city_activity_candidate_organizers(candidate_id,selected_by,person_account_id) VALUES($1,$2,$3)`, []any{c.ID, people[0], people[1]}},
			{"organization_principal_not_resource", `INSERT INTO city_activity_candidate_organizers(candidate_id,selected_by,organization_id) VALUES($1,$2,$3)`, []any{c.ID, people[0], orgPrincipal}},
			{"business_principal_not_resource", `INSERT INTO city_activity_candidate_organizers(candidate_id,selected_by,business_id) VALUES($1,$2,$3)`, []any{c.ID, people[0], bizPrincipal}},
			{"future_time", `INSERT INTO city_activity_candidate_organizers(candidate_id,selected_by,person_account_id,selected_at) VALUES($1,$2,$2,clock_timestamp()+interval '1 hour')`, []any{c.ID, people[0]}},
			{"infinite_time", `INSERT INTO city_activity_candidate_organizers(candidate_id,selected_by,person_account_id,selected_at) VALUES($1,$2,$2,'infinity')`, []any{c.ID, people[0]}},
		} {
			t.Run(v.name, func(t *testing.T) {
				if _, e := pool.Exec(ctx, v.sql, v.values...); e == nil {
					t.Fatal("invalid native selector accepted")
				}
			})
		}
		var count int
		if e := pool.QueryRow(ctx, `SELECT count(*) FROM city_activity_candidate_organizers WHERE candidate_id=$1`, c.ID).Scan(&count); e != nil || count != 0 {
			t.Fatal("negative selector persisted", count, e)
		}
		bound := submit(&refs[0])
		for _, sql := range []string{`UPDATE city_activity_candidate_organizers SET selected_at=selected_at WHERE candidate_id=$1`, `DELETE FROM city_activity_candidate_organizers WHERE candidate_id=$1`} {
			if _, e := pool.Exec(ctx, sql, bound.ID); e == nil {
				t.Fatal("binding changed independently of candidate")
			}
		}
	})
	t.Run("expired_city_cannot_reject_or_return_private_candidate", func(t *testing.T) {
		cityID := "city-organizer-" + people[0]
		exec(`INSERT INTO cities(id,name,region,country_code,time_zone,publication_status,source_label,source_ref,maintainer_label) VALUES($1,'合成城市','合成地区','GB','Europe/London','published','隔离来源','https://example.invalid/city','合成维护标签')`, cityID)
		cities = append(cities, cityID)
		exec(`INSERT INTO city_editor_memberships(city_id,account_id,role) VALUES($1,$2,'contributor'),($1,$3,'reviewer')`, cityID, people[0], people[1])
		c, e := store.SubmitActivity(ctx, people[0], cityID, makeInput(nil))
		if e != nil {
			t.Fatal(e)
		}
		candidates = append(candidates, c.ID)
		var before, after string
		if e = pool.QueryRow(ctx, `SELECT to_jsonb(c)::text FROM activity_candidates c WHERE id=$1`, c.ID).Scan(&before); e != nil {
			t.Fatal(e)
		}
		exec(`UPDATE cities SET expires_at=clock_timestamp()-interval '1 second' WHERE id=$1`, cityID)
		if items, e := store.ListActivityCandidates(ctx, people[1], cityID); !errors.Is(e, cityseed.ErrForbidden) || len(items) != 0 {
			t.Fatalf("expired city listed private candidates: %d %v", len(items), e)
		}
		if result, e := store.ReviewActivity(ctx, people[1], c.ID, cityseed.ActivityReviewInput{Decision: "reject", Note: "过期城市不应执行"}); !errors.Is(e, cityseed.ErrForbidden) || result.ID != "" {
			t.Fatalf("expired city reviewer wrote/received candidate: %s %v", result.Status, e)
		}
		if e = pool.QueryRow(ctx, `SELECT to_jsonb(c)::text FROM activity_candidates c WHERE id=$1`, c.ID).Scan(&after); e != nil || before != after {
			t.Fatalf("expired reject changed original candidate: %v", e)
		}
	})
}

type cityCandidateListTraceKey struct{}
type cityCandidateListTracer struct {
	entered, release chan struct{}
	once             sync.Once
}

func (p *cityCandidateListTracer) TraceQueryStart(ctx context.Context, _ *pgx.Conn, d pgx.TraceQueryStartData) context.Context {
	if strings.Contains(d.SQL, "FROM activity_candidates") && strings.Contains(d.SQL, "status = 'pending'") {
		return context.WithValue(ctx, cityCandidateListTraceKey{}, true)
	}
	return ctx
}
func (p *cityCandidateListTracer) TraceQueryEnd(ctx context.Context, _ *pgx.Conn, _ pgx.TraceQueryEndData) {
	if ctx.Value(cityCandidateListTraceKey{}) == true {
		p.once.Do(func() {
			close(p.entered)
			select {
			case <-p.release:
			case <-ctx.Done():
			}
		})
	}
}

// A real query/cursor boundary changes the native membership after the payload
// SELECT, before the caller can receive it. No fake auth resolver is injected.
func TestCityCandidateOrganizerCurrentListBoundary(t *testing.T) {
	if os.Getenv("BIRDTIE_DATABASE_URL") == "" {
		t.Skip("requires actual disposable native PostgreSQL")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, os.Getenv("BIRDTIE_DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	people, cleanup := ownedSocialPeopleFixture(t, ctx, pool, 2)
	defer cleanup()
	var candidateID string
	defer func() {
		ownedSocialFixtureCleanup(t, context.Background(), pool, `DELETE FROM audit_events WHERE actor_account_id=ANY($1::uuid[])`, people)
		if candidateID != "" {
			ownedSocialFixtureCleanup(t, context.Background(), pool, `DELETE FROM activity_candidates WHERE id=$1`, candidateID)
		}
		ownedSocialFixtureCleanup(t, context.Background(), pool, `DELETE FROM city_editor_memberships WHERE account_id=ANY($1::uuid[])`, people)
	}()
	if _, err = pool.Exec(ctx, `INSERT INTO city_editor_memberships(city_id,account_id,role) VALUES('aberdeen-gb',$1,'contributor'),('aberdeen-gb',$2,'reviewer')`, people[0], people[1]); err != nil {
		t.Fatal(err)
	}
	c, err := New(pool, false).SubmitActivity(ctx, people[0], "aberdeen-gb", cityseed.ActivityInput{Title: "合成旧候选", HostLabel: "明确合成外部线索标签", StartsAt: time.Now().UTC().Add(48 * time.Hour), EndsAt: time.Now().UTC().Add(49 * time.Hour), TimeZone: "Europe/London", SourceLabel: "隔离测试来源", SourceURL: "https://example.invalid/fixture", RightsNote: "仅合成测试", ExpiresAt: time.Now().UTC().Add(96 * time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	candidateID = c.ID
	tracer := &cityCandidateListTracer{entered: make(chan struct{}), release: make(chan struct{})}
	cfg, err := pgxpool.ParseConfig(os.Getenv("BIRDTIE_DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	cfg.ConnConfig.Tracer = tracer
	cfg.ConnConfig.RuntimeParams["default_transaction_isolation"] = "repeatable read"
	traced, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer traced.Close()
	type response struct {
		items []cityseed.ActivityCandidate
		err   error
	}
	done := make(chan response, 1)
	go func() {
		items, e := New(traced, false).ListActivityCandidates(ctx, people[1], "aberdeen-gb")
		done <- response{items, e}
	}()
	select {
	case <-tracer.entered:
	case <-ctx.Done():
		t.Fatal("actual candidate cursor boundary not reached")
	}
	_, err = pool.Exec(ctx, `UPDATE city_editor_memberships SET state='revoked',revoked_at=clock_timestamp() WHERE account_id=$1 AND city_id='aberdeen-gb'`, people[1])
	close(tracer.release)
	if err != nil {
		t.Fatal(err)
	}
	select {
	case r := <-done:
		if !errors.Is(r.err, cityseed.ErrForbidden) || len(r.items) != 0 {
			t.Fatalf("late revoked editor received candidate payload: items=%d error=%v", len(r.items), r.err)
		}
	case <-ctx.Done():
		t.Fatal("late boundary call did not finish")
	}
}
