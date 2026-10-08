package postgres

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"

	arp "github.com/birdtie/birdtie/apps/api/internal/agentresultprojection"
	"github.com/birdtie/birdtie/apps/api/internal/agenttool"
	"github.com/birdtie/birdtie/apps/api/internal/agentworkspace"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	"github.com/birdtie/birdtie/apps/api/internal/modelegressbudget"
)

// Original disposable PostgreSQL/native readers and ledger; every place,
// passage, credential and provider transport is UNIT synthetic. The worker
// compiles only. Root runs these exact tests on its isolated database.
func placeFollowupDBFixture(t *testing.T) (*liveNativeFixture, string, string) {
	t.Helper()
	f, city := resolvedLiveNativeFixture(t)
	b := f.configuration.native.private.base
	gallery, museum := liveNativeID(t, f), liveNativeID(t, f)
	for i, id := range []string{gallery, museum} {
		name := []string{"UNIT Aberdeen Art Gallery", "UNIT Aberdeen Maritime Museum"}[i]
		b.exec(`INSERT INTO places(id,city_id,name,category_code,latitude,longitude,location_precision,publication_status,source_label,source_ref,maintainer_label,maintainer_account_id) VALUES($1,$2,$3,'sports',57.15,-2.09,'point','published','UNIT_PUBLIC_SOURCE','https://example.com/unit-public-place','合成维护者',$4)`, id, city, name, b.other.ID)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if _, e := b.pool.Exec(ctx, `DELETE FROM places WHERE id=ANY($1::uuid[])`, []string{gallery, museum}); e != nil {
			t.Error("owned place followup fixture cleanup")
		}
	})
	task := f.configuration.native.task
	task.Intent, task.Status = agentworkspace.FindPlace, agentworkspace.TaskActive
	query := "找地点 Gallery"
	task.Filters = map[string]string{"currentQuery": query, "targetIntent": agentworkspace.FindPlace, "searchTerm": "Gallery", "locationPreference": "city"}
	task.Conversation = append(task.Conversation, agentworkspace.Message{Role: "user", Text: query})
	var e error
	task, e = b.store.UpdateTask(b.ctx, task)
	if e != nil {
		t.Fatal("original active place Task")
	}
	task, e = b.store.CaptureOwnHumanReply(b.ctx, placeFollowupDBAccess(t, f, task))
	if e != nil || agentworkspace.ValidateReplyMembership(task, len(task.Conversation)-1) != nil || len(task.Conversation[len(task.Conversation)-1].ResultMembership.Refs) != 1 {
		t.Fatal("original native first reply membership", e)
	}
	f.configuration.native.task = task
	return f, gallery, museum
}

func placeFollowupDBAccess(t *testing.T, f *liveNativeFixture, task agentworkspace.Task) arp.Access {
	t.Helper()
	b := f.configuration.native.private.base
	raw, e := json.Marshal(agentworkspace.SanitizeTaskForResponse(task))
	if e != nil {
		t.Fatal(e)
	}
	return arp.Access{Actor: identity.Actor{ID: b.person.ID, AccountType: "person"}, SessionDigest: f.configuration.native.access.SessionDigest, TaskID: task.ID, ExpectedTask: raw}
}

func placeFollowupDBContinue(t *testing.T, f *liveNativeFixture, question string) agentworkspace.Task {
	t.Helper()
	b := f.configuration.native.private.base
	previous := f.configuration.native.task
	next, e := b.store.ContinueOwnPublicPlace(b.ctx, placeFollowupDBAccess(t, f, previous), question)
	if e != nil || next.ID != previous.ID || next.Query != previous.Query || next.Status != agentworkspace.TaskActive || next.Intent != agentworkspace.FindPlace || len(next.Conversation) != len(previous.Conversation)+1 || next.Conversation[len(next.Conversation)-1].Text != question {
		t.Fatal("original owned raw place continuation", e)
	}
	for i := range previous.Conversation {
		if !reflect.DeepEqual(previous.Conversation[i], next.Conversation[i]) {
			t.Fatal("continuation rewrote an earlier message or native membership")
		}
	}
	f.configuration.native.task = next
	return next
}

