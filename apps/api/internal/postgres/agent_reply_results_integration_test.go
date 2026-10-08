package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	arp "github.com/birdtie/birdtie/apps/api/internal/agentresultprojection"
	"github.com/birdtie/birdtie/apps/api/internal/agentworkspace"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
)

func replyNativeAccess(t *testing.T, f *contextBuilderNativeFixture, task agentworkspace.Task) arp.Access {
	t.Helper()
	raw, e := json.Marshal(agentworkspace.SanitizeTaskForResponse(task))
	if e != nil {
		t.Fatal(e)
	}
	return arp.Access{Actor: identity.Actor{ID: f.place.private.base.person.ID, AccountType: "person"}, SessionDigest: f.place.private.owner.SessionDigest, TaskID: task.ID, ExpectedTask: raw}
}

func replyNativeStart(t *testing.T, f *contextBuilderNativeFixture, kind, term string) agentworkspace.Task {
	t.Helper()
	b := f.place.private.base
	task := f.task
	task.Status = agentworkspace.TaskActive
	task.Intent = map[string]string{"place": agentworkspace.FindPlace, "activity": agentworkspace.FindActivity, "organization": agentworkspace.FindOrganization}[kind]
	query := "本轮明确查询 " + kind + " " + term
	task.Filters = map[string]string{"currentQuery": query, "searchTerm": term, "timePreference": "anytime"}
	// Preserve old messages; fixture's original old private text remains history.
	task.Conversation = append(task.Conversation, agentworkspace.Message{Role: "user", Text: query})
	var e error
	task, e = b.store.UpdateTask(b.ctx, task)
	if e != nil {
		t.Fatal(e)
	}
	f.task = task
	return task
}

func replyNativeComplete(t *testing.T, f *contextBuilderNativeFixture, kind, term string) agentworkspace.Task {
	t.Helper()
	task := replyNativeStart(t, f, kind, term)
	b := f.place.private.base
	var e error
	started := time.Now()
	task, e = b.store.CaptureOwnHumanReply(b.ctx, replyNativeAccess(t, f, task))
	if e != nil {
		t.Fatal("native human reply capture", e)
	}
	if task.Status != agentworkspace.TaskCompleted || agentworkspace.ValidateReplyMembership(task, len(task.Conversation)-1) != nil {
		t.Fatal("original completed Task membership absent")
	}
	f.task = task
	t.Logf("LOCAL_SYNTHETIC native rules capture kind=%s elapsed_ms=%d", kind, time.Since(started).Milliseconds())
	return task
}

func replyNativeEntries(t *testing.T, f *contextBuilderNativeFixture, store *Store) (agentworkspace.MessageResultsRead, []agentworkspace.MessageResult) {
	t.Helper()
	b := f.place.private.base
	started := time.Now()
	r, e := store.ReadOwnMessageResults(b.ctx, replyNativeAccess(t, f, f.task))
	if e != nil {
		t.Fatal("owned fresh historical source read", e)
	}
	entries := r.Results()
	t.Logf("LOCAL_SYNTHETIC native historical read messages=%d elapsed_ms=%d", len(entries), time.Since(started).Milliseconds())
	return r, entries
}

