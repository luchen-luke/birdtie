package postgres

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/agentevent"
	"github.com/birdtie/birdtie/apps/api/internal/agentworkspace"
	"github.com/birdtie/birdtie/apps/api/internal/content"
)

type agentEventFixture struct {
	private *agentPrivateFixture
	moment  content.Moment
	task    agentworkspace.Task
	access  agentevent.Access
}

func eventIntegrationFixture(t *testing.T) *agentEventFixture {
	t.Helper()
	f := &agentEventFixture{private: agentPrivateTestFixture(t)}
	b := f.private.base
	f.access = agentevent.Access{SessionDigest: f.private.owner.SessionDigest}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		for _, statement := range []string{
			`DELETE FROM audit_events WHERE actor_account_id=ANY($1::uuid[])`,
			`DELETE FROM moments WHERE author_account_id=ANY($1::uuid[])`,
			`DELETE FROM agent_tasks WHERE owner_account_id=ANY($1::uuid[])`,
		} {
			if _, err := b.pool.Exec(ctx, statement, b.accounts); err != nil {
				t.Errorf("owned event source cleanup failed: %v", err)
			}
		}
		var remaining int
		if err := b.pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM moments WHERE author_account_id=ANY($1::uuid[]))+
			(SELECT count(*) FROM agent_tasks WHERE owner_account_id=ANY($1::uuid[]))`, b.accounts).Scan(&remaining); err != nil || remaining != 0 {
			t.Errorf("owned event fixture residue count=%d error=%v", remaining, err)
		}
	})
	historical := time.Date(2017, 5, 2, 0, 0, 0, 0, time.UTC)
	var err error
	f.moment, err = b.store.CreateMomentDraft(b.ctx, b.person.ID, content.MomentInput{CityID: "aberdeen-gb", Title: "合成事件私密标题014", Body: "合成事件正文绝不能默认进入event014", OccurredAt: &historical, TimePrecision: "day", LocationPrecision: "city"})
	if err != nil {
		t.Fatal("native Moment creation failed", err)
	}
	// Retain the private history canary while making the final native user
	// turn match the actual scalar query. Model-egress tests must exercise a
	// consistent current source without relaxing production query validation.
	query := "合成查询014：帮我找周末羽毛球"
	f.task, err = b.store.SaveTask(b.ctx, agentworkspace.Task{PrincipalType: "person", PrincipalID: b.person.ID, ActingUserID: b.person.ID, CityID: "aberdeen-gb", Query: query, Intent: "PENDING", Status: agentworkspace.TaskActive, Filters: map[string]string{}, Conversation: []agentworkspace.Message{
		{Role: "user", Text: "合成私人会话014不应进入event"},
		{Role: "assistant", Text: "合成历史回答014不应进入event"},
		{Role: "user", Text: query},
	}})
	if err != nil {
		t.Fatal("native UserQuery source creation failed", err)
	}
	return f
}

func eventRequest(kind agentevent.Type, id string) agentevent.Request {
	return agentevent.Request{EventType: kind, SourceID: id, LogicalOperationID: "72000000-0000-4000-8000-000000000051", RootTraceID: "72000000-0000-4000-8000-000000000052"}
}

func requireEventDenied(t *testing.T, f *agentEventFixture, a agentevent.Access, r agentevent.Request, expected error) {
	t.Helper()
	b := f.private.base
	e, err := b.store.ProduceAgentEvent(b.ctx, a, r)
	if !errors.Is(err, expected) || e != (agentevent.Envelope{}) {
		t.Fatalf("event did not reject without payload: %v expected %v", err, expected)
	}
}

func TestAgentEventNativeProducersIntegration(t *testing.T) {
	f := eventIntegrationFixture(t)
	b := f.private.base
	for _, item := range []struct {
		kind agentevent.Type
		id   string
	}{
		{agentevent.MomentCreated, f.moment.ID}, {agentevent.UserQuery, f.task.ID},
	} {
		t.Run(string(item.kind), func(t *testing.T) {
			r := eventRequest(item.kind, item.id)
			first, err := b.store.ProduceAgentEvent(b.ctx, f.access, r)
			if err != nil {
				t.Fatal(err)
			}
			if first.AgentID != b.personID || first.Subject != b.person || first.Tenant != b.person || first.Actor.ID != b.person.ID || first.Source.Owner != b.person || first.ProcessingStatus != agentevent.Unavailable {
				t.Fatal("identity not derived from native session/Agent")
			}
			data, err := agentevent.Encode(first, first.ReceivedAt)
			if err != nil {
				t.Fatal(err)
			}
			for _, canary := range []string{f.moment.Title, f.moment.Body, f.task.Query, f.task.Conversation[0].Text, "token_sha256", "consent_epoch", "latitude"} {
				if bytes.Contains(data, []byte(canary)) {
					t.Fatal("native private contents/authority emitted into event")
				}
			}
			if item.kind == agentevent.MomentCreated {
				if first.Source.Version.Kind != agentevent.RevisionVersion || first.Source.Version.Revision != f.moment.Revision || !first.OccurredAt.Equal(f.moment.CreatedAt) || first.OccurredAt.Equal(*f.moment.OccurredAt) {
					t.Fatal("historical experience or invented source revision used")
				}
			} else if first.Source.Version.Kind != agentevent.UpdatedAtDigestVersion || first.Source.Version.Revision != 0 || !first.OccurredAt.Equal(f.task.UpdatedAt) {
				t.Fatal("task was given fake numeric revision/input time")
			}
			for i := 0; i < 100; i++ {
				repeat, err := b.store.ProduceAgentEvent(b.ctx, f.access, r)
				if err != nil || repeat.EventID != first.EventID || !repeat.ExpiresAt.Equal(first.ExpiresAt) {
					t.Fatal("retry identity/lifetime changed", err)
				}
			}
			if err := b.store.RevalidateAgentEvent(b.ctx, f.access, first); err != nil {
				t.Fatal("current replay denied", err)
			}
			r.LogicalOperationID = "72000000-0000-4000-8000-000000000053"
			second, err := b.store.ProduceAgentEvent(b.ctx, f.access, r)
			if err != nil || second.EventID == first.EventID {
				t.Fatal("new operation collapsed into retry", err)
			}
			if err := b.store.RevalidateAgentEvent(b.ctx, agentevent.Access{SessionDigest: f.private.peer.SessionDigest}, first); !errors.Is(err, agentevent.ErrDenied) {
				t.Fatal("cross-owner replay accepted", err)
			}
		})
	}
	var count int
	if err := b.pool.QueryRow(b.ctx, `SELECT (SELECT count(*) FROM moments WHERE author_account_id=$1)+(SELECT count(*) FROM agent_tasks WHERE owner_account_id=$1)`, b.person.ID).Scan(&count); err != nil || count != 2 {
		t.Fatal("producing duplicates wrote native sources", err)
	}
}

func TestAgentEventSessionIdentityAndSourceBoundariesIntegration(t *testing.T) {
	cases := []struct {
		name   string
		change func(*agentEventFixture)
		access func(*agentEventFixture) agentevent.Access
	}{
		{"anonymous", func(*agentEventFixture) {}, func(*agentEventFixture) agentevent.Access { return agentevent.Access{} }},
		{"invalid_session", func(*agentEventFixture) {}, func(*agentEventFixture) agentevent.Access { return agentevent.Access{SessionDigest: [32]byte{9, 9, 9}} }},
		{"other_person", func(*agentEventFixture) {}, func(f *agentEventFixture) agentevent.Access {
			return agentevent.Access{SessionDigest: f.private.peer.SessionDigest}
		}},
		{"organization", func(*agentEventFixture) {}, func(f *agentEventFixture) agentevent.Access {
			return agentevent.Access{SessionDigest: f.private.org.SessionDigest}
		}},
		{"business", func(*agentEventFixture) {}, func(f *agentEventFixture) agentevent.Access {
			return agentevent.Access{SessionDigest: f.private.biz.SessionDigest}
		}},
		{"revoked_session", func(f *agentEventFixture) {
			f.private.base.exec(`UPDATE sessions SET revoked_at=now() WHERE id=$1`, f.private.ownerSession)
		}, func(f *agentEventFixture) agentevent.Access { return f.access }},
		{"expired_session", func(f *agentEventFixture) {
			f.private.base.exec(`UPDATE sessions SET created_at=now()-interval '2 hours',expires_at=now()-interval '1 hour',idle_expires_at=now()-interval '1 hour' WHERE id=$1`, f.private.ownerSession)
		}, func(f *agentEventFixture) agentevent.Access { return f.access }},
		{"expired_idle", func(f *agentEventFixture) {
			f.private.base.exec(`UPDATE sessions SET created_at=now()-interval '2 hours',idle_expires_at=now()-interval '1 hour' WHERE id=$1`, f.private.ownerSession)
		}, func(f *agentEventFixture) agentevent.Access { return f.access }},
		{"dev_phone_disabled", func(f *agentEventFixture) {
			f.private.base.exec(`UPDATE sessions SET authentication_method='dev_phone' WHERE id=$1`, f.private.ownerSession)
		}, func(f *agentEventFixture) agentevent.Access { return f.access }},
		{"account_suspended", func(f *agentEventFixture) {
			f.private.base.exec(`UPDATE accounts SET status='suspended' WHERE id=$1`, f.private.base.person.ID)
		}, func(f *agentEventFixture) agentevent.Access { return f.access }},
		{"account_deleted", func(f *agentEventFixture) {
			f.private.base.exec(`UPDATE accounts SET status='deleted' WHERE id=$1`, f.private.base.person.ID)
		}, func(f *agentEventFixture) agentevent.Access { return f.access }},
		{"agent_suspended", func(f *agentEventFixture) {
			f.private.base.exec(`UPDATE agents SET status='suspended' WHERE id=$1`, f.private.base.personID)
		}, func(f *agentEventFixture) agentevent.Access { return f.access }},
		{"agent_retired", func(f *agentEventFixture) {
			f.private.base.exec(`UPDATE agents SET status='retired' WHERE id=$1`, f.private.base.personID)
		}, func(f *agentEventFixture) agentevent.Access { return f.access }},
		{"agent_deleted", func(f *agentEventFixture) {
			f.private.base.exec(`DELETE FROM agents WHERE id=$1`, f.private.base.personID)
		}, func(f *agentEventFixture) agentevent.Access { return f.access }},
		{"metadata_deleted", func(f *agentEventFixture) {
			f.private.base.exec(`DELETE FROM agent_profiles WHERE agent_id=$1`, f.private.base.personID)
		}, func(f *agentEventFixture) agentevent.Access { return f.access }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := eventIntegrationFixture(t)
			tc.change(f)
			for _, item := range []struct {
				kind agentevent.Type
				id   string
			}{{agentevent.MomentCreated, f.moment.ID}, {agentevent.UserQuery, f.task.ID}} {
				t.Run(string(item.kind), func(t *testing.T) {
					requireEventDenied(t, f, tc.access(f), eventRequest(item.kind, item.id), agentevent.ErrDenied)
				})
			}
		})
	}
}

func TestAgentEventNativeSourceInvalidationIntegration(t *testing.T) {
	f := eventIntegrationFixture(t)
	b := f.private.base
	mr := eventRequest(agentevent.MomentCreated, f.moment.ID)
	qr := eventRequest(agentevent.UserQuery, f.task.ID)
	m, err := b.store.ProduceAgentEvent(b.ctx, f.access, mr)
	if err != nil {
		t.Fatal(err)
	}
	q, err := b.store.ProduceAgentEvent(b.ctx, f.access, qr)
	if err != nil {
		t.Fatal(err)
	}
	t.Run("moment_edit", func(t *testing.T) {
		f.moment, err = b.store.UpdateMomentDraft(b.ctx, b.person.ID, f.moment.ID, f.moment.Revision, content.MomentInput{CityID: "aberdeen-gb", Title: "本人明确修改版本014", Body: "修改后私密正文", TimePrecision: "unknown", LocationPrecision: "city"})
		if err != nil {
			t.Fatal(err)
		}
		if err := b.store.RevalidateAgentEvent(b.ctx, f.access, m); !errors.Is(err, agentevent.ErrDenied) {
			t.Fatal("old Moment revision revalidated", err)
		}
		current, err := b.store.ProduceAgentEvent(b.ctx, f.access, mr)
		if err != nil || current.Source.Version.Revision != f.moment.Revision || current.EventID == m.EventID {
			t.Fatal("current Moment revision not native", err)
		}
	})
	t.Run("query_edit", func(t *testing.T) {
		f.task.Conversation = append(f.task.Conversation, agentworkspace.Message{Role: "user", Text: "近一点的呢？014"})
		f.task.Status = agentworkspace.TaskCompleted
		f.task, err = b.store.UpdateTask(b.ctx, f.task)
		if err != nil {
			t.Fatal(err)
		}
		if err := b.store.RevalidateAgentEvent(b.ctx, f.access, q); !errors.Is(err, agentevent.ErrDenied) {
			t.Fatal("old query native token revalidated", err)
		}
		current, err := b.store.ProduceAgentEvent(b.ctx, f.access, qr)
		if err != nil || current.Source.Version.Token == q.Source.Version.Token || !current.OccurredAt.Equal(f.task.UpdatedAt) {
			t.Fatal("query native update not bound", err)
		}
	})
	t.Run("content_change_same_time", func(t *testing.T) {
		current, err := b.store.ProduceAgentEvent(b.ctx, f.access, qr)
		if err != nil {
			t.Fatal(err)
		}
		b.exec(`UPDATE agent_tasks SET conversation=conversation||'[ {"role":"user","text":"fixture exact same timestamp changed source"}]'::jsonb WHERE id=$1`, f.task.ID)
		if err := b.store.RevalidateAgentEvent(b.ctx, f.access, current); !errors.Is(err, agentevent.ErrDenied) {
			t.Fatal("same timestamp changed content not invalidated", err)
		}
	})
	t.Run("withdraw_moment", func(t *testing.T) {
		if err := b.store.WithdrawMoment(b.ctx, b.person.ID, f.moment.ID, f.moment.Revision); err != nil {
			t.Fatal(err)
		}
		requireEventDenied(t, f, f.access, mr, agentevent.ErrDenied)
		if err := b.store.RevalidateAgentEvent(b.ctx, f.access, m); !errors.Is(err, agentevent.ErrDenied) {
			t.Fatal("withdrawn source replay accepted", err)
		}
	})
	t.Run("failed_query", func(t *testing.T) {
		f.task.Status = agentworkspace.TaskFailed
		if _, err := b.store.UpdateTask(b.ctx, f.task); err != nil {
			t.Fatal(err)
		}
		requireEventDenied(t, f, f.access, qr, agentevent.ErrDenied)
	})
	t.Run("unknown_source", func(t *testing.T) {
		requireEventDenied(t, f, f.access, eventRequest(agentevent.MomentCreated, "72000000-0000-4000-8000-000000000099"), agentevent.ErrDenied)
	})
	t.Run("wrong_source_kind", func(t *testing.T) {
		requireEventDenied(t, f, f.access, eventRequest(agentevent.UserQuery, f.moment.ID), agentevent.ErrDenied)
	})
}

func TestAgentEventExpiryReplayAndConcurrentDuplicatesIntegration(t *testing.T) {
	f := eventIntegrationFixture(t)
	b := f.private.base
	r := eventRequest(agentevent.UserQuery, f.task.ID)
	initial, err := b.store.ProduceAgentEvent(b.ctx, f.access, r)
	if err != nil {
		t.Fatal(err)
	}
	var wait sync.WaitGroup
	issues := make(chan error, 24)
	for i := 0; i < 24; i++ {
		wait.Add(1)
		go func() {
			defer wait.Done()
			e, err := b.store.ProduceAgentEvent(b.ctx, f.access, r)
			if err != nil {
				issues <- err
			} else if e.EventID != initial.EventID {
				issues <- errors.New("concurrent duplicate identity changed")
			}
		}()
	}
	wait.Wait()
	close(issues)
	for err := range issues {
		t.Error(err)
	}
	t.Run("old_query_no_lifetime_extension", func(t *testing.T) {
		b.exec(`UPDATE agent_tasks SET updated_at=now()-interval '16 minutes' WHERE id=$1`, f.task.ID)
		requireEventDenied(t, f, f.access, r, agentevent.ErrExpired)
	})
	t.Run("old_moment_created_not_current_experience", func(t *testing.T) {
		b.exec(`UPDATE moments SET created_at=now()-interval '16 minutes' WHERE id=$1`, f.moment.ID)
		requireEventDenied(t, f, f.access, eventRequest(agentevent.MomentCreated, f.moment.ID), agentevent.ErrExpired)
	})
	t.Run("revocation_precedes_current_statement", func(t *testing.T) {
		b.exec(`UPDATE sessions SET revoked_at=now() WHERE id=$1`, f.private.ownerSession)
		if err := b.store.RevalidateAgentEvent(b.ctx, f.access, initial); !errors.Is(err, agentevent.ErrDenied) {
			t.Fatal("revoked real session replay accepted", err)
		}
	})
	t.Run("wire_confirmed_does_not_grant", func(t *testing.T) {
		wire, _ := json.Marshal(initial)
		wire = bytes.Replace(wire, []byte(`"schema_version":`), []byte(`"confirmed":true,"consent_epoch":1,"schema_version":`), 1)
		if e, err := agentevent.Decode(wire, initial.ReceivedAt); !errors.Is(err, agentevent.ErrInvalid) || e != (agentevent.Envelope{}) {
			t.Fatal("wire confirmation became authority")
		}
	})
	t.Run("invalid_selector", func(t *testing.T) {
		for _, id := range []string{"https://example.invalid/secret", "00000000-0000-0000-0000-000000000000", " " + f.task.ID} {
			requireEventDenied(t, f, f.access, eventRequest(agentevent.UserQuery, id), agentevent.ErrInvalid)
		}
	})
	t.Run("nil_store", func(t *testing.T) {
		var store *Store
		e, err := store.ProduceAgentEvent(b.ctx, f.access, r)
		if !errors.Is(err, agentevent.ErrUnavailable) || e != (agentevent.Envelope{}) {
			t.Fatal("nil native producer unsafe")
		}
	})
}
