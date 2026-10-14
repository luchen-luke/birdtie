package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
	"time"

	arp "github.com/birdtie/birdtie/apps/api/internal/agentresultprojection"
	"github.com/birdtie/birdtie/apps/api/internal/agentworkspace"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
)

// Presentation doubles only. The native reader decides which saved rounds
// remain authorized; these tests neither validate SQL facts nor call a model.
type licensedNoFallbackHTTPPort struct {
	*liveReplyHistoryHTTPPort
	searches int
}

var _ arp.NativeStore = (*licensedNoFallbackHTTPPort)(nil)

func (p *licensedNoFallbackHTTPPort) ReadAgentResultProjection(context.Context, arp.Access, arp.Query) (arp.Receipt, error) {
	p.searches++
	return arp.Receipt{}, errors.New("unit retired licensed history must not execute a fresh search")
}

func (*licensedNoFallbackHTTPPort) RevalidateAgentResultProjection(context.Context, arp.Access, arp.Query, arp.Receipt) error {
	return errors.New("unit retired history has no fresh search receipt")
}

func licensedNoFallbackFixture(t *testing.T) (*server, *licensedNoFallbackHTTPPort, agentworkspace.Results) {
	t.Helper()
	s, base, result, _ := liveReplyHistoryHTTPFixture(t)
	message := &base.task.Conversation[3]
	message.SourceRunID = ""
	message.LicensedPlaceModelEvidence = &agentworkspace.LicensedPlaceModelEvidence{
		Schema: agentworkspace.LicensedPlaceModelReplySchema, Provider: "tencent_tokenhub",
		Model: "deepseek-v4-pro-0813", ModelCalls: 1, SearchCalls: 0,
		ProviderRequestID:    "UNIT_PROVIDER_REQUEST_NOT_A_REAL_CALL",
		SourceEvidenceDigest: message.SourceEvidenceDigest,
		ValidUntil:           time.Now().UTC().Add(-time.Hour), // Old dispatch expiry grants nothing.
	}
	result.Task = &base.task
	result.ResultSet.Sources = []agentworkspace.AnswerSource{{ID: "unrelated", Title: "UNIT unrelated source", URL: "https://example.com/new"}}
	port := &licensedNoFallbackHTTPPort{liveReplyHistoryHTTPPort: base}
	s.agent = port
	return s, port, result
}

func licensedNoFallbackPrepare(t *testing.T, s *server, result agentworkspace.Results) (agentworkspace.Results, agentworkspace.MessageResultsRead, bool, error) {
	t.Helper()
	r := httptest.NewRequest(http.MethodGet, "/unit-licensed-history", nil)
	got, read, primary, err := s.prepareAgentMessageResults(r, result, [32]byte{1}, identity.Actor{ID: httpActionSafetyOwner, AccountType: "person"})
	// Match the original responder's branch exactly. A false historyPrimary is
	// the real defect: it re-queries the broader city/category after retirement.
	if err == nil && !primary {
		got, _, _, _, _, err = s.prepareNativeAgentResults(r, got, [32]byte{1}, identity.Actor{ID: httpActionSafetyOwner, AccountType: "person"})
	}
	return got, read, primary, err
}

func TestLicensedReplyUnavailableLatestDoesNotFallbackToFreshCitySearch(t *testing.T) {
	for _, cause := range []string{"expired_fields", "withdrawn_permission", "missing_authorized_entry", "no_entries"} {
		t.Run(cause, func(t *testing.T) {
			s, port, initial := licensedNoFallbackFixture(t)
			port.entries = port.entries[:1] // Latest original round failed native current validation.
			if cause == "no_entries" {
				port.entries = nil
			}
			before, _ := json.Marshal(port.task)
			got, read, primary, err := licensedNoFallbackPrepare(t, s, initial)
			if err != nil || !primary || read == nil || port.reads != 1 || port.searches != 0 {
				t.Fatal("retired licensed latest reply triggered a new search or lost its native final read", err, primary, port.reads, port.searches)
			}
			if !got.NativeProjection || len(got.ProjectionItems) != 0 || len(got.ResultSet.Items) != 0 || len(got.ResultSet.Entities) != 0 || len(got.Places) != 0 || len(got.Activities) != 0 || len(got.Organizations) != 0 || len(got.People) != 0 || len(got.Groups) != 0 || len(got.MapEffects.PinEntityIDs) != 0 || len(got.ResultSet.Sources) != 0 || got.ResultSet.AnswerBinding != nil || got.ResultSet.Status != "empty" {
				t.Fatal("unavailable original licensed membership acquired unrelated cards, pins or source capabilities")
			}
			if got.Message != "这条回答使用的地点资料已经变更，请重新询问。" || got.Note != "原会话文字保留；旧结果和来源操作已停用。" {
				t.Fatal("retired licensed round was presented as successful fresh results", got.Message, got.Note)
			}
			after, _ := json.Marshal(port.task)
			if string(before) != string(after) || !reflect.DeepEqual(got.Task.Conversation, initial.Task.Conversation) || len(got.MessageResults) != len(port.entries) {
				t.Fatal("retirement changed original task/history or discarded an authorized older round")
			}
			port.handle.err = arp.ErrChanged
			if !errors.Is(read.Revalidate(context.Background()), arp.ErrChanged) {
				t.Fatal("retired main result bypassed the returned native final barrier")
			}
		})
	}
}

