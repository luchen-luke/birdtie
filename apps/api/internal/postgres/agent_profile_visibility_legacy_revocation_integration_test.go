package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/activitychat"
	"github.com/birdtie/birdtie/apps/api/internal/activitypublish"
	"github.com/birdtie/birdtie/apps/api/internal/agentprofile"
	"github.com/birdtie/birdtie/apps/api/internal/communitychat"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	"github.com/birdtie/birdtie/apps/api/internal/newpeople"
	"github.com/birdtie/birdtie/apps/api/internal/relationshipcontext"
	"github.com/birdtie/birdtie/apps/api/internal/socialcontext"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// The tracer exists only in this test's own pool. It pauses after a real
// preliminary SQL query closes, before the Store can begin its payload SQL.
// The separate writer commits a real revocation before release; no sleeps or
// production hooks are used to guess whether the read reached the race window.
type legacyProjectionTraceKey struct{}
type legacyProjectionGate struct {
	match   string
	entered chan struct{}
	release chan struct{}
	used    atomic.Bool
	close   sync.Once
}

func (g *legacyProjectionGate) TraceQueryStart(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	return context.WithValue(ctx, legacyProjectionTraceKey{}, data.SQL)
}

func (g *legacyProjectionGate) TraceQueryEnd(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryEndData) {
	query, _ := ctx.Value(legacyProjectionTraceKey{}).(string)
	if data.Err == nil && strings.Contains(query, g.match) && g.used.CompareAndSwap(false, true) {
		close(g.entered)
		select {
		case <-g.release:
		case <-ctx.Done():
		}
	}
}

func (g *legacyProjectionGate) resume() { g.close.Do(func() { close(g.release) }) }

type legacyRevocationFixture struct {
	f                     *agentVisibilityFixture
	endpoint              string
	name, activityID      string
	sourceID, candidateID string
	requestID, tieID      string
	cityID                string
}

