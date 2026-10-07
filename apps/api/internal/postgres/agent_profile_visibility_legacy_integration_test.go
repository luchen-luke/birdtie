package postgres

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/activitypublish"
	"github.com/birdtie/birdtie/apps/api/internal/agentprofile"
	"github.com/birdtie/birdtie/apps/api/internal/follow"
	"github.com/birdtie/birdtie/apps/api/internal/intent"
	"github.com/birdtie/birdtie/apps/api/internal/newpeople"
	"github.com/birdtie/birdtie/apps/api/internal/relationshipcontext"
)

// These are synthetic inputs in an explicitly disposable DB, not user data,
// verified identity or deployment evidence. All resources belong to this test.
func TestAgentProfileVisibilityLegacyProjectionsIntegration(t *testing.T) {
	f := agentVisibilityTestFixture(t)
	b := f.private.base
	owner, viewer := b.person.ID, b.other.ID
	name := "AGE003-hidden-name-" + owner
	bio := "AGE003-hidden-bio-" + owner
	topic := "AGE003-explicit-public-intent-" + owner
	b.exec(`UPDATE user_profiles SET display_name=$2,bio=$3 WHERE account_id=$1`, owner, name, bio)
	// A handle is not separately authorized for these name fields. Use the
	// same private canary so a name->handle fallback cannot make this test pass.
	b.exec(`UPDATE accounts SET handle=$2 WHERE id=$1`, owner, name)
	b.exec(`INSERT INTO user_profiles(account_id,display_name,visibility) VALUES($1,'合成第三人','public')`, f.thirdID)
	activities := []string{}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		for _, statement := range []string{
			`DELETE FROM inbox_items WHERE recipient_account_id=ANY($1::uuid[])`,
			`DELETE FROM conversation_messages WHERE sender_account_id=ANY($1::uuid[])`,
			`DELETE FROM conversations WHERE member_a_account_id=ANY($1::uuid[]) OR member_b_account_id=ANY($1::uuid[])`,
			`DELETE FROM follows WHERE follower_account_id=ANY($1::uuid[]) OR person_account_id=ANY($1::uuid[])`,
			`DELETE FROM intents WHERE owner_account_id=ANY($1::uuid[])`,
			`DELETE FROM social_intents WHERE creator_account_id=ANY($1::uuid[])`,
			`DELETE FROM person_new_people_consent WHERE account_id=ANY($1::uuid[])`,
			`DELETE FROM person_agent_relationship_consent WHERE account_id=ANY($1::uuid[])`,
			`DELETE FROM admin_audit_events WHERE actor_account_id=ANY($1::uuid[])`,
		} {
			if _, err := b.pool.Exec(ctx, statement, b.accounts); err != nil {
				t.Errorf("owned legacy projection cleanup failed: %v", err)
			}
		}
		if _, err := b.pool.Exec(ctx, `DELETE FROM activities WHERE id=ANY($1::uuid[])`, activities); err != nil {
			t.Errorf("owned legacy activity cleanup failed: %v", err)
		}
	})
	if _, err := b.store.SubmitIntent(b.ctx, owner, "aberdeen-gb", intent.Input{
		Confirmed: true, Topic: topic, Details: "独立公开意图，不含私密姓名", TimeZone: "Europe/London", CoarseAreaLabel: "合成明确公开区域",
		AvailableFrom: time.Now().Add(-time.Minute), AvailableUntil: time.Now().Add(time.Hour),
		ExpiresAt: time.Now().Add(2 * time.Hour),
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := b.store.Follow(b.ctx, viewer, follow.Target{Type: follow.Person, ID: owner}); err != nil {
		t.Fatal(err)
	}
	setAudience := func(audience agentprofile.FieldVisibility) {
		t.Helper()
		rules := agentprofile.DefaultFieldRules()
		ids := []string{}
		if audience == agentprofile.VisibilityCommunity {
			ids = append(ids, f.communityID)
		}
		rules[agentprofile.FieldDisplayName] = agentprofile.FieldRule{Visibility: audience, CommunityIDs: ids}
		rules[agentprofile.FieldBio] = agentprofile.FieldRule{Visibility: audience, CommunityIDs: ids}
		replaceVisibilityRules(t, f, rules)
	}
	assertName := func(t *testing.T, value any, allowed bool) {
		t.Helper()
		encoded, err := json.Marshal(value)
		if err != nil {
			t.Fatal("cannot inspect synthetic projection")
		}
		if strings.Contains(string(encoded), name) != allowed {
			t.Fatalf("legacy field projection did not enforce current display-name rule; allowed=%t", allowed)
		}
		if !allowed && strings.Contains(string(encoded), bio) {
			t.Fatal("legacy projection leaked hidden bio")
		}
	}
	assertDeniedLabel := func(t *testing.T, label, generic string, allowed bool) {
		t.Helper()
		if !allowed && label != generic {
			t.Fatal("denied name field did not retain its fixed generic label")
		}
	}

	t.Run("follow_does_not_authorize_connections", func(t *testing.T) {
		setAudience(agentprofile.VisibilityConnections)
		p, err := b.store.ReadProfile(b.ctx, viewer, owner)
		if err != nil || p.DisplayName != "" || p.Bio != "" {
			t.Fatal("a Follow was mistaken for a currently accepted FriendTie")
		}
		list, err := b.store.ListOwnFollows(b.ctx, viewer)
		if err != nil || len(list) != 1 {
			t.Fatal("the independent Follow resource stopped working")
		}
		assertName(t, list, false)
		assertDeniedLabel(t, list[0].Label, "Birdtie 成员", false)
	})
	requestID, tieID := f.acceptedTie(t)
	conversation, err := b.store.StartFriendConversation(b.ctx, viewer, tieID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := b.store.SetRelationshipConsent(b.ctx, viewer, relationshipcontext.Consent{Enabled: true}); err != nil {
		t.Fatal(err)
	}
	for _, person := range []string{owner, viewer} {
		if _, err := b.store.JoinCommunityChat(b.ctx, person, f.communityID); err != nil {
			t.Fatal(err)
		}
	}
	setAudience(agentprofile.VisibilityPrivate)
	activity, err := b.store.CreateSocialDraft(b.ctx, owner, activitypublish.Input{
		CityID: "aberdeen-gb", Title: "合成字段权限活动", Summary: "仅一次性验证库",
		StartsAt: time.Now().Add(time.Hour), EndsAt: time.Now().Add(2 * time.Hour),
		TimeZone: "Europe/London", Visibility: "public", Organizer: activitypublish.Organizer{Type: "PERSON", ID: owner},
	})
	if err != nil {
		t.Fatal(err)
	}
	activities = append(activities, activity.ID)
	var hostLabel string
	if err := b.pool.QueryRow(b.ctx, `SELECT host_label FROM activities WHERE id=$1`, activity.ID).Scan(&hostLabel); err != nil {
		t.Fatal(err)
	}
	t.Run("person_auto_host_label_uses_public_scope", func(t *testing.T) { assertName(t, hostLabel, false) })
	if _, err := b.store.PublishSocialActivity(b.ctx, owner, activity.ID); err != nil {
		t.Fatal(err)
	}
	for _, person := range []string{owner, viewer} {
		if _, _, err := b.store.JoinActivity(b.ctx, person, activity.ID); err != nil {
			t.Fatal(err)
		}
		if _, err := b.store.JoinActivityChat(b.ctx, person, activity.ID); err != nil {
			t.Fatal(err)
		}
	}
	var activityClient, communityClient string
	if err := b.pool.QueryRow(b.ctx, `SELECT gen_random_uuid(),gen_random_uuid()`).Scan(&activityClient, &communityClient); err != nil {
		t.Fatal(err)
	}
	if _, err := b.store.SendActivityChatMessage(b.ctx, owner, activity.ID, activityClient, "合成房间消息"); err != nil {
		t.Fatal(err)
	}
	if _, err := b.store.SendCommunityChatMessage(b.ctx, owner, f.communityID, communityClient, "合成社区消息"); err != nil {
		t.Fatal(err)
	}
	b.exec(`INSERT INTO organization_memberships(organization_id,user_account_id,role,status)
		VALUES($1,$2,'owner','active')`, b.orgID, viewer)
	invited, err := b.store.InviteMember(b.ctx, viewer, b.orgID, owner, "member")
	if err != nil {
		t.Fatal(err)
	}
	t.Run("invitation_manager_does_not_inherit_private_name", func(t *testing.T) {
		assertName(t, invited, false)
		assertDeniedLabel(t, invited.DisplayName, "Birdtie 用户", false)
	})
	t.Run("invitation_owner_can_review_own_private_name", func(t *testing.T) {
		list, err := b.store.ListInvitations(b.ctx, owner)
		if err != nil || len(list) != 1 {
			t.Fatal("self invitation read failed")
		}
		assertName(t, list, true)
	})
	accepted, err := b.store.AcceptInvitation(b.ctx, owner, invited.ID)
	if err != nil {
		t.Fatal(err)
	}
	t.Run("invitation_accept_uses_actual_self_viewer", func(t *testing.T) { assertName(t, accepted, true) })
	changed, err := b.store.ChangeMemberRole(b.ctx, viewer, b.orgID, invited.ID, "moderator")
	if err != nil {
		t.Fatal(err)
	}
	t.Run("role_change_manager_does_not_inherit_private_name", func(t *testing.T) {
		assertName(t, changed, false)
		assertDeniedLabel(t, changed.DisplayName, "Birdtie 用户", false)
	})
	var newPeopleSourceID string
	for _, person := range []string{owner, f.thirdID} {
		if _, err := b.store.SetNewPeopleConsent(b.ctx, person, true); err != nil {
			t.Fatal(err)
		}
		draft, err := b.store.CreateNewPeopleIntent(b.ctx, person, newpeople.DraftInput{
			Title: "合成线上匹配", Category: "synthetic-field-visibility", Modality: "ONLINE", ExpiresAt: time.Now().Add(time.Hour),
		})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := b.store.ActivateSocialIntent(b.ctx, person, draft.ID); err != nil {
			t.Fatal(err)
		}
		if person == f.thirdID {
			newPeopleSourceID = draft.ID
		}
	}

	for _, audience := range []agentprofile.FieldVisibility{
		agentprofile.VisibilityPublic, agentprofile.VisibilityPrivate, agentprofile.VisibilityConnections,
		agentprofile.VisibilityCommunity, agentprofile.VisibilityAgentOnly,
	} {
		t.Run(string(audience), func(t *testing.T) {
			setAudience(audience)
			allowed := audience == agentprofile.VisibilityPublic || audience == agentprofile.VisibilityConnections || audience == agentprofile.VisibilityCommunity
			t.Run("legacy_profile", func(t *testing.T) {
				p, err := b.store.ReadProfile(b.ctx, viewer, owner)
				if err != nil || (p.DisplayName == name) != allowed || (p.Bio == bio) != allowed {
					t.Fatal("original four-field Profile projection ignored field rule")
				}
				assertName(t, p, allowed)
			})
			t.Run("anonymous_profile", func(t *testing.T) {
				p, err := b.store.ReadProfile(b.ctx, "", owner)
				if err != nil {
					t.Fatal(err)
				}
				assertName(t, p, audience == agentprofile.VisibilityPublic)
			})
			t.Run("owner_review", func(t *testing.T) {
				p, err := b.store.ReadProfile(b.ctx, owner, owner)
				if err != nil {
					t.Fatal(err)
				}
				assertName(t, p, true)
			})
			t.Run("friend_requests", func(t *testing.T) {
				list, err := b.store.ListRequests(b.ctx, viewer)
				if err != nil || len(list) != 1 {
					t.Fatal("accepted request projection failed")
				}
				assertName(t, list, allowed)
				assertDeniedLabel(t, list[0].OtherName, "Birdtie 成员", allowed)
			})
			t.Run("friend_ties", func(t *testing.T) {
				list, err := b.store.ListTies(b.ctx, viewer)
				if err != nil || len(list) != 1 {
					t.Fatal("Tie projection failed")
				}
				assertName(t, list, allowed)
				assertDeniedLabel(t, list[0].OtherName, "Birdtie 成员", allowed)
			})
			t.Run("start_friend_conversation", func(t *testing.T) {
				chat, err := b.store.StartFriendConversation(b.ctx, viewer, tieID)
				if err != nil || chat.ID != conversation.ID {
					t.Fatal("friend conversation idempotent read failed")
				}
				assertName(t, chat, allowed)
				assertDeniedLabel(t, chat.OtherName, "Birdtie 成员", allowed)
			})
			t.Run("conversation_list", func(t *testing.T) {
				list, err := b.store.ListConversations(b.ctx, viewer)
				if err != nil || len(list) != 1 {
					t.Fatal("conversation list failed")
				}
				assertName(t, list, allowed)
				assertDeniedLabel(t, list[0].OtherName, "Birdtie 成员", allowed)
			})
			t.Run("entity_send_response_uses_sender_viewer", func(t *testing.T) {
				message, err := b.store.SendEntityMessage(b.ctx, viewer, conversation.ID, "合成分享公开资料链接", "person", owner)
				if err != nil || message.Entity == nil {
					t.Fatal("explicit human entity message failed")
				}
				assertName(t, message, allowed)
				assertDeniedLabel(t, message.Entity.Title, "Birdtie 成员", allowed)
			})
			t.Run("entity_message_current_read", func(t *testing.T) {
				list, err := b.store.ListMessages(b.ctx, viewer, conversation.ID)
				if err != nil || len(list) == 0 {
					t.Fatal("entity message current projection failed")
				}
				assertName(t, list, allowed)
				if list[0].Entity == nil {
					t.Fatal("explicit resource card lost its independent reference")
				}
				assertDeniedLabel(t, list[0].Entity.Title, "Birdtie 成员", allowed)
			})
			t.Run("follow_list", func(t *testing.T) {
				list, err := b.store.ListOwnFollows(b.ctx, viewer)
				if err != nil || len(list) != 1 {
					t.Fatal("Follow current projection failed")
				}
				assertName(t, list, allowed)
				assertDeniedLabel(t, list[0].Label, "Birdtie 成员", allowed)
			})
			t.Run("community_members", func(t *testing.T) {
				list, err := b.store.ListSocialMembers(b.ctx, viewer, f.communityID, false)
				if err != nil || len(list) != 3 {
					t.Fatal("Community members current projection failed")
				}
				assertName(t, list, allowed)
				for _, member := range list {
					if member.UserAccountID == owner {
						assertDeniedLabel(t, member.DisplayName, "Birdtie 成员", allowed)
					}
				}
			})
			t.Run("organization_members", func(t *testing.T) {
				list, err := b.store.ListMembers(b.ctx, viewer, b.orgID)
				if err != nil || len(list) != 2 {
					t.Fatal("Organization members current projection failed")
				}
				assertName(t, list, allowed)
				for _, member := range list {
					if member.UserAccountID == owner {
						assertDeniedLabel(t, member.DisplayName, "Birdtie 用户", allowed)
					}
				}
			})
			t.Run("activity_chat_reader", func(t *testing.T) {
				page, err := b.store.ActivityChatMessages(b.ctx, viewer, activity.ID, "")
				if err != nil || len(page.Messages) != 1 {
					t.Fatal("Activity room messages current projection failed")
				}
				assertName(t, page, allowed)
				assertDeniedLabel(t, page.Messages[0].SenderName, "Birdtie 成员", allowed)
			})
			t.Run("community_chat_reader", func(t *testing.T) {
				page, err := b.store.CommunityChatMessages(b.ctx, viewer, f.communityID, "")
				if err != nil || len(page.Messages) != 1 {
					t.Fatal("Community room messages current projection failed")
				}
				assertName(t, page, allowed)
				assertDeniedLabel(t, page.Messages[0].SenderName, "Birdtie 成员", allowed)
			})
			t.Run("chat_insert_replay_is_own_viewer", func(t *testing.T) {
				message, err := b.store.SendActivityChatMessage(b.ctx, owner, activity.ID, activityClient, "合成房间消息")
				if err != nil {
					t.Fatal(err)
				}
				assertName(t, message, true)
				communityMessage, err := b.store.SendCommunityChatMessage(b.ctx, owner, f.communityID, communityClient, "合成社区消息")
				if err != nil {
					t.Fatal(err)
				}
				assertName(t, communityMessage, true)
			})
			t.Run("agent_public_topic_search", func(t *testing.T) {
				result, err := b.store.Search(b.ctx, "aberdeen-gb", viewer, []string{strings.ToLower(topic)})
				if err != nil || len(result.People) != 1 {
					t.Fatal("explicit public intent discovery was removed")
				}
				assertName(t, result.People, allowed)
			})
			t.Run("agent_name_search_side_channel", func(t *testing.T) {
				result, err := b.store.Search(b.ctx, "aberdeen-gb", viewer, []string{strings.ToLower(name)})
				if err != nil || (len(result.People) == 1) != allowed {
					t.Fatal("name-only search inferred a hidden display name")
				}
				assertName(t, result.People, allowed)
			})
			t.Run("relationship_context_name", func(t *testing.T) {
				result, err := b.store.OwnRelationshipContext(b.ctx, viewer)
				if err != nil || !result.Enabled || len(result.Peers) != 1 {
					t.Fatal("explicit relationship metadata projection failed")
				}
				assertName(t, result, allowed)
				assertDeniedLabel(t, result.Peers[0].DisplayName, "好友", allowed)
			})
			t.Run("new_people_candidate_current_viewer", func(t *testing.T) {
				result, err := b.store.FindNewPeople(b.ctx, f.thirdID, newPeopleSourceID)
				if err != nil || len(result.Candidates) != 1 {
					t.Fatal("new people synthetic declaration match failed")
				}
				assertName(t, result, audience == agentprofile.VisibilityPublic || audience == agentprofile.VisibilityCommunity)
				assertDeniedLabel(t, result.Candidates[0].DisplayName, "Birdtie 成员", audience == agentprofile.VisibilityPublic || audience == agentprofile.VisibilityCommunity)
			})
		})
	}
	setAudience(agentprofile.VisibilityConnections)
	t.Run("removed_tie_does_not_preserve_name_permission", func(t *testing.T) {
		b.exec(`UPDATE person_ties SET status='removed' WHERE id=$1`, tieID)
		p, err := b.store.ReadProfile(b.ctx, viewer, owner)
		if err != nil {
			t.Fatal(err)
		}
		assertName(t, p, false)
		b.exec(`UPDATE person_ties SET status='active' WHERE id=$1`, tieID)
	})
	t.Run("pending_request_is_not_accepted_tie_permission", func(t *testing.T) {
		b.exec(`UPDATE connection_requests SET state='pending' WHERE id=$1`, requestID)
		p, err := b.store.ReadProfile(b.ctx, viewer, owner)
		if err != nil {
			t.Fatal(err)
		}
		assertName(t, p, false)
		b.exec(`UPDATE connection_requests SET state='accepted' WHERE id=$1`, requestID)
	})
	setAudience(agentprofile.VisibilityCommunity)
	t.Run("left_community_does_not_preserve_name_permission", func(t *testing.T) {
		b.exec(`UPDATE community_memberships SET status='left' WHERE community_id=$1 AND user_account_id=$2`, f.communityID, viewer)
		p, err := b.store.ReadProfile(b.ctx, viewer, owner)
		if err != nil {
			t.Fatal(err)
		}
		assertName(t, p, false)
		b.exec(`UPDATE community_memberships SET status='active' WHERE community_id=$1 AND user_account_id=$2`, f.communityID, viewer)
	})
	setAudience(agentprofile.VisibilityPrivate)
	t.Run("organization_revoke_preserves_field_restriction", func(t *testing.T) {
		if err := b.store.RevokeMember(b.ctx, viewer, b.orgID, invited.ID); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("deleting_agent_does_not_resurrect_public_defaults", func(t *testing.T) {
		b.exec(`DELETE FROM agents WHERE id=$1`, b.personID)
		p, err := b.store.ReadProfile(b.ctx, "", owner)
		if err != nil {
			t.Fatal(err)
		}
		assertName(t, p, false)
	})
}