func TestLicensedReplyValidLatestKeepsExactMembershipDespiteExpiredDispatch(t *testing.T) {
	for _, older := range []string{"authorized", "retired"} {
		t.Run(older, func(t *testing.T) {
			s, port, initial := licensedNoFallbackFixture(t)
			latest := port.entries[1]
			if older == "retired" {
				port.entries = port.entries[1:]
			}
			got, read, primary, err := licensedNoFallbackPrepare(t, s, initial)
			if err != nil || !primary || read == nil || port.searches != 0 || !reflect.DeepEqual(got.ResultSet.Entities, latest.ResultSet.Entities) || !reflect.DeepEqual(got.MapEffects, latest.MapEffects) || got.ResultSet.ID != latest.ResultSet.ID || got.Message != initial.Task.Conversation[3].Text || got.ResultSet.AnswerBinding != nil || len(got.MessageResults) != len(port.entries) {
				t.Fatal("current authorized membership was swallowed by old history or dispatch expiry", err)
			}
		})
	}
}

func TestLicensedReplyWrongLatestCorrelationCannotBecomeBroadSearch(t *testing.T) {
	for _, mismatch := range []string{"task", "city", "kind", "current_query", "missing_membership"} {
		t.Run(mismatch, func(t *testing.T) {
			s, port, initial := licensedNoFallbackFixture(t)
			port.entries = port.entries[:1]
			switch mismatch {
			case "task":
				port.task.Conversation[3].ResultMembership.TaskID = "66666666-6666-4666-8666-666666666666"
			case "city":
				port.task.Conversation[3].ResultMembership.CityID = "another-city"
			case "kind":
				port.task.Intent = agentworkspace.FindActivity
			case "current_query":
				port.task.Filters["currentQuery"] = "UNIT later task query"
			case "missing_membership":
				port.task.Conversation[3].ResultMembership = nil
			}
			initial.Task = &port.task
			_, _, primary, err := licensedNoFallbackPrepare(t, s, initial)
			if !errors.Is(err, arp.ErrChanged) || primary || port.searches != 0 {
				t.Fatal("wrong original task/query correlation was silently broadened", err, primary, port.searches)
			}
		})
	}
}

func TestLicensedReplyReaderDenialAndSourceChangeRemainTypedFailures(t *testing.T) {
	for _, refusal := range []error{arp.ErrDenied, arp.ErrChanged, arp.ErrUnavailable} {
		t.Run(refusal.Error(), func(t *testing.T) {
			s, port, initial := licensedNoFallbackFixture(t)
			port.err = refusal
			_, read, primary, err := licensedNoFallbackPrepare(t, s, initial)
			if !errors.Is(err, refusal) || primary || read != nil || port.searches != 0 {
				t.Fatal("native refusal was converted to successful empty result or a fresh search", err)
			}
		})
	}
}

func TestLicensedReplyReaderRejectsWrongOwnedTaskSnapshot(t *testing.T) {
	for _, mismatch := range []string{"task_id", "expected_snapshot"} {
		t.Run(mismatch, func(t *testing.T) {
			s, port, initial := licensedNoFallbackFixture(t)
			other := *initial.Task
			if mismatch == "task_id" {
				other.ID = "66666666-6666-4666-8666-666666666666"
			} else {
				other.Query = "UNIT changed stored task snapshot"
			}
			initial.Task = &other
			_, read, primary, err := licensedNoFallbackPrepare(t, s, initial)
			if !errors.Is(err, arp.ErrDenied) || primary || read != nil || port.reads != 1 || port.searches != 0 {
				t.Fatal("wrong owner-bound task snapshot reached retirement or a fresh search", err)
			}
		})
	}
}

func pendingLicensedFixture(t *testing.T) (*server, *licensedNoFallbackHTTPPort, agentworkspace.Results) {
	t.Helper()
	s, port, initial := licensedNoFallbackFixture(t)
	port.task.Status = agentworkspace.TaskActive
	port.task.Filters["targetIntent"] = agentworkspace.FindPlace
	port.task.Filters["currentQuery"] = "UNIT pending next restaurant field query"
	port.task.Conversation = append(port.task.Conversation, agentworkspace.Message{Role: "user", Text: port.task.Filters["currentQuery"]})
	initial.Task, initial.Query = &port.task, port.task.Filters["currentQuery"]
	return s, port, initial
}