func newLegacyRevocationFixture(t *testing.T, endpoint string, physical bool) *legacyRevocationFixture {
	t.Helper()
	r := &legacyRevocationFixture{f: agentVisibilityTestFixture(t), endpoint: endpoint}
	b := r.f.private.base
	r.name = "synthetic-legacy-current-name-" + b.person.ID
	b.exec(`UPDATE user_profiles SET display_name=$2 WHERE account_id=$1`, b.person.ID, r.name)
	b.exec(`UPDATE accounts SET handle=$2 WHERE id=$1`, b.person.ID, r.name)
	activities := []string{}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		for _, query := range []string{
			`DELETE FROM inbox_items WHERE recipient_account_id=ANY($1::uuid[])`,
			`DELETE FROM conversation_messages WHERE sender_account_id=ANY($1::uuid[])`,
			`DELETE FROM conversations WHERE member_a_account_id=ANY($1::uuid[]) OR member_b_account_id=ANY($1::uuid[])`,
			`DELETE FROM social_intents WHERE creator_account_id=ANY($1::uuid[])`,
			`DELETE FROM person_new_people_consent WHERE account_id=ANY($1::uuid[])`,
			`DELETE FROM person_agent_relationship_consent WHERE account_id=ANY($1::uuid[])`,
			`DELETE FROM person_social_disclosure WHERE account_id=ANY($1::uuid[])`,
		} {
			if _, err := b.pool.Exec(ctx, query, b.accounts); err != nil {
				t.Errorf("owned legacy revocation fixture cleanup failed: %v", err)
			}
		}
		if _, err := b.pool.Exec(ctx, `DELETE FROM activities WHERE id=ANY($1::uuid[])`, activities); err != nil {
			t.Errorf("owned legacy revocation activity cleanup failed: %v", err)
		}
		if r.cityID != "" {
			for _, query := range []string{
				`DELETE FROM contexts WHERE city_id=$1`, `DELETE FROM city_contexts WHERE city_id=$1`, `DELETE FROM cities WHERE id=$1`,
			} {
				if _, err := b.pool.Exec(ctx, query, r.cityID); err != nil {
					t.Errorf("owned legacy revocation scope cleanup failed: %v", err)
				}
			}
		}
	})
	if endpoint == "new_people" {
		if physical {
			r.cityID = "legacy-race-city-" + b.person.ID
			b.exec(`INSERT INTO cities(id,name,region,country_code,time_zone,publication_status,source_label,source_ref,maintainer_label)
				VALUES($1,'Synthetic isolated City','test','GB','Europe/London','published','Synthetic fixture','test-only','Synthetic')`, r.cityID)
			b.exec(`INSERT INTO city_contexts(city_id) VALUES($1)`, r.cityID)
		}
		for _, person := range []string{b.other.ID, b.person.ID} {
			if _, err := b.store.SetNewPeopleConsent(b.ctx, person, true); err != nil {
				t.Fatal(err)
			}
			input := newpeople.DraftInput{Title: "合成并发读取声明", Category: "synthetic-revocation", Modality: "ONLINE", ExpiresAt: time.Now().Add(time.Hour)}
			if physical {
				input.Modality, input.CityID, input.AreaLabel = "IN_PERSON", r.cityID, "合成明确区域"
			}
			draft, err := b.store.CreateNewPeopleIntent(b.ctx, person, input)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := b.store.ActivateSocialIntent(b.ctx, person, draft.ID); err != nil {
				t.Fatal(err)
			}
			if person == b.other.ID {
				r.sourceID = draft.ID
			} else {
				r.candidateID = draft.ID
			}
		}
		return r
	}
	if endpoint == "relationship" {
		r.requestID, r.tieID = r.f.acceptedTie(t)
		if _, err := b.store.SetRelationshipConsent(b.ctx, b.other.ID, relationshipcontext.Consent{Enabled: true}); err != nil {
			t.Fatal(err)
		}
		for _, person := range []string{b.person.ID, b.other.ID} {
			if _, err := b.store.SetSocialDisclosure(b.ctx, person, socialcontext.Disclosure{SharedActivities: true}); err != nil {
				t.Fatal(err)
			}
		}
	}
	if endpoint == "activity_chat" || endpoint == "relationship" {
		activity, err := b.store.CreateSocialDraft(b.ctx, b.person.ID, activitypublish.Input{
			CityID: "aberdeen-gb", Title: "合成公开参与事实", Summary: "一次性库并发验证",
			StartsAt: time.Now().Add(time.Hour), EndsAt: time.Now().Add(2 * time.Hour),
			TimeZone: "Europe/London", Visibility: "public", Organizer: activitypublish.Organizer{Type: "PERSON", ID: b.person.ID},
		})
		if err != nil {
			t.Fatal(err)
		}
		r.activityID = activity.ID
		activities = append(activities, activity.ID)
		if _, err := b.store.PublishSocialActivity(b.ctx, b.person.ID, activity.ID); err != nil {
			t.Fatal(err)
		}
		for _, person := range []string{b.person.ID, b.other.ID} {
			if _, _, err := b.store.JoinActivity(b.ctx, person, activity.ID); err != nil {
				t.Fatal(err)
			}
			if endpoint == "activity_chat" {
				if _, err := b.store.JoinActivityChat(b.ctx, person, activity.ID); err != nil {
					t.Fatal(err)
				}
			}
		}
	}
	var clientID string
	if err := b.pool.QueryRow(b.ctx, `SELECT gen_random_uuid()`).Scan(&clientID); err != nil {
		t.Fatal(err)
	}
	if endpoint == "activity_chat" {
		if _, err := b.store.SendActivityChatMessage(b.ctx, b.person.ID, r.activityID, clientID, "合成聊天室正文"); err != nil {
			t.Fatal(err)
		}
	}
	if endpoint == "community_chat" {
		for _, person := range []string{b.person.ID, b.other.ID} {
			if _, err := b.store.JoinCommunityChat(b.ctx, person, r.f.communityID); err != nil {
				t.Fatal(err)
			}
		}
		if _, err := b.store.SendCommunityChatMessage(b.ctx, b.person.ID, r.f.communityID, clientID, "合成社区正文"); err != nil {
			t.Fatal(err)
		}
	}
	return r
}

func (r *legacyRevocationFixture) read(ctx context.Context, store *Store) (any, error) {
	b := r.f.private.base
	switch r.endpoint {
	case "activity_chat":
		return store.ActivityChatMessages(ctx, b.other.ID, r.activityID, "")
	case "community_chat":
		return store.CommunityChatMessages(ctx, b.other.ID, r.f.communityID, "")
	case "relationship":
		return store.OwnRelationshipContext(ctx, b.other.ID)
	default:
		return store.FindNewPeople(ctx, b.other.ID, r.sourceID)
	}
}

