package postgres

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/activitypublish"
	arp "github.com/birdtie/birdtie/apps/api/internal/agentresultprojection"
	"github.com/birdtie/birdtie/apps/api/internal/agentworkspace"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
)

// Original SQL/native ledger tests with FAKE WSA/model transport only. These
// are compiled by the worker, but SQL/provider/device execution is NOT_RUN.
func liveReplyHistoryNativeFixture(t *testing.T) (*liveNativeFixture, string, []string) {
	t.Helper()
	f, city := resolvedLiveNativeFixture(t)
	b := f.configuration.native.private.base
	ids := []string{}
	for i, name := range []string{"FirstOnly", "SecondOnly"} {
		var id string
		if e := b.pool.QueryRow(b.ctx, `INSERT INTO places(id,city_id,name,category_code,publication_status,source_label,source_ref,maintainer_label,maintainer_account_id,coordinate_system,location_precision,latitude,longitude) VALUES(gen_random_uuid(),$1,$2,'culture','published','LOCAL_SYNTHETIC_FIXTURE','https://example.com/unit-native-history','合成',$3,'wgs84','point',$4,-2.1) RETURNING id`, city, name, b.other.ID, 57.15+float64(i)*0.01).Scan(&id); e != nil {
			t.Fatal("owned synthetic public native place", e)
		}
		ids = append(ids, id)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if _, e := b.pool.Exec(ctx, `DELETE FROM places WHERE city_id=$1 AND maintainer_account_id=$2`, city, b.other.ID); e != nil {
			t.Error("owned stage2 place cleanup", e)
		}
	})
	task := f.configuration.native.task
	// Explicitly start this test's ordinary city-wide Place turn via the native
	// Task writer. Original egress canaries stay in history/private filters.
	for _, key := range []string{"mapWest", "mapEast", "mapSouth", "mapNorth"} {
		delete(task.Filters, key)
	}
	task.Intent = agentworkspace.FindPlace
	task.Filters["targetIntent"], task.Filters["category"], task.Filters["timePreference"] = agentworkspace.FindPlace, "", "anytime"
	task.Filters["currentQuery"], task.Filters["searchTerm"] = "找地点 FirstOnly", "FirstOnly"
	task.Conversation[len(task.Conversation)-1].Text = task.Filters["currentQuery"]
	var e error
	f.configuration.native.task, e = b.store.UpdateTask(b.ctx, task)
	if e != nil {
		t.Fatal("stage2 exact ACTIVE native Task", e)
	}
	return f, city, ids
}

func liveReplyHistoryNativeAccess(t *testing.T, f *liveNativeFixture, task agentworkspace.Task) arp.Access {
	t.Helper()
	raw, e := json.Marshal(agentworkspace.SanitizeTaskForResponse(task))
	if e != nil {
		t.Fatal(e)
	}
	return arp.Access{Actor: identity.Actor{ID: f.configuration.native.private.base.person.ID, AccountType: "person"}, SessionDigest: f.configuration.native.access.SessionDigest, TaskID: task.ID, ExpectedTask: raw}
}

func liveReplyHistoryNativeComplete(t *testing.T, f *liveNativeFixture) (agentworkspace.Task, *nativeLiveSourceAnswerRun, []byte) {
	t.Helper()
	b := f.configuration.native.private.base
	h := resolvedLiveNativeRun(t, f)
	_, preview, calls := liveSourceReplyCompleteFixture(t, f, h)
	started := time.Now()
	reply, e := h.FinalizeSourceReply(b.ctx)
	if e != nil || *calls != 1 {
		t.Fatal("stage2 native model finalization", e, *calls)
	}
	task := reply.Task()
	last := len(task.Conversation) - 1
	if agentworkspace.ValidateReplyMembership(task, last) != nil || task.Conversation[last].Text != liveSourceReplyFixtureText || task.Conversation[last].SourceRunID != h.id || len(task.Conversation[last].Sources) == 0 {
		t.Fatal("one model/source/native membership message was not persisted")
	}
	t.Logf("LOCAL_SYNTHETIC stage2 finalization elapsed_ms=%d", time.Since(started).Milliseconds())
	f.configuration.native.task = task
	return task, h, preview.Prepared().ModelWire().ExactWire()
}