func TestPlaceFollowupDBRealNativeTwoQuestionsAndOriginalLivePipeline(t *testing.T) {
	f, gallery, museum := placeFollowupDBFixture(t)
	b := f.configuration.native.private.base
	originalTaskID := f.configuration.native.task.ID
	for turn, question := range []string{"它周末几点开门？", "那 Maritime Museum 呢？"} {
		next := placeFollowupDBContinue(t, f, question)
		id, name := []string{gallery, museum}[turn], []string{"UNIT Aberdeen Art Gallery", "UNIT Aberdeen Maritime Museum"}[turn]
		if next.Filters[agentworkspace.PlaceFollowupIDFilter] != id || next.Filters["searchTerm"] != name || next.Filters[agentworkspace.PlaceFollowupTopicFilter] != agentworkspace.PlaceOpeningHours || next.Filters["timePreference"] != "weekend" {
			t.Fatal("current public entity/name/opening topic/weekend did not resolve")
		}
		h := resolvedLiveNativeRun(t, f)
		if h.original.query != question || h.original.resolved.place.id != id || h.original.resolved.context.ResolvedSlots.SearchTerm != name+" opening hours" || !strings.Contains(h.original.SearchQuery(), "weekend "+name+" opening hours") || h.original.SearchQuery() == question {
			t.Fatal("actual new WSA Query did not use the public subject/current question")
		}
		_, preview := liveSourceRunBindFixture(t, f, h)
		wire := preview.Prepared().ModelWire().ExactWire()
		for _, forbidden := range []string{"PRIVATE_HISTORY_CANARY", "PRIVATE_ASSISTANT_CANARY", agentworkspace.PlaceFollowupIDFilter, agentworkspace.PlaceFollowupGenerationFilter, "\"conversation\"", "\"row\"", id} {
			if bytes.Contains(wire, []byte(forbidden)) {
				t.Fatal("private history/native generation or place ID entered model bytes")
			}
		}
		if !bytes.Contains(wire, []byte(name+" opening hours")) {
			t.Fatal("public opening topic omitted from actual prepared model envelope")
		}
		resolvedNativeCompleteModel(t, f, h, preview)
		reply, e := h.FinalizeSourceReply(b.ctx)
		if e != nil || reply.Task().ID != originalTaskID || reply.Task().Status != agentworkspace.TaskCompleted || reply.Revalidate(b.ctx) != nil {
			t.Fatal("same original Task sourced answer/final current proof", e)
		}
		f.configuration.native.task = reply.Task()
		member := f.configuration.native.task.Conversation[len(f.configuration.native.task.Conversation)-1].ResultMembership
		if member == nil || len(member.Refs) != 1 || member.Refs[0] != (arp.Ref{Type: "place", ID: id}) {
			t.Fatal("fresh completed answer did not preserve the same native card/map subject")
		}
		views, e := b.store.ReadOwnModelBudget(b.ctx, f.configuration.native.access, h.search.RootTraceID, originalTaskID)
		if e != nil || len(views) != 4 {
			t.Fatal("original four budget layers", e)
		}
		for _, v := range views {
			want := int64(279680)
			if v.Scope == "TENANT_PERSON" || v.Scope == "SUBJECT_PERSON" {
				want *= int64(turn + 1)
			}
			if v.Allocated.CostMicros != want || (v.Scope == "ROOT" && v.Allocated.Requests != 2) {
				t.Fatal("new explicit turn reset accumulated holds or exceeded original two-request root")
			}
		}
	}
}