func (r *legacyRevocationFixture) mutate(t *testing.T, scenario string) {
	t.Helper()
	b := r.f.private.base
	var err error
	switch scenario {
	case "control":
	case "field_private":
		rules := agentprofile.DefaultFieldRules()
		rules[agentprofile.FieldDisplayName] = agentprofile.FieldRule{Visibility: agentprofile.VisibilityPrivate}
		replaceVisibilityRules(t, r.f, rules)
	case "owner_blocks_viewer":
		err = b.store.BlockAccount(b.ctx, b.person.ID, b.other.ID)
	case "viewer_blocks_owner":
		err = b.store.BlockAccount(b.ctx, b.other.ID, b.person.ID)
	case "viewer_suspended":
		b.exec(`UPDATE accounts SET status='suspended' WHERE id=$1`, b.other.ID)
	case "peer_suspended":
		b.exec(`UPDATE accounts SET status='suspended' WHERE id=$1`, b.person.ID)
	case "room_left":
		if r.endpoint == "activity_chat" {
			err = b.store.LeaveActivityChat(b.ctx, b.other.ID, r.activityID)
		} else {
			err = b.store.LeaveCommunityChat(b.ctx, b.other.ID, r.f.communityID)
		}
	case "rsvp_revoked":
		_, err = b.store.CancelParticipation(b.ctx, b.other.ID, r.activityID)
	case "room_source_hidden":
		if r.endpoint == "activity_chat" {
			b.exec(`UPDATE activities SET publication_status='hidden' WHERE id=$1`, r.activityID)
		} else {
			b.exec(`UPDATE communities SET publication_status='hidden' WHERE id=$1`, r.f.communityID)
		}
	case "community_member_left":
		err = b.store.LeaveSocialCommunity(b.ctx, b.other.ID, r.f.communityID)
	case "relationship_opt_out":
		_, err = b.store.SetRelationshipConsent(b.ctx, b.other.ID, relationshipcontext.Consent{Enabled: false})
	case "own_agent_suspended":
		b.exec(`UPDATE agents SET status='suspended' WHERE id=$1`, b.otherID)
	case "tie_removed":
		err = b.store.RemoveTie(b.ctx, b.other.ID, r.tieID)
	case "accepted_request_withdrawn":
		b.exec(`UPDATE connection_requests SET state='withdrawn' WHERE id=$1`, r.requestID)
	case "accepted_request_wrong_pair":
		b.exec(`UPDATE connection_requests SET recipient_account_id=$2 WHERE id=$1`, r.requestID, r.f.thirdID)
	case "shared_disclosure_revoked":
		_, err = b.store.SetSocialDisclosure(b.ctx, b.person.ID, socialcontext.Disclosure{})
	case "owner_opt_out":
		_, err = b.store.SetNewPeopleConsent(b.ctx, b.other.ID, false)
	case "peer_opt_out":
		_, err = b.store.SetNewPeopleConsent(b.ctx, b.person.ID, false)
	case "source_cancelled":
		_, err = b.store.CancelSocialIntent(b.ctx, b.other.ID, r.sourceID)
	case "peer_cancelled":
		_, err = b.store.CancelSocialIntent(b.ctx, b.person.ID, r.candidateID)
	case "source_constraint_changed":
		b.exec(`UPDATE social_intents SET constraints=jsonb_set(constraints,'{category}','"different-current-category"'::jsonb) WHERE id=$1`, r.sourceID)
	case "source_scope_hidden":
		b.exec(`UPDATE cities SET publication_status='hidden' WHERE id=$1`, r.cityID)
	case "source_profile_private":
		_, err = b.store.UpdateOwnProfile(b.ctx, b.other.ID, identity.ProfileInput{
			DisplayName: "合成当前源资料", Bio: "本人明确更改源权限", Visibility: "private",
		})
	default:
		t.Fatal("unknown owned fixture scenario")
	}
	if err != nil {
		t.Fatalf("actual fixture revocation did not commit: %v", err)
	}
}

