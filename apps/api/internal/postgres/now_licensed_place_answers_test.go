package postgres

import (
	"context"
	"go/ast"
	"go/parser"
	"go/token"
	"strings"
	"testing"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/agentworkspace"
	"github.com/birdtie/birdtie/apps/api/internal/modelgateway"
	"github.com/jackc/pgx/v5"
)

func TestNowLicensedPlaceEligibilityKeepsOriginalOwnerTaskBoundary(t *testing.T) {
	n := &ownNowLicensedPlaceAnswers{options: NowLicensedPlaceAnswerOptions{OwnerID: "00000000-0000-4000-8000-000000000001"}}
	base := agentworkspace.Task{PrincipalType: "person", PrincipalID: n.options.OwnerID, ActingUserID: n.options.OwnerID, Status: agentworkspace.TaskActive, ContextType: "CITY", CityID: "aberdeen", Intent: agentworkspace.FindPlace}
	if !n.Eligible(context.Background(), [32]byte{1}, base) {
		t.Fatal("valid owner public place scope rejected")
	}
	for name, edit := range map[string]func(*agentworkspace.Task){"other_owner": func(t *agentworkspace.Task) { t.PrincipalID = "other" }, "organization": func(t *agentworkspace.Task) { t.PrincipalType = "organization" }, "completed": func(t *agentworkspace.Task) { t.Status = agentworkspace.TaskCompleted }, "online": func(t *agentworkspace.Task) { t.ContextType = "ONLINE" }, "generic_chat": func(t *agentworkspace.Task) { t.Intent = agentworkspace.FindActivity }, "compare_activity_mapping": func(t *agentworkspace.Task) { t.Intent = agentworkspace.CompareResults }} {
		t.Run(name, func(t *testing.T) {
			v := base
			edit(&v)
			if n.Eligible(context.Background(), [32]byte{1}, v) {
				t.Fatal("unapproved scope admitted")
			}
		})
	}
	if n.Eligible(nil, [32]byte{1}, base) || n.Eligible(context.Background(), [32]byte{}, base) {
		t.Fatal("missing native session/context admitted")
	}
}

func TestNowLicensedRealProviderVersionCorrelatesBareAuditedModel(t *testing.T) {
	d := modelgateway.ProviderDescriptor{ProviderID: "tencent_tokenhub", ModelID: modelgateway.TencentTokenHubDeepSeekModel, ModelVersion: modelgateway.TencentTokenHubDeepSeekModel, Mode: modelgateway.Live}
	r := modelgateway.Result{ProviderID: d.ProviderID, ProviderModelVersion: "deepseek-v4-pro-0813@deepseek-v4-pro-0813", ProviderRequestID: "test-only-real-format"}
	if got, err := licensedPlaceResultModel(d, r); err != nil || got != d.ModelID {
		t.Fatal("original Gateway real ModelID@Version shape rejected or retained as bare model", got, err)
	}
	for _, bad := range []modelgateway.Result{{ProviderID: d.ProviderID, ProviderModelVersion: d.ModelID, ProviderRequestID: r.ProviderRequestID}, {ProviderID: d.ProviderID, ProviderModelVersion: d.ModelID + "@other", ProviderRequestID: r.ProviderRequestID}, {ProviderID: "other", ProviderModelVersion: r.ProviderModelVersion, ProviderRequestID: r.ProviderRequestID}, {ProviderID: d.ProviderID, ProviderModelVersion: r.ProviderModelVersion}} {
		if _, err := licensedPlaceResultModel(d, bad); err == nil {
			t.Fatal("unbound provider/version/request accepted")
		}
	}
	d.ModelVersion = "other"
	if _, err := licensedPlaceResultModel(d, r); err == nil {
		t.Fatal("unreviewed descriptor version mapped to audited bare model")
	}
}

func TestNowLicensedFinalizerKeepsOriginalTaskLedgerLockOrderAndFinalFence(t *testing.T) {
	for _, test := range []struct {
		file, function string
		order          []string
	}{
		{"now_licensed_place_answers.go", "finalizeLicensedPlaceReply", []string{"beginCurrentToolRead", "ownReplyTaskTx", "licensedReplyTryOwnerLock", "replayLicensedPlaceEgressTx", "captureReplyCurrentSourceTx", "persistReplyMembershipTx", "finalReplySourceProofsTx", "ValidateLicensedPlaceModelEvidenceTx", "recheckAgentPrivateSession", "Commit"}},
		{"model_egress_live_licensed_places.go", "PreviewOwnLicensedPlaceEgress", []string{"prepareLicensedPlaceEgressTx", "egressOwnerLock", "finishLivePreparation", "Commit"}},
	} {
		f, e := parser.ParseFile(token.NewFileSet(), test.file, nil, 0)
		if e != nil {
			t.Fatal(e)
		}
		var calls []string
		for _, d := range f.Decls {
			fn, ok := d.(*ast.FuncDecl)
			if !ok || fn.Name.Name != test.function {
				continue
			}
			ast.Inspect(fn.Body, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok {
					return true
				}
				switch c := call.Fun.(type) {
				case *ast.SelectorExpr:
					calls = append(calls, c.Sel.Name)
				case *ast.Ident:
					calls = append(calls, c.Name)
				}
				return true
			})
		}
		at := 0
		for _, want := range test.order {
			found := false
			for at < len(calls) {
				name := calls[at]
				at++
				if name == want {
					found = true
					break
				}
			}
			if !found {
				t.Fatalf("%s lost ordered Task/CAS -> ledger -> final source/session fence %s: %s", test.function, want, strings.Join(calls, ","))
			}
		}
	}
}

type licensedLedgerUnitTx struct {
	pgx.Tx
	acquired bool
	failure  error
	query    string
	args     []any
}

func (x *licensedLedgerUnitTx) QueryRow(_ context.Context, q string, args ...any) pgx.Row {
	x.query, x.args = q, args
	return currentReadonlyUnitRow{values: []any{x.acquired}, err: x.failure}
}
func TestNowLicensedFinalizerOwnerContentionNeverWaitsOrRewritesAccounting(t *testing.T) {
	for _, acquired := range []bool{true, false} {
		x := &licensedLedgerUnitTx{acquired: acquired}
		err := licensedReplyTryOwnerLock(context.Background(), x, "test-only-owner")
		if (err == nil) != acquired {
			t.Fatal("failed try-lock did not withhold publication", err)
		}
		if x.query != `SELECT pg_try_advisory_xact_lock(hashtextextended('birdtie.model-budget.owner:'||$1,0))` || len(x.args) != 1 || x.args[0] != "test-only-owner" {
			t.Fatal("ledger lock waits, uses another key or mutates accounting", x.query)
		}
	}
	x := &licensedLedgerUnitTx{failure: pgx.ErrNoRows}
	if licensedReplyTryOwnerLock(context.Background(), x, "test-only-owner") == nil {
		t.Fatal("unknown try-lock outcome published reply")
	}
}
func TestLicensedModelEvidenceCannotReconstructNativeAuthority(t *testing.T) {
	for _, m := range []agentworkspace.Message{{}, {Role: "assistant", Text: "fake [source-1]", LicensedPlaceModelEvidence: &agentworkspace.LicensedPlaceModelEvidence{Schema: agentworkspace.LicensedPlaceModelReplySchema, ModelCalls: 1}}} {
		if _, _, err := ValidateLicensedPlaceModelEvidenceTx(context.Background(), nil, "00000000-0000-4000-8000-000000000001", "00000000-0000-4000-8000-000000000002", m, time.Now().UTC()); err == nil {
			t.Fatal("JSON IDs or empty proof minted authority")
		}
	}
}