func TestPlaceFollowupDBDenialBeforeTaskMutationOrProvider(t *testing.T) {
	for _, mode := range []string{"ambiguous_membership", "missing_latest_membership", "partial_retained_proof", "hidden", "expired", "future_place", "renamed_subject", "retained_generation_changed", "wrong_session", "wrong_actor", "stale_task", "named_missing", "named_ambiguous", "named_hidden", "named_foreign_city"} {
		t.Run(mode, func(t *testing.T) {
			f, gallery, museum := placeFollowupDBFixture(t)
			b := f.configuration.native.private.base
			task, question := f.configuration.native.task, "它周末几点开门？"
			switch mode {
			case "ambiguous_membership":
				i := len(task.Conversation) - 1
				m, e := agentworkspace.NewReplyMembership(task, i, "place", []arp.Ref{{Type: "place", ID: gallery}, {Type: "place", ID: museum}})
				if e != nil {
					t.Fatal(e)
				}
				task.Conversation[i].ResultMembership = m
				var writeErr error
				task, writeErr = b.store.UpdateTask(b.ctx, task)
				if writeErr != nil {
					t.Fatal("owned ambiguous membership fixture", writeErr)
				}
			case "missing_latest_membership":
				task.Conversation = append(task.Conversation, agentworkspace.Message{Role: "user", Text: "UNIT new unrelated query"}, agentworkspace.Message{Role: "assistant", Text: "UNIT unbound reply"})
				var writeErr error
				task, writeErr = b.store.UpdateTask(b.ctx, task)
				if writeErr != nil {
					t.Fatal("owned latest unbound reply fixture", writeErr)
				}
			case "partial_retained_proof":
				task.Filters[agentworkspace.PlaceFollowupGenerationFilter] = strings.Repeat("a", 64)
				var writeErr error
				task, writeErr = b.store.UpdateTask(b.ctx, task)
				if writeErr != nil {
					t.Fatal("owned partial retained proof fixture", writeErr)
				}
			case "hidden":
				b.exec(`UPDATE places SET publication_status='hidden' WHERE id=$1`, gallery)
			case "expired":
				b.exec(`UPDATE places SET expires_at=clock_timestamp()-interval '1 second' WHERE id=$1`, gallery)
			case "future_place":
				b.exec(`UPDATE places SET updated_at=clock_timestamp()+interval '1 hour' WHERE id=$1`, gallery)
			case "renamed_subject":
				b.exec(`UPDATE places SET name='UNIT different public Museum' WHERE id=$1`, gallery)
			case "retained_generation_changed":
				active := placeFollowupDBContinue(t, f, question)
				var writeErr error
				task, writeErr = b.store.CaptureOwnHumanReply(b.ctx, placeFollowupDBAccess(t, f, active))
				if writeErr != nil {
					t.Fatal("owned retained generation fixture", writeErr)
				}
				b.exec(`UPDATE places SET name=name WHERE id=$1`, gallery)
			case "named_missing":
				question = "那 UNIT nonexistent museum 呢？"
			case "named_ambiguous":
				b.exec(`UPDATE places SET name='UNIT Aberdeen Art Gallery Museum' WHERE id=$1`, gallery)
				question = "那 Museum 呢？"
			case "named_hidden":
				question = "那 Maritime Museum 呢？"
				b.exec(`UPDATE places SET publication_status='hidden' WHERE id=$1`, museum)
			case "named_foreign_city":
				foreignCity, foreignPlace := "unit-followup-foreign-"+liveNativeID(t, f), liveNativeID(t, f)
				b.exec(`INSERT INTO cities(id,name,region,country_code,time_zone,publication_status,source_label,source_ref,maintainer_label,maintainer_account_id) VALUES($1,'UNIT foreign city','LOCAL','GB','Europe/London','published','UNIT_PUBLIC_SOURCE','https://example.com/unit-foreign-city','合成维护者',$2)`, foreignCity, b.other.ID)
				b.exec(`INSERT INTO places(id,city_id,name,category_code,publication_status,source_label,source_ref,maintainer_label,maintainer_account_id) VALUES($1,$2,'UNIT Foreign Only Place','sports','published','UNIT_PUBLIC_SOURCE','https://example.com/unit-foreign-place','合成维护者',$3)`, foreignPlace, foreignCity, b.other.ID)
				t.Cleanup(func() {
					ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
					defer cancel()
					if _, e := b.pool.Exec(ctx, `DELETE FROM places WHERE id=$1`, foreignPlace); e != nil {
						t.Error("owned foreign place cleanup")
					}
					for _, sql := range []string{`DELETE FROM contexts WHERE city_id=$1`, `DELETE FROM city_contexts WHERE city_id=$1`, `DELETE FROM cities WHERE id=$1`} {
						if _, e := b.pool.Exec(ctx, sql, foreignCity); e != nil {
							t.Error("owned foreign city cleanup")
						}
					}
				})
				question = "那 Foreign Only Place 呢？"
			}
			access := placeFollowupDBAccess(t, f, task)
			if mode == "wrong_session" {
				access.SessionDigest = [32]byte{1}
			} else if mode == "wrong_actor" {
				access.Actor.ID = b.other.ID
			} else if mode == "stale_task" {
				before := access.ExpectedTask
				task.Filters["searchTerm"] = "UNIT different current query"
				var writeErr error
				task, writeErr = b.store.UpdateTask(b.ctx, task)
				if writeErr != nil {
					t.Fatal("owned stale task fixture", writeErr)
				}
				access.ExpectedTask = before
			}
			storedBefore := liveSourceReplyReadTask(t, f, task.ID)
			if _, e := b.store.ContinueOwnPublicPlace(b.ctx, access, question); e == nil {
				t.Fatal("unavailable/ambiguous/foreign source accepted a place followup")
			}
			if !reflect.DeepEqual(storedBefore, liveSourceReplyReadTask(t, f, task.ID)) {
				t.Fatal("denied followup changed original filters/task/conversation")
			}
			assertLiveNativeViews(t, f, modelegressbudget.Limits{})
		})
	}
}