func TestAgentProfileVisibilityLegacyStatementRevocationIntegration(t *testing.T) {
	for _, endpoint := range []string{"activity_chat", "community_chat", "relationship", "new_people"} {
		t.Run(endpoint, func(t *testing.T) {
			scenarios := []string{"control", "field_private", "owner_blocks_viewer", "viewer_blocks_owner", "viewer_suspended", "peer_suspended"}
			switch endpoint {
			case "activity_chat":
				scenarios = append(scenarios, "room_left", "rsvp_revoked", "room_source_hidden")
			case "community_chat":
				scenarios = append(scenarios, "room_left", "community_member_left", "room_source_hidden")
			case "relationship":
				scenarios = append(scenarios, "relationship_opt_out", "own_agent_suspended", "tie_removed", "accepted_request_withdrawn", "accepted_request_wrong_pair", "shared_disclosure_revoked")
			case "new_people":
				scenarios = append(scenarios, "owner_opt_out", "peer_opt_out", "source_cancelled", "peer_cancelled", "source_constraint_changed", "source_scope_hidden", "source_profile_private")
			}
			for _, scenario := range scenarios {
				t.Run(scenario, func(t *testing.T) {
					r := newLegacyRevocationFixture(t, endpoint, scenario == "source_scope_hidden")
					b := r.f.private.base
					initial, err := r.read(b.ctx, b.store)
					initialJSON, encodeErr := json.Marshal(initial)
					if err != nil || encodeErr != nil || !strings.Contains(string(initialJSON), r.name) {
						t.Fatalf("initial authorized synthetic payload missing: %v", err)
					}
					match := map[string]string{
						"activity_chat":  "FROM activities a LEFT JOIN activity_conversations c",
						"community_chat": "FROM communities c LEFT JOIN community_conversations conversation",
						"relationship":   "SELECT coalesce(c.enabled,false),EXISTS(SELECT 1 FROM agents ag",
						"new_people":     "WHERE i.id=$2 AND i.creator_account_id=$1 AND",
					}[endpoint]
					gate := &legacyProjectionGate{match: match, entered: make(chan struct{}), release: make(chan struct{})}
					config, err := pgxpool.ParseConfig(os.Getenv("BIRDTIE_DATABASE_URL"))
					if err != nil {
						t.Fatal("cannot configure owned traced database pool")
					}
					config.ConnConfig.Tracer = gate
					pool, err := pgxpool.NewWithConfig(b.ctx, config)
					if err != nil {
						t.Fatal("cannot create owned traced database pool")
					}
					t.Cleanup(pool.Close)
					t.Cleanup(gate.resume)
					type outcome struct {
						value any
						err   error
					}
					completed := make(chan outcome, 1)
					go func() {
						value, err := r.read(b.ctx, New(pool, false))
						completed <- outcome{value, err}
					}()
					select {
					case <-gate.entered:
					case early := <-completed:
						t.Fatalf("read did not enter its preliminary SQL barrier: %v", early.err)
					case <-time.After(3 * time.Second):
						t.Fatal("read did not reach its deterministic query barrier")
					}
					r.mutate(t, scenario)
					gate.resume()
					var result outcome
					select {
					case result = <-completed:
					case <-time.After(3 * time.Second):
						t.Fatal("read did not finish after committed revocation")
					}
					denied := scenario != "control" && scenario != "field_private" && scenario != "shared_disclosure_revoked"
					if result.err != nil {
						if !denied || !(errors.Is(result.err, activitychat.ErrNotFound) || errors.Is(result.err, communitychat.ErrNotFound) || errors.Is(result.err, relationshipcontext.ErrNotFound) || errors.Is(result.err, relationshipcontext.ErrAgentUnavailable) || errors.Is(result.err, newpeople.ErrNotFound)) {
							t.Fatalf("revocation produced an unexpected Store error: %v", result.err)
						}
						return
					}
					body, err := json.Marshal(result.value)
					if err != nil {
						t.Fatal("cannot inspect bounded synthetic result")
					}
					nameAllowed := scenario == "control" || scenario == "shared_disclosure_revoked"
					if strings.Contains(string(body), r.name) != nameAllowed {
						t.Fatal("payload used a field rule from before the committed revocation")
					}
					var count int
					switch value := result.value.(type) {
					case activitychat.Page:
						count = len(value.Messages)
						if scenario == "field_private" && count == 1 && value.Messages[0].SenderName != "Birdtie 成员" {
							t.Fatal("denied chat name did not use its fixed generic label")
						}
					case newpeople.Response:
						count = len(value.Candidates)
						if scenario == "field_private" && count == 1 && value.Candidates[0].DisplayName != "Birdtie 成员" {
							t.Fatal("denied candidate name did not use its fixed generic label")
						}
					case relationshipcontext.Context:
						count = len(value.Peers)
						if scenario == "field_private" && count == 1 && value.Peers[0].DisplayName != "好友" {
							t.Fatal("denied relationship name did not use its fixed generic label")
						}
						if scenario == "relationship_opt_out" && value.Enabled {
							t.Fatal("relationship result retained stale enabled consent")
						}
						if scenario == "shared_disclosure_revoked" && (count != 1 || len(value.Peers[0].SharedActivities) != 0) {
							t.Fatal("shared activity disclosure used an earlier source query")
						}
					default:
						t.Fatal("unexpected owned Store response type")
					}
					if denied && count != 0 || !denied && count != 1 {
						t.Fatalf("original source/room/consent gate was not enforced at payload statement; count=%d denied=%t", count, denied)
					}
				})
			}
		})
	}
}