func TestModelLiveReplyHistoryTwoTurnsRestartNoLatestJoinIntegration(t *testing.T) {
	f, city, places := liveReplyHistoryNativeFixture(t)
	b := f.configuration.native.private.base
	first, _, _ := liveReplyHistoryNativeComplete(t, f)
	firstIndex := len(first.Conversation) - 1
	if !reflect.DeepEqual(first.Conversation[firstIndex].ResultMembership.Refs, []arp.Ref{{Type: "place", ID: places[0]}}) {
		t.Fatal("first native membership differs")
	}
	secondStart := first
	secondStart.Status = agentworkspace.TaskActive
	secondStart.Filters["currentQuery"], secondStart.Filters["searchTerm"] = "找地点 SecondOnly", "SecondOnly"
	secondStart.Conversation = append(secondStart.Conversation, agentworkspace.Message{Role: "user", Text: secondStart.Filters["currentQuery"]})
	var e error
	f.configuration.native.task, e = b.store.UpdateTask(b.ctx, secondStart)
	if e != nil {
		t.Fatal("same Task second turn", e)
	}
	second, lastRun, modelWire := liveReplyHistoryNativeComplete(t, f)
	secondIndex := len(second.Conversation) - 1
	if !reflect.DeepEqual(first.Conversation, second.Conversation[:len(first.Conversation)]) || !reflect.DeepEqual(second.Conversation[secondIndex].ResultMembership.Refs, []arp.Ref{{Type: "place", ID: places[1]}}) {
		t.Fatal("old model round was replaced by latest entity")
	}
	b.exec(`INSERT INTO places(id,city_id,name,category_code,publication_status,source_label,source_ref,maintainer_label,maintainer_account_id) VALUES(gen_random_uuid(),$1,'SecondOnly newly published','culture','published','LOCAL_SYNTHETIC_FIXTURE','https://example.com/unit-third','合成',$2)`, city, b.other.ID)
	restarted := New(b.pool, false)
	restored, e := restarted.GetTask(b.ctx, b.person.ID, second.ID)
	if e != nil {
		t.Fatal(e)
	}
	started := time.Now()
	read, e := restarted.ReadOwnMessageResults(b.ctx, liveReplyHistoryNativeAccess(t, f, restored))
	if e != nil {
		t.Fatal("fresh Store historical model membership", e)
	}
	entries := read.Results()
	if len(entries) != 2 {
		t.Fatal("two model memberships not recovered", len(entries))
	}
	for i, entry := range entries {
		index := []int{firstIndex, secondIndex}[i]
		member := restored.Conversation[index].ResultMembership
		if entry.MessageIndex != index || entry.ResultSet.ID != member.ResultSetID || len(entry.ResultSet.Items) != 1 || entry.ResultSet.Items[0].Entity.ID != places[i] || !reflect.DeepEqual(entry.ResultSet.Entities, member.Refs) || !reflect.DeepEqual(entry.MapEffects.PinEntityIDs, []string{"place:" + places[i]}) || len(entry.ResultSet.Items[0].Actions) == 0 || !entry.ValidUntil.After(entry.ResultSet.GeneratedAt) || len(entry.ResultSet.Sources) != 0 || entry.ResultSet.AnswerBinding != nil {
			t.Fatal("fresh native model card/map/history contract differs", i)
		}
		if restored.Conversation[index].SourceRunID == "" || len(restored.Conversation[index].Sources) == 0 {
			t.Fatal("citation ownership lost")
		}
	}
	if e = read.Revalidate(b.ctx); e != nil {
		t.Fatal("native final source recheck", e)
	}
	t.Logf("LOCAL_SYNTHETIC stage2 two-turn read+tail elapsed_ms=%d", time.Since(started).Milliseconds())
	before := restored
	if _, e = lastRun.FinalizeSourceReply(b.ctx); e == nil {
		t.Fatal("duplicate model reply appended")
	}
	liveSourceReplyRequireNoTaskWrite(t, f, lastRun, before)
	views, e := b.store.ReadOwnModelBudget(b.ctx, f.configuration.native.access, f.root, second.ID)
	if e != nil {
		t.Fatal(e)
	}
	for _, view := range views {
		want := int64(279680)
		if view.Scope == "TENANT_PERSON" || view.Scope == "SUBJECT_PERSON" {
			want *= 2
		}
		if view.Allocated.CostMicros != want {
			t.Fatal("history reset old UNKNOWN holds", view.Scope)
		}
	}
	for _, canary := range []string{"PRIVATE_HISTORY_CANARY", "PRIVATE_ASSISTANT_CANARY", "PRIVATE_PROFILE_CANARY", "PRIVATE_RESULT_CANARY", places[0], places[1]} {
		if strings.Contains(string(modelWire), canary) {
			t.Fatal("human membership became model egress", canary)
		}
	}
}

