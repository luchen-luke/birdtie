package postgres

import (
	"context"
	"encoding/json"
	"errors"
	arp "github.com/birdtie/birdtie/apps/api/internal/agentresultprojection"
	"github.com/birdtie/birdtie/apps/api/internal/agenttool"
	"github.com/birdtie/birdtie/apps/api/internal/agentworkspace"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"reflect"
	"strings"
	"testing"
	"time"
)

const currentReadonlyPGOwner = "49100000-0000-4000-8000-000000000001"
const currentReadonlyPGTask = "49100000-0000-4000-8000-000000000002"
const currentReadonlyPGEntity = "49100000-0000-4000-8000-000000000003"

type currentReadonlyUnitRow struct {
	values []any
	err    error
}

func (r currentReadonlyUnitRow) Scan(dest ...any) error {
	if r.err != nil {
		return r.err
	}
	if len(dest) != len(r.values) {
		return errors.New("unit row arity mismatch")
	}
	for i, v := range r.values {
		d := reflect.ValueOf(dest[i]).Elem()
		if v == nil {
			d.SetZero()
			continue
		}
		s := reflect.ValueOf(v)
		if !s.Type().AssignableTo(d.Type()) {
			return errors.New("unit row type mismatch")
		}
		d.Set(s)
	}
	return nil
}

type currentReadonlySearchTx struct {
	pgx.Tx
	task           agentworkspace.Task
	calls          []string
	payloadSQL     string
	payloadArgs    []any
	sessionError   error
	current        bool
	items, actions []byte
}

func (t *currentReadonlySearchTx) Exec(_ context.Context, sql string, _ ...any) (pgconn.CommandTag, error) {
	t.calls = append(t.calls, "relations")
	return pgconn.NewCommandTag("SELECT 1"), nil
}
func (t *currentReadonlySearchTx) QueryRow(_ context.Context, sql string, args ...any) pgx.Row {
	switch {
	case strings.HasPrefix(sql, "SELECT id FROM accounts"):
		t.calls = append(t.calls, "account")
		return currentReadonlyUnitRow{values: []any{t.task.PrincipalID}}
	case strings.HasPrefix(sql, "SELECT "+agentTaskColumns):
		t.calls = append(t.calls, "task")
		f, _ := json.Marshal(t.task.Filters)
		c, _ := json.Marshal(t.task.Conversation)
		actor := t.task.ActingUserID
		return currentReadonlyUnitRow{values: []any{t.task.ID, t.task.PrincipalType, t.task.PrincipalID, &actor, t.task.Query, t.task.Intent, t.task.Status, t.task.CityID, t.task.ContextType, t.task.ContextID, f, c, t.task.CreatedAt, t.task.UpdatedAt}}
	case strings.HasPrefix(sql, "SELECT id FROM sessions"):
		t.calls = append(t.calls, "session")
		return currentReadonlyUnitRow{values: []any{currentReadonlyPGEntity}, err: t.sessionError}
	default:
		t.calls = append(t.calls, "payload")
		t.payloadSQL = sql
		t.payloadArgs = args
		now := time.Now().UTC()
		items, actions := t.items, t.actions
		if items == nil {
			items = []byte(`[]`)
		}
		if actions == nil {
			actions = []byte(`{}`)
		}
		return currentReadonlyUnitRow{values: []any{now, now.Add(20 * time.Second), items, []byte(`[]`), []byte(`null`), strings.Repeat("a", 64), true, []byte(`[]`), []byte(`[]`), t.current, actions}}
	}
}

func TestCurrentReadonlyProjectionActionsNeverOutliveRestrictedRead(t *testing.T) {
	task, a, q := currentReadonlyPGTaskInput()
	item := arp.Item{Entity: arp.Ref{Type: "person", ID: currentReadonlyPGEntity}, Title: "当前对象", Summary: "明确公开来源", Scope: arp.AuthorizedView}
	raw, _ := json.Marshal([]arp.Item{item})
	tx := &currentReadonlySearchTx{task: task, current: true, items: raw, actions: []byte(`{"person:` + currentReadonlyPGEntity + `":{}}`)}
	p := &nativeToolPolicy{owner: a.Actor.ID, agent: currentReadonlyPGEntity, token: strings.Repeat("b", 64), until: time.Now().Add(5 * time.Second)}
	r, e := (&Store{}).captureAgentResultProjectionCurrentTx(context.Background(), tx, a, q, nil, p)
	if e != nil || len(r.Items) != 1 || r.Items[0].ActionsValidUntil == nil {
		t.Fatal("actual item projection", e)
	}
	if r.Items[0].ActionsValidUntil.After(r.ValidUntil) {
		t.Fatal("action descriptor lifetime exceeded current restricted read")
	}
}

type currentReadonlyPolicyTx struct {
	pgx.Tx
	calls     []string
	at        time.Time
	execError error
}

