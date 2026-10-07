package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/birdtie/birdtie/apps/api/internal/agenttool"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	"github.com/birdtie/birdtie/apps/api/internal/newpeople"
	"github.com/jackc/pgx/v5"
	"strings"
	"testing"
	"time"
)

type currentReadonlyMatchRows struct {
	pgx.Rows
	values []any
	read   bool
	err    error
}

func (r *currentReadonlyMatchRows) Next() bool {
	if r.read {
		return false
	}
	r.read = true
	return r.values != nil
}
func (r *currentReadonlyMatchRows) Scan(d ...any) error {
	return (currentReadonlyUnitRow{values: r.values}).Scan(d...)
}
func (r *currentReadonlyMatchRows) Err() error { return r.err }
func (r *currentReadonlyMatchRows) Close()     {}

type currentReadonlyMatchTx struct {
	*currentReadonlySearchTx
	sql      string
	args     []any
	source   string
	empty    bool
	observed time.Time
}

func (t *currentReadonlyMatchTx) QueryRow(_ context.Context, sql string, args ...any) pgx.Row {
	if strings.HasPrefix(sql, "SELECT i.id,i.creator_account_id") {
		return currentReadonlyUnitRow{values: []any{t.source, currentReadonlyPGOwner, "合成当前声明", "ONLINE", []byte(`{"category":"badminton"}`), "", ""}}
	}
	return t.currentReadonlySearchTx.QueryRow(context.Background(), sql, args...)
}
func (t *currentReadonlyMatchTx) Query(_ context.Context, sql string, args ...any) (pgx.Rows, error) {
	t.sql, t.args = sql, args
	t.calls = append(t.calls, "match_payload")
	if t.empty {
		return &currentReadonlyMatchRows{}, nil
	}
	until := t.observed.Add(time.Hour)
	v := []any{t.source, currentReadonlyPGOwner, "合成当前声明", "ONLINE", []byte(`{"category":"badminton"}`), "", "", strings.Repeat("a", 64), until, nil, nil, nil, nil, []byte(nil), nil, nil, nil, nil}
	if len(args) == 9 {
		v = append(v, t.observed)
	}
	return &currentReadonlyMatchRows{values: v}, nil
}

func TestCurrentReadonlyMatchTxPreservesOptInACLAndFinalPolicyFence(t *testing.T) {
	for _, policy := range []bool{false, true} {
		t.Run(map[bool]string{false: "old_contract", true: "current_tool"}[policy], func(t *testing.T) {
			at := time.Now().UTC()
			tx := &currentReadonlyMatchTx{currentReadonlySearchTx: &currentReadonlySearchTx{}, source: currentReadonlyPGTask, observed: at}
			b := humanNewPeopleBinding{Digest: [32]byte{1}}
			var observed time.Time
			if policy {
				b.Policy = &nativeToolPolicy{owner: currentReadonlyPGOwner, agent: currentReadonlyPGEntity, token: strings.Repeat("b", 64), until: at.Add(30 * time.Second)}
				b.Observed = &observed
			}
			r, _, _, e := (&Store{}).findNewPeopleTx(context.Background(), tx, currentReadonlyPGOwner, currentReadonlyPGTask, b)
			if e != nil || r.Candidates == nil || len(r.Candidates) != 0 || r.SourceIntentID != tx.source {
				t.Fatal("original actual matcher empty contract", e)
			}
			for _, guard := range []string{"person_new_people_consent opt", "opt.enabled", "birdtie_social_intent_visible_to(i.id,$1)", "birdtie_social_intent_visible_to($2,i.creator_account_id)", "account_blocks", "person_ties", "connection_requests", "current_owner", "stamp.at"} {
				if !strings.Contains(tx.sql, guard) {
					t.Fatal("native original predicate missing", guard)
				}
			}
			if policy {
				if len(tx.args) != 9 || !observed.Equal(at) || tx.args[6] != b.Policy.token || !strings.Contains(tx.sql, "tp.xmin::text") || !strings.Contains(tx.sql, "current_agent.id=$9::uuid") || !strings.Contains(tx.sql, "stamp.at<$6::timestamptz") {
					t.Fatal("same source/policy native fence missing")
				}
			} else if len(tx.args) != 5 || strings.Contains(tx.sql, "agent_policy_settings") {
				t.Fatal("original human matcher changed")
			}
		})
	}
}
func TestCurrentReadonlyMatchMissingCurrentSourceIsNotEmptySuccess(t *testing.T) {
	tx := &currentReadonlyMatchTx{currentReadonlySearchTx: &currentReadonlySearchTx{}, source: currentReadonlyPGTask, empty: true}
	if _, _, _, e := (&Store{}).findNewPeopleTx(context.Background(), tx, currentReadonlyPGOwner, currentReadonlyPGTask, humanNewPeopleBinding{Digest: [32]byte{1}}); !errors.Is(e, newpeople.ErrNotFound) {
		t.Fatal("withdrawn source was successful empty", e)
	}
}
func TestCurrentReadonlyMatchNativeSealCannotAuthorizeForeignSource(t *testing.T) {
	q := agenttool.CurrentMatch{Actor: identity.Actor{ID: currentReadonlyPGOwner, AccountType: "person"}, SessionDigest: [32]byte{1}, SourceIntentID: currentReadonlyPGTask}
	at := time.Now().UTC()
	source := newpeople.HumanReceipt{Response: newpeople.Response{Source: "RULE_BASED", RuleVersion: newpeople.RuleVersion, SourceIntentID: q.SourceIntentID, Candidates: []newpeople.Candidate{}}, Proof: strings.Repeat("a", 64), ExpiresAt: at.Add(20 * time.Second)}
	source.Seal, _ = humanNewPeopleSeal(q.Actor, q.SessionDigest, source)
	p := &nativeToolPolicy{owner: q.Actor.ID, agent: currentReadonlyPGEntity, token: strings.Repeat("b", 64), observed: at, until: source.ExpiresAt}
	d := nativeToolDecision(agenttool.PersonMatch, q.SourceIntentID, q.SourceIntentID, source.Proof, agenttool.CurrentMatchDigest(q), p.owner, p.agent, p, agenttool.Allow, "CURRENT_NATIVE_HUMAN_READ_NO_MACHINE_AUTHORITY")
	r := agenttool.CurrentMatchReceipt{Decision: d, Source: source, ObservedAt: at}
	r.Seal, _ = currentReadSeal("birdtie.human-match-read.v1", agenttool.CurrentMatchDigest(q), d, source.Seal)
	if !r.Valid(q) {
		t.Fatal("native sealed original response fixture invalid")
	}
	for name, m := range map[string]func(*agenttool.CurrentMatchReceipt){"same_version_other_policy": func(r *agenttool.CurrentMatchReceipt) { r.Decision.PolicyVersion = strings.Repeat("c", 64) }, "source": func(r *agenttool.CurrentMatchReceipt) { r.Source.Response.SourceIntentID = currentReadonlyPGEntity }, "fake_digest": func(r *agenttool.CurrentMatchReceipt) { r.Decision.ArgumentsDigest = strings.Repeat("c", 64) }, "remote_destination": func(r *agenttool.CurrentMatchReceipt) { r.Decision.DataDestinations = []string{"MODEL"} }} {
		t.Run(name, func(t *testing.T) {
			copy := r
			m(&copy)
			if e := (&Store{}).RevalidateOwnCurrentMatch(context.Background(), q, copy); !errors.Is(e, agenttool.ErrDenied) {
				raw, _ := json.Marshal(copy.Decision)
				t.Fatal("native tamper got past guard", e, string(raw))
			}
		})
	}
}