func TestAgentReplyResultsNativeTwoTurnsRestartAndNoNewJoin(t *testing.T) {
	f := contextBuilderNative(t)
	b := f.place.private.base
	b.exec(`UPDATE places SET name='FirstOnly',coordinate_system='wgs84',location_precision='point',latitude=57.15,longitude=-2.1 WHERE id=$1`, f.place.place)
	b.exec(`UPDATE places SET name='SecondOnly',coordinate_system='wgs84',location_precision='point',latitude=57.16,longitude=-2.11 WHERE id=$1`, f.place.otherPlace)
	first := replyNativeComplete(t, f, "place", "FirstOnly")
	firstMember := first.Conversation[len(first.Conversation)-1].ResultMembership
	second := replyNativeComplete(t, f, "place", "SecondOnly")
	secondMember := second.Conversation[len(second.Conversation)-1].ResultMembership
	if reflect.DeepEqual(firstMember.Refs, secondMember.Refs) || len(firstMember.Refs) != 1 || len(secondMember.Refs) != 1 {
		t.Fatal("two distinct original memberships not captured")
	}
	var third string
	if e := b.pool.QueryRow(b.ctx, `INSERT INTO places(id,city_id,name,category_code,publication_status,source_label,source_ref,maintainer_label,maintainer_account_id) VALUES(gen_random_uuid(),$1,'SecondOnly newly published','sports','published','LOCAL_SYNTHETIC_FIXTURE','disposable://reply-history','合成',$2) RETURNING id`, f.place.city, b.other.ID).Scan(&third); e != nil {
		t.Fatal(e)
	}
	// A new process-local Store cannot recover an old receipt; it must read the
	// persisted bounded membership and rebuild only currently visible originals.
	restarted := New(b.pool, false)
	restored, e := restarted.GetTask(b.ctx, b.person.ID, second.ID)
	if e != nil {
		t.Fatal(e)
	}
	f.task = restored
	h, entries := replyNativeEntries(t, f, restarted)
	if len(entries) != 2 {
		t.Fatal("two stored rounds lost", len(entries))
	}
	for i, want := range []string{f.place.place, f.place.otherPlace} {
		entry := entries[i]
		if len(entry.ResultSet.Items) != 1 || entry.ResultSet.Items[0].Entity.ID != want || entry.ResultSet.ID != []string{firstMember.ResultSetID, secondMember.ResultSetID}[i] || len(entry.MapEffects.PinEntityIDs) != 1 || entry.MapEffects.PinEntityIDs[0] != "place:"+want {
			t.Fatal("same message/card/map membership failed", i)
		}
		if !entry.ValidUntil.After(entry.ResultSet.GeneratedAt) || entry.ValidUntil.After(entry.ResultSet.GeneratedAt.Add(30*time.Second)) || len(entry.ResultSet.Items[0].Actions) != 6 {
			t.Fatal("fresh native action/deadline missing")
		}
	}
	started := time.Now()
	if e = h.Revalidate(b.ctx); e != nil {
		t.Fatal("fresh read failed own final revalidation", e)
	}
	t.Logf("LOCAL_SYNTHETIC native historical final tail elapsed_ms=%d", time.Since(started).Milliseconds())
	// Response mutation cannot alter the private source proof or future reads.
	entries[0].ResultSet.Items[0].Title = "UNTRUSTED_RESPONSE_MUTATION"
	if h.Results()[0].ResultSet.Items[0].Title == entries[0].ResultSet.Items[0].Title {
		t.Fatal("private receipt aliases serialized mutable response")
	}
	var before, after int
	if e = b.pool.QueryRow(b.ctx, `SELECT count(*) FROM native_notification_decisions WHERE actor_id=$1 OR recipient_id=$1`, b.person.ID).Scan(&before); e != nil {
		t.Fatal(e)
	}
	again, e := b.store.CaptureOwnHumanReply(b.ctx, replyNativeAccess(t, f, restored))
	if e != nil {
		t.Fatal("exact completed retry", e)
	}
	if e = b.pool.QueryRow(b.ctx, `SELECT count(*) FROM native_notification_decisions WHERE actor_id=$1 OR recipient_id=$1`, b.person.ID).Scan(&after); e != nil {
		t.Fatal(e)
	}
	if !reflect.DeepEqual(again, restored) || before != after {
		t.Fatal("retry appended message or notification")
	}
	var modelWrites int
	if e = b.pool.QueryRow(b.ctx, `SELECT (SELECT count(*) FROM model_request_runs WHERE owner_id=$1)+(SELECT count(*) FROM model_egress_previews WHERE owner_id=$1)`, b.person.ID).Scan(&modelWrites); e != nil || modelWrites != 0 {
		t.Fatal("human history created model/provider permission", modelWrites, e)
	}
}