func (s *currentReadonlyPolicyTx) Exec(_ context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	if strings.Contains(sql, "human-agent-policy:") {
		s.calls = append(s.calls, "original_writer_advisory")
		if len(args) != 1 || args[0] != currentReadonlyPGEntity {
			return pgconn.CommandTag{}, errors.New("owner serializer mismatch")
		}
		return pgconn.NewCommandTag("SELECT 1"), s.execError
	}
	if sql == `LOCK TABLE agent_policy_settings IN SHARE MODE` {
		s.calls = append(s.calls, "settings_table_share")
		return pgconn.NewCommandTag("LOCK TABLE"), nil
	}
	return pgconn.CommandTag{}, errors.New("unexpected policy SQL")
}
func (s *currentReadonlyPolicyTx) Query(_ context.Context, sql string, args ...any) (pgx.Rows, error) {
	s.calls = append(s.calls, "original_policy_rows")
	return &currentReadonlyMatchRows{}, nil
}
func (s *currentReadonlyPolicyTx) QueryRow(_ context.Context, sql string, args ...any) pgx.Row {
	if sql == `SELECT clock_timestamp()` {
		s.calls = append(s.calls, "native_clock")
		return currentReadonlyUnitRow{values: []any{s.at}}
	}
	if sql == nativeToolPolicyTokenSQL {
		s.calls = append(s.calls, "original_xmin_fingerprint")
		return currentReadonlyUnitRow{values: []any{strings.Repeat("a", 64)}}
	}
	return currentReadonlyUnitRow{err: errors.New("unexpected policy row")}
}

func TestCurrentReadonlyPolicyReusesWriterSerializerBeforeSettingsRowFreeze(t *testing.T) {
	// PostgreSQL timestamptz has microsecond precision; retain the real bundle
	// validator rather than passing an impossible sub-microsecond row fixture.
	at := time.Now().UTC().Truncate(time.Microsecond)
	tx := &currentReadonlyPolicyTx{at: at}
	b := agentPrivateBinding{accountID: currentReadonlyPGOwner, agentID: currentReadonlyPGEntity}
	p, e := captureCurrentHumanToolPolicy(context.Background(), tx, b, at.Add(5*time.Second))
	if e != nil || p.owner != b.accountID || p.agent != b.agentID || strings.Join(tx.calls, ",") != "original_writer_advisory,settings_table_share,original_policy_rows,native_clock,original_xmin_fingerprint" {
		t.Fatal("actual helper lock-call order", e, tx.calls)
	}
	tx = &currentReadonlyPolicyTx{at: at, execError: errors.New("unit wait canceled")}
	if _, e = captureCurrentHumanToolPolicy(context.Background(), tx, b, at.Add(5*time.Second)); !errors.Is(e, agenttool.ErrUnavailable) || len(tx.calls) != 1 {
		t.Fatal("failed original serializer retained source snapshot", e, tx.calls)
	}
}
func currentReadonlyPGTaskInput() (agentworkspace.Task, arp.Access, arp.Query) {
	task := agentworkspace.Task{ID: currentReadonlyPGTask, PrincipalType: "person", PrincipalID: currentReadonlyPGOwner, ActingUserID: currentReadonlyPGOwner, Intent: agentworkspace.FindPerson, Status: agentworkspace.TaskCompleted, CityID: "city_fixture", Query: "找公开成员", Filters: map[string]string{}, Conversation: []agentworkspace.Message{}}
	raw, _ := json.Marshal(agentworkspace.SanitizeTaskForResponse(task))
	a := arp.Access{Actor: identity.Actor{ID: task.PrincipalID, AccountType: "person"}, SessionDigest: [32]byte{1}, TaskID: task.ID, ExpectedTask: raw}
	return task, a, arp.Query{CityID: task.CityID, Kind: "person", CompareIDs: []string{}}
}