func TestModelLiveReplyHistoryFreshACLAndEpochIntegration(t *testing.T) {
	for _, change := range []string{"sourceHidden", "sourceABA", "ownerInactive", "sessionRevoked", "cityHidden", "taskABA"} {
		t.Run(change, func(t *testing.T) {
			f, city, places := liveReplyHistoryNativeFixture(t)
			b := f.configuration.native.private.base
			task, _, _ := liveReplyHistoryNativeComplete(t, f)
			access := liveReplyHistoryNativeAccess(t, f, task)
			read, e := b.store.ReadOwnMessageResults(b.ctx, access)
			if e != nil || len(read.Results()) != 1 {
				t.Fatal("fresh baseline", e)
			}
			switch change {
			case "sourceHidden":
				b.exec(`UPDATE places SET publication_status='hidden' WHERE id=$1`, places[0])
			case "sourceABA":
				b.exec(`UPDATE places SET name=name WHERE id=$1`, places[0])
			case "ownerInactive":
				b.exec(`UPDATE accounts SET status='suspended' WHERE id=$1`, b.person.ID)
			case "sessionRevoked":
				b.exec(`UPDATE sessions SET revoked_at=clock_timestamp() WHERE id=$1`, f.configuration.native.private.ownerSession)
			case "cityHidden":
				b.exec(`UPDATE cities SET publication_status='hidden' WHERE id=$1`, city)
			case "taskABA":
				b.exec(`UPDATE agent_tasks SET query=query WHERE id=$1`, task.ID)
			}
			if read.Revalidate(b.ctx) == nil {
				t.Fatal("stale native history released after ACL/source epoch change")
			}
			if change == "sourceHidden" {
				next, e := b.store.ReadOwnMessageResults(b.ctx, access)
				if e != nil || len(next.Results()) != 1 || len(next.Results()[0].ResultSet.Items) != 0 || len(task.Conversation[len(task.Conversation)-1].ResultMembership.Refs) != 1 {
					t.Fatal("withdrawal replaced persisted refs or joined another place", e)
				}
			}
		})
	}
}