func TestAgentReplyResultsNativeCurrentPermissionAndSourceWithdrawal(t *testing.T) {
	for _, change := range []string{"sourceHidden", "sourceExpired", "sourceABA", "invitationRevoked", "ownerInactive", "sessionRevoked", "cityHidden", "cityExpired", "taskABA"} {
		t.Run(change, func(t *testing.T) {
			f := contextBuilderNative(t)
			b := f.place.private.base
			kind := "place"
			if change == "invitationRevoked" {
				kind = "activity"
			}
			replyNativeComplete(t, f, kind, "")
			h, before := replyNativeEntries(t, f, b.store)
			if len(before) != 1 || len(before[0].ResultSet.Items) != 2 {
				t.Fatal("original current native set missing")
			}
			switch change {
			case "sourceHidden":
				b.exec(`UPDATE places SET publication_status='hidden' WHERE id=$1`, f.place.place)
			case "sourceExpired":
				b.exec(`UPDATE places SET expires_at=clock_timestamp()-interval '1 second' WHERE id=$1`, f.place.place)
			case "sourceABA":
				b.exec(`UPDATE places SET name=name WHERE id=$1`, f.place.place)
			case "invitationRevoked":
				b.exec(`UPDATE activity_invitations SET status='revoked' WHERE activity_id=$1 AND invitee_account_id=$2`, f.private, b.person.ID)
			case "ownerInactive":
				b.exec(`UPDATE accounts SET status='suspended' WHERE id=$1`, b.person.ID)
			case "sessionRevoked":
				b.exec(`UPDATE sessions SET revoked_at=clock_timestamp() WHERE id=$1`, f.place.private.ownerSession)
			case "cityHidden":
				b.exec(`UPDATE cities SET publication_status='hidden' WHERE id=$1`, f.place.city)
			case "cityExpired":
				b.exec(`UPDATE cities SET expires_at=clock_timestamp()-interval '1 second' WHERE id=$1`, f.place.city)
			case "taskABA":
				b.exec(`UPDATE agent_tasks SET updated_at=updated_at WHERE id=$1`, f.task.ID)
			}
			if e := h.Revalidate(b.ctx); e == nil {
				t.Fatal("stale projection released after native epoch/source change")
			}
			if change == "sourceHidden" || change == "sourceExpired" || change == "invitationRevoked" {
				_, after := replyNativeEntries(t, f, b.store)
				if len(after) != 1 || len(after[0].ResultSet.Items) != 1 || len(f.task.Conversation[len(f.task.Conversation)-1].ResultMembership.Refs) != 2 {
					t.Fatal("withdrawal replaced original membership or missed ACL")
				}
			}
		})
	}
}