// Actual native Tx mapper/call order using row spies ONLY. This does not
// execute SQL, PostgreSQL locks, source ACL predicates or migrations.
func TestCurrentReadonlyProjectionUsesOriginalTaskAndFinalPolicyStatement(t *testing.T) {
	task, a, q := currentReadonlyPGTaskInput()
	tx := &currentReadonlySearchTx{task: task, current: true}
	p := &nativeToolPolicy{owner: a.Actor.ID, agent: currentReadonlyPGEntity, token: strings.Repeat("b", 64), until: time.Now().Add(30 * time.Second)}
	r, e := (&Store{}).captureAgentResultProjectionCurrentTx(context.Background(), tx, a, q, nil, p)
	if e != nil || r.Items == nil || len(r.Items) != 0 {
		t.Fatal("actual mapper", e)
	}
	if strings.Join(tx.calls, ",") != "relations,account,task,session,payload" || len(tx.payloadArgs) != 10 {
		t.Fatal("source/policy call order or arguments", tx.calls, len(tx.payloadArgs))
	}
	for _, required := range []string{" WHERE true AND p.observed_at<$7::timestamptz", "tp.owner_id=$9::uuid", "tp.agent_id=$10::uuid", "tp.xmin::text", "current_agent.id=$10::uuid", "current_agent.principal_account_id=$9::uuid", "clock_timestamp()", "i.owner_confirmed_at IS NOT NULL", "birdtie_agent_profile_field_allowed", "account_blocks", "p.publication_status='published'", "birdtie_activity_visible_to"} {
		if !strings.Contains(tx.payloadSQL, required) {
			t.Fatal("final original predicate missing", required)
		}
	}
	if tx.payloadArgs[7] != p.token || tx.payloadArgs[8] != p.owner || tx.payloadArgs[9] != p.agent || r.ValidUntil.After(p.until) {
		t.Fatal("policy fingerprint/owner/lifetime changed")
	}
	// No tool policy keeps the exact old original public/native ModelRun SQL.
	oldTx := &currentReadonlySearchTx{task: task, current: true}
	if _, e = (&Store{}).captureAgentResultProjectionTx(context.Background(), oldTx, a, q, nil); e != nil || oldTx.payloadSQL != resultProjectionSQL() || len(oldTx.payloadArgs) != 6 {
		t.Fatal("legacy source behavior changed", e)
	}
}
func TestCurrentReadonlyProjectionTaskSlotsSessionAndLateSourceStillReject(t *testing.T) {
	for name, change := range map[string]func(*currentReadonlySearchTx, *arp.Access, *arp.Query){"task_aba": func(tx *currentReadonlySearchTx, a *arp.Access, q *arp.Query) {
		tx.task.Status = agentworkspace.TaskFailed
	}, "foreign_expected_task": func(tx *currentReadonlySearchTx, a *arp.Access, q *arp.Query) { a.ExpectedTask = json.RawMessage(`{}`) }, "different_kind": func(tx *currentReadonlySearchTx, a *arp.Access, q *arp.Query) { q.Kind = "place" }, "different_search": func(tx *currentReadonlySearchTx, a *arp.Access, q *arp.Query) { q.SearchTerm = "hidden" }, "session_revoked": func(tx *currentReadonlySearchTx, a *arp.Access, q *arp.Query) { tx.sessionError = pgx.ErrNoRows }, "final_session_expired": func(tx *currentReadonlySearchTx, a *arp.Access, q *arp.Query) { tx.current = false }} {
		t.Run(name, func(t *testing.T) {
			task, a, q := currentReadonlyPGTaskInput()
			tx := &currentReadonlySearchTx{task: task, current: true}
			change(tx, &a, &q)
			if _, e := (&Store{}).captureAgentResultProjectionCurrentTx(context.Background(), tx, a, q, nil, nil); e == nil {
				t.Fatal("source mismatch released as empty")
			}
		})
	}
}
func TestCurrentReadonlySearchNativeCompoundSealRejectsTamperedSourceWithoutDB(t *testing.T) {
	task, a, q := currentReadonlyPGTaskInput()
	input := agenttool.CurrentSearch{Access: a, Query: q}
	at := time.Now().UTC()
	s := arp.Receipt{Items: []arp.Item{}, PublicCommercialRefs: []arp.Ref{}, ObservedAt: at, ValidUntil: at.Add(20 * time.Second), Proof: strings.Repeat("a", 64)}
	s.Seal, _ = resultProjectionSeal(a, q, s)
	p := &nativeToolPolicy{owner: a.Actor.ID, agent: currentReadonlyPGEntity, token: strings.Repeat("b", 64), observed: at, until: s.ValidUntil}
	d := nativeToolDecision(agenttool.PersonSearch, task.ID, task.ID, s.Proof, agenttool.CurrentSearchDigest(input), p.owner, p.agent, p, agenttool.Allow, "CURRENT_NATIVE_HUMAN_READ_NO_MACHINE_AUTHORITY")
	r := agenttool.CurrentSearchReceipt{Decision: d, Source: s}
	r.Seal, _ = currentReadSeal("birdtie.human-task-read.v1", agenttool.CurrentSearchDigest(input), d, s.Seal)
	for name, change := range map[string]func(*agenttool.CurrentSearchReceipt){"fake_policy": func(r *agenttool.CurrentSearchReceipt) { r.Decision.PolicyVersion = strings.Repeat("c", 64) }, "source_body": func(r *agenttool.CurrentSearchReceipt) { r.Source.Proof = strings.Repeat("c", 64) }, "late_other_owner": func(r *agenttool.CurrentSearchReceipt) { r.Decision.ActorID = currentReadonlyPGEntity }, "fake_compound": func(r *agenttool.CurrentSearchReceipt) { r.Seal = strings.Repeat("0", 64) }} {
		t.Run(name, func(t *testing.T) {
			copy := r
			change(&copy)
			if e := (&Store{}).RevalidateOwnCurrentSearch(context.Background(), input, copy); !errors.Is(e, agenttool.ErrDenied) {
				t.Fatal("tamper reached unavailable DB or released", e)
			}
		})
	}
}