func TestLicensedReplyPendingUserTurnHasNoFreshOrOldAnswerResults(t *testing.T) {
	for _, history := range []string{"authorized", "previous_licensed_retired", "all_retired"} {
		t.Run(history, func(t *testing.T) {
			s, port, initial := pendingLicensedFixture(t)
			if history == "previous_licensed_retired" {
				port.entries = port.entries[:1]
			} else if history == "all_retired" {
				port.entries = nil
			}
			before, _ := json.Marshal(port.task)
			got, read, primary, err := licensedNoFallbackPrepare(t, s, initial)
			if err != nil || !primary || read == nil || port.reads != 1 || port.searches != 0 {
				t.Fatal("pending licensed turn executed broad search instead of owned empty read", err, primary, port.reads, port.searches)
			}
			if !got.NativeProjection || len(got.ProjectionItems) != 0 || len(got.ResultSet.Entities) != 0 || len(got.ResultSet.Items) != 0 || len(got.ResultSet.Sources) != 0 || got.ResultSet.AnswerBinding != nil || len(got.MapEffects.PinEntityIDs) != 0 || len(got.Places) != 0 || len(got.Activities) != 0 || len(got.People) != 0 || len(got.Groups) != 0 || len(got.Organizations) != 0 || got.ResultSet.Status != "empty" {
				t.Fatal("unfinished user turn acquired previous/new model cards, pins or sources")
			}
			if got.Message != "这轮查询尚未完成；尚无可用结果。" || got.Note != "原会话文字保留；本轮没有已确认的结果和来源。" || len(got.MessageResults) != len(port.entries) {
				t.Fatal("pending query was disguised as completed answer or authorized history was discarded")
			}
			after, _ := json.Marshal(port.task)
			if string(before) != string(after) || !reflect.DeepEqual(got.Task, initial.Task) {
				t.Fatal("pending read rewrote task status, user query, saved membership or history")
			}
			port.handle.err = arp.ErrChanged
			if !errors.Is(read.Revalidate(context.Background()), arp.ErrChanged) {
				t.Fatal("pending result bypassed owned history final source check")
			}
		})
	}
}

func TestLicensedReplyPendingUserTurnRejectsChangedCorrelation(t *testing.T) {
	for _, mismatch := range []string{"current_query", "prior_task", "prior_city", "prior_membership_missing"} {
		t.Run(mismatch, func(t *testing.T) {
			s, port, initial := pendingLicensedFixture(t)
			switch mismatch {
			case "current_query":
				port.task.Filters["currentQuery"] = "UNIT changed current query"
			case "prior_task":
				port.task.Conversation[3].ResultMembership.TaskID = "66666666-6666-4666-8666-666666666666"
			case "prior_city":
				port.task.Conversation[3].ResultMembership.CityID = "another-city"
			case "prior_membership_missing":
				port.task.Conversation[3].ResultMembership = nil
			}
			_, _, primary, err := licensedNoFallbackPrepare(t, s, initial)
			if !errors.Is(err, arp.ErrChanged) || primary || port.searches != 0 {
				t.Fatal("corrupt prior/current task binding enabled fresh broad results", err, primary, port.searches)
			}
		})
	}
}

func TestLicensedReplyPendingReaderDenialRemainsFailure(t *testing.T) {
	for _, refusal := range []error{arp.ErrDenied, arp.ErrChanged, arp.ErrUnavailable} {
		t.Run(refusal.Error(), func(t *testing.T) {
			s, port, initial := pendingLicensedFixture(t)
			port.err = refusal
			_, read, primary, err := licensedNoFallbackPrepare(t, s, initial)
			if !errors.Is(err, refusal) || read != nil || primary || port.searches != 0 {
				t.Fatal("pending retirement ignored owned source denial", err)
			}
		})
	}
}

func TestLicensedReplyPendingGuardLeavesOtherActiveQueriesUnchanged(t *testing.T) {
	for _, ordinary := range []string{"fresh_active", "rules_previous", "local_previous", "activity", "in_progress", "clarifying", "answered_tail", "nonplace_target"} {
		t.Run(ordinary, func(t *testing.T) {
			s, port, initial := pendingLicensedFixture(t)
			switch ordinary {
			case "fresh_active":
				port.task.Conversation = port.task.Conversation[4:]
				port.entries = nil
			case "rules_previous":
				port.task.Conversation[3].LicensedPlaceModelEvidence = nil
			case "local_previous":
				port.task.Conversation[3].LicensedPlaceModelEvidence = nil
				port.task.Conversation[3].LocalReplyEvidence = &agentworkspace.LocalPlaceReplyEvidence{}
			case "activity":
				port.task.Intent = agentworkspace.FindActivity
			case "in_progress":
				port.task.Status = "IN_PROGRESS"
			case "clarifying":
				port.task.Status = "CLARIFYING"
			case "answered_tail":
				port.task.Conversation = port.task.Conversation[:4]
			case "nonplace_target":
				port.task.Filters["targetIntent"] = agentworkspace.FindActivity
			}
			r := httptest.NewRequest(http.MethodGet, "/unit-pending-ordinary", nil)
			got, read, primary, err := s.prepareAgentMessageResults(r, initial, [32]byte{1}, identity.Actor{ID: httpActionSafetyOwner, AccountType: "person"})
			if err != nil || primary || read == nil || port.reads != 1 || !reflect.DeepEqual(got.ResultSet, initial.ResultSet) {
				t.Fatal("narrow pending licensed guard blocked or changed unrelated query", ordinary, err)
			}
		})
	}
}