func TestAgentReplyResultsNativeOwnedBindingAndOriginalCurrentGuard(t *testing.T) {
	f := contextBuilderNative(t)
	b := f.place.private.base
	task := replyNativeStart(t, f, "place", "")
	a := replyNativeAccess(t, f, task)
	wrong := a
	wrong.Actor.ID = b.other.ID
	wrong.SessionDigest = f.place.private.peer.SessionDigest
	if _, e := b.store.CaptureOwnHumanReply(b.ctx, wrong); e == nil {
		t.Fatal("other owner captured task")
	}
	stale := task
	stale.Filters["searchTerm"] = "not persisted"
	raw, _ := json.Marshal(agentworkspace.SanitizeTaskForResponse(stale))
	wrong = a
	wrong.ExpectedTask = raw
	if _, e := b.store.CaptureOwnHumanReply(b.ctx, wrong); !errors.Is(e, arp.ErrChanged) {
		t.Fatal("unsaved filters bypassed ACTIVE CAS", e)
	}
	// Restore exact original saved Task and verify the ordinary current reader
	// still rejects a caller-selected historical kind/filter.
	task, e := b.store.GetTask(b.ctx, b.person.ID, task.ID)
	if e != nil {
		t.Fatal(e)
	}
	f.task = task
	a = replyNativeAccess(t, f, task)
	if _, e = b.store.ReadAgentResultProjection(b.ctx, a, arp.Query{CityID: task.CityID, Kind: "activity", CompareIDs: []string{}}); !errors.Is(e, arp.ErrDenied) {
		t.Fatal("history weakened original current-kind guard", e)
	}
	task, e = b.store.CaptureOwnHumanReply(b.ctx, a)
	if e != nil {
		t.Fatal(e)
	}
	f.task = task
	wrong = replyNativeAccess(t, f, task)
	wrong.Actor.ID = b.other.ID
	wrong.SessionDigest = f.place.private.peer.SessionDigest
	if _, e = b.store.ReadOwnMessageResults(b.ctx, wrong); e == nil {
		t.Fatal("other owner restored history")
	}
	// Prefix mutation invalidates correlation; never recover it with newest refs.
	task.Conversation[0].Text = "modified old prefix"
	task, e = b.store.UpdateTask(b.ctx, task)
	if e != nil {
		t.Fatal(e)
	}
	f.task = task
	_, entries := replyNativeEntries(t, f, b.store)
	if len(entries) != 0 {
		t.Fatal("invalid old prefix membership was grafted to current set")
	}
}

func TestAgentReplyResultsNativeLegacyNoRetrofit(t *testing.T) {
	f := contextBuilderNative(t)
	b := f.place.private.base
	task := f.task
	task.Status = agentworkspace.TaskCompleted
	task.Conversation = append(task.Conversation, agentworkspace.Message{Role: "assistant", Text: "原历史答复"})
	var e error
	task, e = b.store.UpdateTask(b.ctx, task)
	if e != nil {
		t.Fatal(e)
	}
	f.task = task
	_, entries := replyNativeEntries(t, f, b.store)
	if entries == nil || len(entries) != 0 {
		t.Fatal("legacy history got current entities")
	}
	var actual int
	if e = b.pool.QueryRow(b.ctx, `SELECT jsonb_array_length(conversation) FROM agent_tasks WHERE id=$1`, task.ID).Scan(&actual); e != nil || actual != len(task.Conversation) {
		t.Fatal("history GET wrote conversation", e)
	}
}

func TestAgentReplyResultsNativeActivityHumanACLNotModelEgress(t *testing.T) {
	f := contextBuilderNative(t)
	b := f.place.private.base
	task := replyNativeComplete(t, f, "activity", "")
	m := task.Conversation[len(task.Conversation)-1].ResultMembership
	if len(m.Refs) != 2 || task.Conversation[len(task.Conversation)-1].Text != "找到 2 个当前可见活动。" {
		t.Fatal("lawful invited activity removed or advertised as public")
	}
	_, entries := replyNativeEntries(t, f, b.store)
	if len(entries[0].Activities) != 2 {
		t.Fatal("original human Activity DTO capability lost")
	}
}

