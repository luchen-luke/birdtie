package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/birdtie/birdtie/apps/api/internal/activitypublish"
	"github.com/birdtie/birdtie/apps/api/internal/content"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	ph "github.com/birdtie/birdtie/apps/api/internal/placehistory"
	pp "github.com/birdtie/birdtie/apps/api/internal/placeprofile"
	"strings"
	"testing"
	"time"
)

func TestPlaceHistoryNativeRecentPublicBounded(t *testing.T) {
	f := placeMemoryNativeFixture(t)
	b := f.private.base
	a := momentPublicationOrdinary(t, f)
	for i := 0; i < 7; i++ {
		m := momentPublicationDraft(t, f, a)
		momentPublicationPublish(t, f, a, m)
	}
	got, e := b.store.GetPublicPlaceSocialHistory(b.ctx, ph.Access{}, f.place)
	if e != nil || got.RecentMomentCount != 7 || len(got.RecentMoments) != 5 || got.Suitability != nil {
		t.Fatal("current public bounded count/UNKNOWN", e, got)
	}
	// Raw alterations are negative source invalidation only, never publication
	// provenance or a positive social/attendance sample.
	b.exec(`UPDATE moments SET published_at=clock_timestamp()-interval '31 days',author_confirmed_at=clock_timestamp()-interval '32 days' WHERE author_account_id=$1`, a.actor.ID)
	got, e = b.store.GetPublicPlaceSocialHistory(b.ctx, ph.Access{}, f.place)
	if e != nil || got.RecentMomentCount != 0 {
		t.Fatal("old disclosure included", e)
	}
}
func TestPlaceHistoryNativeAuthorAndBlockPrivacy(t *testing.T) {
	for _, mode := range []string{"suspended_author", "block_by_reader", "block_by_author", "private", "withdrawn"} {
		t.Run(mode, func(t *testing.T) {
			f := placeMemoryNativeFixture(t)
			b := f.private.base
			a := momentPublicationOrdinary(t, f)
			reader := momentPublicationOrdinary(t, f)
			m := momentPublicationDraft(t, f, a)
			var r content.MomentPublicationReceipt
			if mode != "private" {
				r = momentPublicationPublish(t, f, a, m)
			}
			switch mode {
			case "suspended_author":
				b.exec(`UPDATE accounts SET status='suspended' WHERE id=$1`, a.actor.ID)
			case "block_by_reader":
				b.exec(`INSERT INTO account_blocks(blocker_account_id,blocked_account_id) VALUES($1,$2)`, reader.actor.ID, a.actor.ID)
			case "block_by_author":
				b.exec(`INSERT INTO account_blocks(blocker_account_id,blocked_account_id) VALUES($1,$2)`, a.actor.ID, reader.actor.ID)
			case "private":
				// Real ordinary private draft is never published, not an invalid
				// raw SQL published/private row that violates the native schema.
			case "withdrawn":
				if e := b.store.WithdrawHumanMoment(b.ctx, a.digest, a.actor, m.ID, r.Revision); e != nil {
					t.Fatal(e)
				}
			}
			got, e := b.store.GetPublicPlaceSocialHistory(b.ctx, ph.Access{Actor: reader.actor, SessionDigest: reader.digest}, f.place)
			if e != nil || got.RecentMomentCount != 0 || len(got.RecentMoments) != 0 {
				t.Fatal("forbidden source/aggregate leaked", e, got)
			}
		})
	}
}
func TestPlaceHistoryNativeReviewedSevenFields(t *testing.T) {
	f := semanticFixture(t)
	b := f.place.private.base
	c := f.published(t)
	got, e := b.store.GetPublicPlaceSocialHistory(b.ctx, ph.Access{}, f.place.place)
	if e != nil || got.Suitability == nil || pp.ValidatePublic(*got.Suitability, got.CheckedAt) != nil || !got.Suitability.CheckedAt.Equal(got.CheckedAt) || got.Suitability.Claims()["price"].State != "UNKNOWN" {
		t.Fatal("real approved source seven fields same final frame", e)
	}
	raw, _ := json.Marshal(got)
	for _, private := range []string{"rightsNote", "submittedBy", "reviewedBy", "longitude", "latitude", "agentNotes", "attendance"} {
		if strings.Contains(string(raw), private) {
			t.Fatal("private/reconstructed field", private)
		}
	}
	b.exec(`UPDATE city_editor_memberships SET state='revoked' WHERE city_id=$1 AND account_id=$2`, f.place.city, f.editor.ActorID)
	got, e = b.store.GetPublicPlaceSocialHistory(b.ctx, ph.Access{}, f.place.place)
	if e != nil || got.Suitability != nil {
		t.Fatal("revoked source permission retained old claims", e, c.ID)
	}
}
func TestPlaceHistoryNativeCurrentReviewedSourceAtTargetWait(t *testing.T) {
	for _, mode := range []string{"editor_revoked", "source_expiry"} {
		t.Run(mode, func(t *testing.T) {
			f := semanticFixture(t)
			b := f.place.private.base
			var c pp.Candidate
			var e error
			if mode == "source_expiry" {
				in := f.input(t)
				in.Source.ExpiresAt = f.now.Add(2 * time.Second)
				var created bool
				c, created, e = b.store.SubmitPlaceSemanticCandidate(b.ctx, f.editor, f.place.city, f.place.place, in)
				if e != nil || !created {
					t.Fatal("native short-lived source submit", e)
				}
				c, e = b.store.ReviewPlaceSemanticCandidate(b.ctx, f.reviewer, c.ID, pp.ReviewInput{Decision: "approve", CandidateVersion: 1, ExpectedVersion: 0, Note: "独立审核短期合成来源以检验自然失效"})
				if e != nil {
					t.Fatal("native short-lived source approve", e)
				}
			} else {
				c = f.published(t)
			}
			blocker, e := b.pool.Begin(b.ctx)
			if e != nil {
				t.Fatal(e)
			}
			defer blocker.Rollback(context.Background())
			var pid int
			if e = blocker.QueryRow(b.ctx, `SELECT pg_backend_pid() FROM places WHERE id=$1 FOR UPDATE`, f.place.place).Scan(&pid); e != nil {
				t.Fatal(e)
			}
			type result struct {
				out ph.Summary
				e   error
			}
			done := make(chan result, 1)
			go func() {
				out, e := b.store.GetPublicPlaceSocialHistory(b.ctx, ph.Access{}, f.place.place)
				done <- result{out, e}
			}()
			momentPublicationWait(t, f.place, pid, "FROM places")
			if mode == "editor_revoked" {
				b.exec(`UPDATE city_editor_memberships SET state='revoked' WHERE city_id=$1 AND account_id=$2`, f.place.city, f.editor.ActorID)
			} else {
				deadline := time.Now().Add(4 * time.Second)
				for {
					var expired bool
					if e = b.pool.QueryRow(b.ctx, `SELECT expires_at<=clock_timestamp() FROM place_semantic_candidates WHERE id=$1`, c.ID).Scan(&expired); e != nil {
						t.Fatal(e)
					}
					if expired {
						break
					}
					if time.Now().After(deadline) {
						t.Fatal("real approved source expiry not reached")
					}
					time.Sleep(20 * time.Millisecond)
				}
			}
			if e = blocker.Commit(b.ctx); e != nil {
				t.Fatal(e)
			}
			select {
			case r := <-done:
				if r.e != nil || r.out.Suitability != nil || r.out.PlaceID != f.place.place {
					t.Fatal("late old semantic source exposed or lost valid public target", r.e)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("approved source wait stuck")
			}
		})
	}
}
func TestPlaceHistoryNativeActualPublishedArrangements(t *testing.T) {
	f := placeMemoryNativeFixture(t)
	b := f.private.base
	a := momentPublicationOrdinary(t, f)
	var now time.Time
	if e := b.pool.QueryRow(b.ctx, `SELECT clock_timestamp()`).Scan(&now); e != nil {
		t.Fatal(e)
	}
	in := activitypublish.Input{CityID: f.city, PlaceID: f.place, Title: "真正原生发布的合成安排", Summary: "只有安排事实", StartsAt: now.Add(time.Second), EndsAt: now.Add(2 * time.Second), TimeZone: "Europe/London", CategoryCode: "badminton", Modality: "in_person", PhysicalPlaceStatus: "confirmed", Visibility: "public", Organizer: activitypublish.Organizer{Type: "PERSON", ID: a.actor.ID}}
	activity, e := b.store.CreateSocialDraft(b.ctx, a.actor.ID, in)
	if e != nil {
		t.Fatal("actual social draft", e)
	}
	t.Cleanup(func() {
		for _, q := range []string{`DELETE FROM activity_participations WHERE activity_id=$1`, `DELETE FROM activities WHERE id=$1`} {
			if _, e := b.pool.Exec(context.Background(), q, activity.ID); e != nil {
				t.Error(e)
			}
		}
	})
	activity, e = b.store.PublishSocialActivity(b.ctx, a.actor.ID, activity.ID)
	if e != nil {
		t.Fatal("actual social publish", e)
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		var ended bool
		if e = b.pool.QueryRow(b.ctx, `SELECT ends_at<=clock_timestamp() FROM activities WHERE id=$1`, activity.ID).Scan(&ended); e != nil {
			t.Fatal(e)
		}
		if ended {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("actual schedule did not end")
		}
		time.Sleep(15 * time.Millisecond)
	}
	got, e := b.store.GetPublicPlaceSocialHistory(b.ctx, ph.Access{}, f.place)
	if e != nil || len(got.ActivityPatterns) != 1 || got.ActivityPatterns[0].Arrangements != 1 || got.ActivityPatterns[0].Category != "badminton" {
		t.Fatal("public retained arrangement not read", e, got)
	}
	if _, e = b.pool.Exec(b.ctx, `UPDATE activities SET cancelled_at=clock_timestamp() WHERE id=$1`, activity.ID); e != nil {
		t.Fatal(e)
	}
	got, e = b.store.GetPublicPlaceSocialHistory(b.ctx, ph.Access{}, f.place)
	if e != nil || len(got.ActivityPatterns) != 0 {
		t.Fatal("cancelled arrangement remained", e)
	}
}
func TestPlaceHistoryNativeLateCurrentSessionAndTarget(t *testing.T) {
	for _, mode := range []string{"session_revoke", "session_expiry", "place_expiry", "city_hidden"} {
		t.Run(mode, func(t *testing.T) {
			f := placeMemoryNativeFixture(t)
			b := f.private.base
			a := momentPublicationOrdinary(t, f)
			if mode == "session_expiry" {
				b.exec(`UPDATE sessions SET expires_at=clock_timestamp()+interval '1500 milliseconds',idle_expires_at=clock_timestamp()+interval '1400 milliseconds' WHERE token_sha256=$1`, a.digest[:])
			}
			if mode == "place_expiry" {
				b.exec(`UPDATE places SET expires_at=clock_timestamp()+interval '1500 milliseconds' WHERE id=$1`, f.place)
			}
			blocker, e := b.pool.Begin(b.ctx)
			if e != nil {
				t.Fatal(e)
			}
			defer blocker.Rollback(context.Background())
			var pid int
			barrierSQL := `SELECT pg_backend_pid() FROM places WHERE id=$1 FOR UPDATE`
			barrierID := f.place
			if mode == "city_hidden" {
				barrierSQL = `SELECT pg_backend_pid() FROM cities WHERE id=$1 FOR UPDATE`
				barrierID = f.city
			}
			if e = blocker.QueryRow(b.ctx, barrierSQL, barrierID).Scan(&pid); e != nil {
				t.Fatal(e)
			}
			type result struct {
				out ph.Summary
				e   error
			}
			done := make(chan result, 1)
			go func() {
				out, e := b.store.GetPublicPlaceSocialHistory(b.ctx, ph.Access{Actor: a.actor, SessionDigest: a.digest}, f.place)
				done <- result{out, e}
			}()
			momentPublicationWait(t, f, pid, "FROM places")
			want := ph.ErrNotFound
			if mode == "session_revoke" {
				want = identity.ErrUnauthorized
				if e = b.store.RevokeSession(b.ctx, a.digest); e != nil {
					t.Fatal(e)
				}
			} else if mode == "city_hidden" {
				if _, e = blocker.Exec(b.ctx, `UPDATE cities SET publication_status='hidden' WHERE id=$1`, f.city); e != nil {
					t.Fatal(e)
				}
			} else {
				query := `SELECT expires_at<=clock_timestamp() FROM places WHERE id=$1`
				var param any = f.place
				if mode == "session_expiry" {
					want = identity.ErrUnauthorized
					query = `SELECT expires_at<=clock_timestamp() FROM sessions WHERE token_sha256=$1`
					param = a.digest[:]
				}
				deadline := time.Now().Add(4 * time.Second)
				for {
					var expired bool
					if e = b.pool.QueryRow(b.ctx, query, param).Scan(&expired); e != nil {
						t.Fatal(e)
					}
					if expired {
						break
					}
					if time.Now().After(deadline) {
						t.Fatal("real source/session clock expiry not reached")
					}
					time.Sleep(20 * time.Millisecond)
				}
			}
			if e = blocker.Commit(b.ctx); e != nil {
				t.Fatal(e)
			}
			select {
			case r := <-done:
				if !errors.Is(r.e, want) || r.out.PlaceID != "" {
					t.Fatal("late source authorization leaked", r.e, r.out)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("late source wait stuck")
			}
		})
	}
}
