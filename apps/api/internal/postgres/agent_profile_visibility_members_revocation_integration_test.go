package postgres

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/agentprofile"
	"github.com/birdtie/birdtie/apps/api/internal/community"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestAgentProfileVisibilityCommunityRosterStatementRevocationIntegration(t *testing.T) {
	for _, scenario := range []string{
		"members_control", "field_private", "viewer_left", "viewer_suspended", "group_archived",
		"requests_control", "requests_role_dropped", "requests_viewer_left", "requests_group_archived",
	} {
		t.Run(scenario, func(t *testing.T) {
			f := agentVisibilityTestFixture(t)
			b := f.private.base
			requests := strings.HasPrefix(scenario, "requests_")
			name := "synthetic-roster-visibility-" + b.person.ID
			b.exec(`UPDATE user_profiles SET display_name=$2 WHERE account_id=$1`, b.person.ID, name)
			b.exec(`UPDATE accounts SET handle=$2 WHERE id=$1`, b.person.ID, name)
			if requests {
				b.exec(`UPDATE community_memberships SET role='admin' WHERE community_id=$1 AND user_account_id=$2`, f.communityID, b.other.ID)
				b.exec(`UPDATE community_memberships SET status='pending' WHERE community_id=$1 AND user_account_id=$2`, f.communityID, b.person.ID)
			}
			before, err := b.store.ListSocialMembers(b.ctx, b.other.ID, f.communityID, requests)
			body, encodeErr := json.Marshal(before)
			if err != nil || encodeErr != nil || !strings.Contains(string(body), name) {
				t.Fatalf("initial authorized roster missing synthetic name: %v", err)
			}
			gate := &legacyProjectionGate{
				match:   "SELECT role,status FROM community_memberships WHERE community_id=$1 AND user_account_id=$2",
				entered: make(chan struct{}), release: make(chan struct{}),
			}
			config, err := pgxpool.ParseConfig(os.Getenv("BIRDTIE_DATABASE_URL"))
			if err != nil {
				t.Fatal("cannot configure owned roster pool")
			}
			config.ConnConfig.Tracer = gate
			pool, err := pgxpool.NewWithConfig(b.ctx, config)
			if err != nil {
				t.Fatal("cannot create owned traced roster pool")
			}
			t.Cleanup(pool.Close)
			t.Cleanup(gate.resume)
			type result struct {
				values []community.Membership
				err    error
			}
			completed := make(chan result, 1)
			go func() {
				values, err := New(pool, false).ListSocialMembers(b.ctx, b.other.ID, f.communityID, requests)
				completed <- result{values, err}
			}()
			select {
			case <-gate.entered:
			case early := <-completed:
				t.Fatalf("roster bypassed its deterministic role-query barrier: %v", early.err)
			case <-time.After(3 * time.Second):
				t.Fatal("roster role query did not reach owned barrier")
			}
			switch scenario {
			case "field_private":
				rules := agentprofile.DefaultFieldRules()
				rules[agentprofile.FieldDisplayName] = agentprofile.FieldRule{Visibility: agentprofile.VisibilityPrivate}
				replaceVisibilityRules(t, f, rules)
			case "viewer_left", "requests_viewer_left":
				if err := b.store.LeaveSocialCommunity(b.ctx, b.other.ID, f.communityID); err != nil {
					t.Fatal(err)
				}
			case "viewer_suspended":
				b.exec(`UPDATE accounts SET status='suspended' WHERE id=$1`, b.other.ID)
			case "group_archived", "requests_group_archived":
				b.exec(`UPDATE communities SET lifecycle_status='archived' WHERE id=$1`, f.communityID)
			case "requests_role_dropped":
				b.exec(`UPDATE community_memberships SET role='member' WHERE community_id=$1 AND user_account_id=$2`, f.communityID, b.other.ID)
			}
			gate.resume()
			var after result
			select {
			case after = <-completed:
			case <-time.After(3 * time.Second):
				t.Fatal("roster did not finish after actual committed revocation")
			}
			if after.err != nil {
				t.Fatalf("unexpected final roster query failure: %v", after.err)
			}
			control := strings.HasSuffix(scenario, "_control")
			if control || scenario == "field_private" {
				if len(after.values) != len(before) {
					t.Fatal("authorized source roster changed unexpectedly")
				}
			} else if len(after.values) != 0 {
				t.Fatal("roster payload retained revoked membership/admin/source authority")
			}
			body, err = json.Marshal(after.values)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(body), name) != control {
				t.Fatal("roster returned a name after committed field/resource revocation")
			}
			if scenario == "field_private" {
				for _, member := range after.values {
					if member.UserAccountID == b.person.ID && member.DisplayName != "Birdtie 成员" {
						t.Fatal("denied roster name did not use its fixed generic label")
					}
				}
			}
		})
	}
}