func TestModelLiveReplyHistoryInvitedActivityNeverExportedIntegration(t *testing.T) {
	f, city, _ := liveReplyHistoryNativeFixture(t)
	b := f.configuration.native.private.base
	activity, e := b.store.CreateSocialDraft(b.ctx, b.other.ID, activitypublish.Input{Organizer: activitypublish.Organizer{Type: "PERSON", ID: b.other.ID}, CityID: city, Title: "UNIT_INVITED_ACTIVITY_NOT_MODEL", Summary: "UNIT private authorized human activity", Description: "UNIT_PRIVATE_BODY_NOT_MODEL", CategoryCode: "badminton", Visibility: "invite_only", Modality: "online", PhysicalPlaceStatus: "not_applicable", StartsAt: time.Now().UTC().Add(time.Hour), EndsAt: time.Now().UTC().Add(2 * time.Hour), TimeZone: "Europe/London"})
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() {
		if _, e := b.pool.Exec(context.Background(), `DELETE FROM activities WHERE id=$1 AND created_by_account_id=$2`, activity.ID, b.other.ID); e != nil {
			t.Error(e)
		}
	})
	if _, e = b.store.PublishSocialActivity(b.ctx, b.other.ID, activity.ID); e != nil {
		t.Fatal(e)
	}
	if e = b.store.InviteActivityPerson(b.ctx, b.other.ID, activity.ID, b.person.ID); e != nil {
		t.Fatal(e)
	}
	task := f.configuration.native.task
	task.Intent = agentworkspace.FindActivity
	task.Filters["currentQuery"], task.Filters["targetIntent"], task.Filters["searchTerm"], task.Filters["category"] = "找羽毛球活动", agentworkspace.FindActivity, "", "badminton"
	task.Conversation[len(task.Conversation)-1].Text = task.Filters["currentQuery"]
	f.configuration.native.task, e = b.store.UpdateTask(b.ctx, task)
	if e != nil {
		t.Fatal(e)
	}
	task, _, modelWire := liveReplyHistoryNativeComplete(t, f)
	member := task.Conversation[len(task.Conversation)-1].ResultMembership
	if !reflect.DeepEqual(member.Refs, []arp.Ref{{Type: "activity", ID: activity.ID}}) {
		t.Fatal("legal human invitation was removed from membership")
	}
	for _, canary := range []string{activity.ID, activity.Title, "UNIT_PRIVATE_BODY_NOT_MODEL"} {
		if strings.Contains(string(modelWire), canary) {
			t.Fatal("invited human result escaped into model wire")
		}
	}
	// The second approved MODEL request is the actual history boundary: its
	// original Task already contains the first invited human membership.
	secondStart := task
	secondStart.Status = agentworkspace.TaskActive
	secondStart.Filters["currentQuery"] = "还有羽毛球活动"
	secondStart.Conversation = append(secondStart.Conversation, agentworkspace.Message{Role: "user", Text: secondStart.Filters["currentQuery"]})
	f.configuration.native.task, e = b.store.UpdateTask(b.ctx, secondStart)
	if e != nil {
		t.Fatal("same Task invited history follow-up", e)
	}
	task, _, secondWire := liveReplyHistoryNativeComplete(t, f)
	for _, canary := range []string{activity.ID, activity.Title, "UNIT_PRIVATE_BODY_NOT_MODEL"} {
		if strings.Contains(string(secondWire), canary) {
			t.Fatal("persisted invited history escaped into second model wire")
		}
	}
	access := liveReplyHistoryNativeAccess(t, f, task)
	read, e := b.store.ReadOwnMessageResults(b.ctx, access)
	if e != nil || len(read.Results()) != 2 || len(read.Results()[0].ResultSet.Items) != 1 {
		t.Fatal("authorized invited history", e)
	}
	b.exec(`UPDATE activity_invitations SET status='revoked' WHERE activity_id=$1 AND invitee_account_id=$2`, activity.ID, b.person.ID)
	if read.Revalidate(b.ctx) == nil {
		t.Fatal("revoked invitation released old card")
	}
	next, e := b.store.ReadOwnMessageResults(b.ctx, access)
	if e != nil || len(next.Results()) != 2 || len(next.Results()[0].ResultSet.Items) != 0 || len(next.Results()[1].ResultSet.Items) != 0 || len(member.Refs) != 1 {
		t.Fatal("fresh history ignored original invited ACL", e)
	}
}

func TestModelLiveReplyHistoryMalformedNativeBoundsDoNotFinishIntegration(t *testing.T) {
	f, _, _ := liveReplyHistoryNativeFixture(t)
	b := f.configuration.native.private.base
	task := f.configuration.native.task
	task.Filters["mapWest"] = "-2.2"
	var e error
	f.configuration.native.task, e = b.store.UpdateTask(b.ctx, task)
	if e != nil {
		t.Fatal(e)
	}
	h := resolvedLiveNativeRun(t, f)
	_, _, calls := liveSourceReplyCompleteFixture(t, f, h)
	before := liveSourceReplyReadTask(t, f, h.search.TaskID)
	if reply, e := h.FinalizeSourceReply(b.ctx); e == nil || reply != nil {
		t.Fatal("malformed human query silently finished text-only")
	}
	liveSourceReplyRequireNoTaskWrite(t, f, h, before)
	if *calls != 1 {
		t.Fatal("denied finalization invoked provider again")
	}
}