func TestPlaceFollowupDBRetainedPlaceGenerationBeforeWireAndFinalReply(t *testing.T) {
	for _, mode := range []string{"hidden_before_send", "hidden_restore", "name_change", "generation_update", "expiry", "name_filter_mismatch", "body_read_withdrawal", "final_reply_withdrawal", "final_revalidate_withdrawal"} {
		t.Run(mode, func(t *testing.T) {
			f, gallery, _ := placeFollowupDBFixture(t)
			b := f.configuration.native.private.base
			placeFollowupDBContinue(t, f, "它周末几点开门？")
			h := resolvedLiveNativeRun(t, f)
			if mode == "final_reply_withdrawal" || mode == "final_revalidate_withdrawal" {
				_, preview := liveSourceRunBindFixture(t, f, h)
				resolvedNativeCompleteModel(t, f, h, preview)
				if mode == "final_reply_withdrawal" {
					b.exec(`UPDATE places SET publication_status='hidden' WHERE id=$1`, gallery)
					if _, e := h.FinalizeSourceReply(b.ctx); e == nil {
						t.Fatal("withdrawn public place released a final sourced reply")
					}
				} else {
					reply, e := h.FinalizeSourceReply(b.ctx)
					if e != nil || reply.Revalidate(b.ctx) != nil {
						t.Fatal("original current final reply fixture", e)
					}
					b.exec(`UPDATE places SET publication_status='hidden' WHERE id=$1`, gallery)
					if e = reply.Revalidate(b.ctx); e == nil {
						t.Fatal("withdrawn public place kept final reply bytes authorized")
					}
				}
				return
			}
			if mode == "body_read_withdrawal" {
				calls, emitted := 0, 0
				config, _ := agenttool.ParseTencentWSAConfig([]byte(`{"apiKey":"unit-test-placeholder","keyName":"unit-place-body"}`))
				adapter, e := agenttool.NewTencentWSAAdapter(config, liveNativeTransport(func(req *http.Request) (*http.Response, error) {
					calls++
					b.exec(`UPDATE places SET publication_status='hidden' WHERE id=$1`, gallery)
					raw, bodyErr := io.ReadAll(req.Body)
					emitted += len(raw)
					if bodyErr == nil {
						t.Fatal("native query body read bypassed current public place guard")
					}
					return nil, bodyErr
				}))
				if e != nil {
					t.Fatal(e)
				}
				if _, e = h.ExecuteSourceSearch(b.ctx, adapter); e == nil || calls != 1 || emitted != 0 {
					t.Fatal("withdrawal in actual body read released query bytes or retried", calls, emitted)
				}
				assertLiveNativePhase(t, f, h.search.OperationID, "UNKNOWN", "LIVE_ATTEMPTED")
				assertLiveNativeViews(t, f, modelegressbudget.Limits{Requests: 1, CostMicros: 80000})
				return
			}
			switch mode {
			case "hidden_before_send":
				b.exec(`UPDATE places SET publication_status='hidden' WHERE id=$1`, gallery)
			case "hidden_restore":
				b.exec(`UPDATE places SET publication_status='hidden' WHERE id=$1`, gallery)
				b.exec(`UPDATE places SET publication_status='published' WHERE id=$1`, gallery)
			case "name_change":
				b.exec(`UPDATE places SET name='UNIT changed Gallery' WHERE id=$1`, gallery)
			case "generation_update":
				b.exec(`UPDATE places SET name=name WHERE id=$1`, gallery)
			case "expiry":
				b.exec(`UPDATE places SET expires_at=clock_timestamp()-interval '1 second' WHERE id=$1`, gallery)
			case "name_filter_mismatch":
				task := f.configuration.native.task
				task.Filters["searchTerm"] = "UNIT unrelated public name"
				if _, e := b.store.UpdateTask(b.ctx, task); e != nil {
					t.Fatal("owned changed filter fixture", e)
				}
			}
			calls := 0
			if _, e := h.ExecuteSourceSearch(b.ctx, liveSourceRunWSA(t, f, h, &calls, nil)); e == nil || calls != 0 {
				t.Fatal("retired native public entity proof reached provider", calls)
			}
			assertLiveNativeViews(t, f, modelegressbudget.Limits{})
		})
	}
}

func TestPlaceFollowupDBNoImplicitResumeOfPartialTask(t *testing.T) {
	f, _, _ := placeFollowupDBFixture(t)
	b := f.configuration.native.private.base
	task := f.configuration.native.task
	task.Status = agentworkspace.TaskActive
	var e error
	task, e = b.store.UpdateTask(b.ctx, task)
	if e != nil {
		t.Fatal(e)
	}
	before := liveSourceReplyReadTask(t, f, task.ID)
	_, e = b.store.ContinueOwnPublicPlace(b.ctx, placeFollowupDBAccess(t, f, task), "它周末几点开门？")
	if e == nil || (!errors.Is(e, arp.ErrDenied) && !errors.Is(e, agentworkspace.ErrPlaceFollowupChanged)) || !reflect.DeepEqual(before, liveSourceReplyReadTask(t, f, task.ID)) {
		t.Fatal("partial/unknown prior task was implicitly replayed")
	}
	assertLiveNativeViews(t, f, modelegressbudget.Limits{})
}