func TestAgentReplyResultsNativeFinalBatchCoversEarlierOrganization(t *testing.T) {
	f := contextBuilderNative(t)
	b := f.place.private.base
	b.exec(`UPDATE organizations SET visibility='public',verification_status='verified' WHERE id=$1`, b.orgID)
	b.exec(`INSERT INTO organization_map_locations(organization_id,city_id,latitude,longitude,visibility,review_status,submitted_by,reviewed_by,reviewed_at) VALUES($1,$2,57.15,-2.1,'public','approved',$3,$4,clock_timestamp())`, b.orgID, f.place.city, b.person.ID, b.other.ID)
	t.Cleanup(func() {
		b.pool.Exec(context.Background(), `DELETE FROM organization_map_locations WHERE organization_id=$1`, b.orgID)
	})
	replyNativeComplete(t, f, "organization", "")
	replyNativeComplete(t, f, "place", "")
	read, entries := replyNativeEntries(t, f, b.store)
	if len(entries) != 2 || len(entries[0].ResultSet.Items) != 1 || entries[0].ResultSet.Items[0].Entity.Type != "organization" {
		t.Fatal("earlier map-only organization fixture missing")
	}
	h := read.(*nativeMessageResultsRead).state()
	// Exercise the FINAL atomic batch against receipts observed earlier in the
	// transaction/read. A later Place receipt cannot stand in for an earlier Org.
	b.exec(`UPDATE organizations SET visibility='private' WHERE id=$1`, b.orgID)
	tx, _, policy, e := b.store.beginCurrentToolRead(b.ctx, h.access.Actor, h.access.SessionDigest, resultProjectionRelations)
	if e != nil {
		t.Fatal(e)
	}
	defer tx.Rollback(context.Background())
	current, e := ownReplyTaskTx(b.ctx, tx, h.access, false)
	if e != nil {
		t.Fatal(e)
	}
	if e = b.store.finalReplySourceProofsTx(b.ctx, tx, h.access, current, policy, h); !errors.Is(e, arp.ErrChanged) {
		t.Fatal("final snapshot ignored earlier source withdrawal", e)
	}
}

func TestAgentReplyResultsNativeEmptyHistoryAuthorityABAAndCityDeadline(t *testing.T) {
	for _, change := range []string{"accountABA", "agentABA", "profileRevision", "cityABA", "contextABA", "cityExpired"} {
		t.Run(change, func(t *testing.T) {
			f := contextBuilderNative(t)
			b := f.place.private.base
			read, entries := replyNativeEntries(t, f, b.store)
			if len(entries) != 0 {
				t.Fatal("expected original legacy empty history")
			}
			switch change {
			case "accountABA":
				b.exec(`UPDATE accounts SET status=status WHERE id=$1`, b.person.ID)
			case "agentABA":
				b.exec(`UPDATE agents SET status=status WHERE id=$1`, b.personID)
			case "profileRevision":
				// The original metadata guard prohibits same-version rewrites;
				// exercise the valid +1 generation, never disable its trigger.
				b.exec(`UPDATE agent_profiles SET profile_version=profile_version+1 WHERE agent_id=$1`, b.personID)
			case "cityABA":
				b.exec(`UPDATE cities SET name=name WHERE id=$1`, f.place.city)
			case "contextABA":
				b.exec(`UPDATE contexts SET context_type=context_type WHERE id=$1`, f.task.ContextID)
			case "cityExpired":
				b.exec(`UPDATE cities SET expires_at=clock_timestamp()-interval '1 second' WHERE id=$1`, f.place.city)
			}
			if e := read.Revalidate(b.ctx); e == nil {
				t.Fatal("empty history bypassed native authority epoch/lifetime")
			}
		})
	}
}

func TestAgentReplyResultsNativeOpaqueHandleCannotSerializeOrFormatPrivateState(t *testing.T) {
	state := &nativeMessageResultsState{taskToken: "PRIVATE_TOKEN_CANARY", access: arp.Access{ExpectedTask: json.RawMessage(`{"conversation":"PRIVATE_TEXT_CANARY"}`)}}
	h := &nativeMessageResultsRead{read: func() *nativeMessageResultsState { return state }}
	if _, e := json.Marshal(h); e == nil {
		t.Fatal("native read marshaled into a reconstructable assertion")
	}
	for _, verb := range []string{"%v", "%+v", "%#v", "%s", "%q", "%x", "%w"} {
		rendered := fmt.Sprintf(verb, h)
		if strings.Contains(rendered, "CANARY") || strings.Contains(rendered, "ExpectedTask") || strings.Contains(rendered, "SessionDigest") {
			t.Fatal("native handle leaked private fields", verb)
		}
	}
	if e := json.Unmarshal([]byte(`{"access":{"ExpectedTask":{"conversation":"evil"}}}`), h); e == nil || h.read != nil {
		t.Fatal("JSON restored native read authority")
	}
}

func TestAgentReplyResultsNativeElapsedDeadlineRejectsExpiryDuringFinalBatch(t *testing.T) {
	started := time.Now()
	observed := time.Date(2026, 10, 8, 0, 0, 0, 0, time.UTC)
	h := &nativeMessageResultsState{checkedAt: started, observed: observed}
	for _, test := range []struct {
		name    string
		budget  time.Duration
		elapsed time.Duration
		want    bool
	}{
		{"before_deadline", time.Second, time.Millisecond, true},
		{"empty_history_city_expired_during_batch", time.Millisecond, 2 * time.Millisecond, false},
		{"source_expired_during_batch", 100 * time.Millisecond, 200 * time.Millisecond, false},
		{"original_handle_expires_during_revalidation", 10 * time.Millisecond, 11 * time.Millisecond, false},
		{"at_deadline", time.Second, time.Second, false},
		{"already_expired", -time.Millisecond, 0, false},
		{"backward_local_clock", time.Second, -time.Millisecond, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := nativeReplyReadUnexpired(h, observed.Add(test.budget), started.Add(test.elapsed)); got != test.want {
				t.Fatal("final native lifetime verdict differs", got, test.want)
			}
		})
	}
	if nativeReplyReadUnexpired(nil, observed.Add(time.Hour), started) || nativeReplyReadUnexpired(&nativeMessageResultsState{}, observed.Add(time.Hour), started) {
		t.Fatal("missing native observation accepted")
	}
}

func TestAgentReplyResultsNativeBoundedThirtyMessagesThirtyRefs(t *testing.T) {
	f := contextBuilderNative(t)
	b := f.place.private.base
	for i := 0; i < 28; i++ {
		b.exec(`INSERT INTO places(id,city_id,name,category_code,publication_status,source_label,source_ref,maintainer_label,maintainer_account_id) VALUES(gen_random_uuid(),$1,$2,'sports','published','LOCAL_SYNTHETIC_FIXTURE','disposable://history-bound','合成',$3)`, f.place.city, fmt.Sprintf("Bounded %02d", i), b.other.ID)
	}
	task := replyNativeComplete(t, f, "place", "")
	refs := task.Conversation[len(task.Conversation)-1].ResultMembership.Refs
	if len(refs) != 30 {
		t.Fatal("actual first native projection did not reach existing 30-ref limit")
	}
	// The added historical rows are synthetic owned DB test data. Their refs
	// come from the original native projection above, and every ref is reread.
	for i := 0; i < 31; i++ {
		task.Conversation = append(task.Conversation, agentworkspace.Message{Role: "user", Text: fmt.Sprintf("合成历史轮次 %d", i)}, agentworkspace.Message{Role: "assistant", Text: "合成历史结果"})
		index := len(task.Conversation) - 1
		m, e := agentworkspace.NewReplyMembership(task, index, "place", refs)
		if e != nil {
			t.Fatal(e)
		}
		task.Conversation[index].ResultMembership = m
	}
	var e error
	task, e = b.store.UpdateTask(b.ctx, task)
	if e != nil {
		t.Fatal(e)
	}
	f.task = task
	started := time.Now()
	_, entries := replyNativeEntries(t, f, b.store)
	elapsed := time.Since(started)
	if len(entries) != 30 || entries[0].MessageIndex != len(task.Conversation)-59 {
		t.Fatal("latest bounded message window incorrect", len(entries))
	}
	for _, entry := range entries {
		if len(entry.ResultSet.Items) != 30 {
			t.Fatal("original refs truncated or replaced in history")
		}
	}
	if elapsed >= 30*time.Second {
		t.Fatal("maximum history read exceeded original 30-second authority interval")
	}
	t.Logf("LOCAL_SYNTHETIC actual bounded history messages=30 refs_per_message=30 elapsed_ms=%d", elapsed.Milliseconds())
}
