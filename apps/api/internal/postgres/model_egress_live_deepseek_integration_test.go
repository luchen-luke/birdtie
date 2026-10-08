package postgres

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/modelegressbudget"
	"github.com/birdtie/birdtie/apps/api/internal/modelgateway"
	"github.com/jackc/pgx/v5/pgconn"
)

// Real SQL uses the original 062 fixture and fake provider transport. These
// assertions are neither a live Tencent call nor a tokenizer/price receipt.
func TestModelLiveDeepSeekNativeSQLExactPriceAndUnknownHoldIntegration(t *testing.T) {
	f := newLiveNativeFixture(t)
	b := f.configuration.native.private.base
	a := f.configuration.native.access
	var shape string
	if err := b.pool.QueryRow(b.ctx, `SELECT pg_get_constraintdef(oid) FROM pg_constraint WHERE conname='model_live_price_shape' AND conrelid='model_local_price_versions'::regclass`).Scan(&shape); err != nil || !strings.Contains(shape, "deepseek-v4-pro-0813") {
		t.Fatal("DeepSeek native SQL requires migration 112", err)
	}
	// Preserve the original fixture's immutable unused HY3 row for mismatch
	// checks; its owned cleanup has no references and never touches real holds.
	old := f.token
	f.token = liveDeepSeekPrice(time.Now().UTC().Truncate(time.Microsecond))
	f.token.Base.Version = "unit_deepseek_" + strings.ReplaceAll(b.person.ID, "-", "")
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if _, err := b.pool.Exec(ctx, `DELETE FROM model_local_price_versions WHERE version=$1`, old.Base.Version); err != nil {
			t.Errorf("owned unused HY3 price cleanup: %v", err)
		}
	})
	if err := b.store.RegisterLivePrice(b.ctx, f.token); err != nil {
		t.Fatal("register exact peak DS price", err)
	}
	f.projector = liveDeepSeekAdapter(t)
	p := f.preview(t, modelegressbudget.LiveToken, true)
	if p.Upper.Amount != (modelegressbudget.Amount{InputTokens: 16384, OutputTokens: 768, CostMicros: 168192}) || p.Prepared().ModelWire().Descriptor().ModelID != modelgateway.TencentTokenHubDeepSeekModel {
		t.Fatal("native SQL chose another price/proof")
	}
	r := f.reserve(t, p)
	allocated := modelegressbudget.Limits{Requests: 1, InputTokens: 16384, OutputTokens: 768, CostMicros: 168192}
	assertLiveNativeViews(t, f, allocated)
	duplicate, err := New(b.pool, false).ReserveOwnLiveAttempt(b.ctx, a, modelegressbudget.ReserveInput{OperationID: r.OperationID, PreviewID: p.ID, RootTraceID: f.root, TaskID: p.TaskID}, f.controller, f.projector)
	if err != nil || !duplicate.CreatedAt.Equal(r.CreatedAt) {
		t.Fatal("duplicate reserve changed original ledger", err)
	}
	assertLiveNativeViews(t, f, allocated)
	for name, replacement := range map[string]string{
		"borrow_hy3_price":  `jsonb_build_object('price_version',$3::text)`,
		"borrow_hy3_amount": `jsonb_build_object('upper_input',196608,'upper_output',768,'upper_cost',199680)`,
	} {
		t.Run(name, func(t *testing.T) {
			id := liveNativeID(t, f)
			t.Cleanup(func() {
				ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
				defer cancel()
				// Only a copied fixture row could exist if this negative test
				// failed. Keep its original operation and all holds untouched.
				if _, err := b.pool.Exec(ctx, `DELETE FROM model_budget_reservations WHERE operation_id=$1 AND owner_id=$2`, id, b.person.ID); err != nil {
					t.Errorf("owned negative reservation cleanup: %v", err)
				}
			})
			// The row satisfies a closed arithmetic shape, but must fail the
			// exact preview/price binding before it can enter the ledger.
			sql := `INSERT INTO model_budget_reservations SELECT (jsonb_populate_record(NULL::model_budget_reservations,to_jsonb(x)||jsonb_build_object('operation_id',$1::text)||` + replacement + `)).* FROM model_budget_reservations x WHERE operation_id=$2::uuid`
			args := []any{id, r.OperationID}
			if name == "borrow_hy3_price" {
				args = append(args, old.Base.Version)
			}
			_, err := b.pool.Exec(b.ctx, sql, args...)
			var pgError *pgconn.PgError
			if !errors.As(err, &pgError) || pgError.Code != "P0001" || pgError.Message != "exact live price preview and amount required" {
				t.Fatal("expected exact SQL price binding rejection was absent")
			}
			assertLiveNativeViews(t, f, allocated)
		})
	}
	port, request, err := b.store.NewOwnLiveModelDispatch(b.ctx, a, r.OperationID, f.controller, f.projector, liveNativeGate{})
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := modelgateway.ParseTencentTokenHubConfig([]byte(`{"provider":"tencent_tokenhub","baseUrl":"https://tokenhub.tencentmaas.com/v1","model":"deepseek-v4-pro-0813","maxOutputTokens":768,"apiKey":"unit-test-placeholder"}`))
	if err != nil {
		t.Fatal(err)
	}
	calls := 0
	adapter, err := modelgateway.NewTencentTokenHubAdapter(cfg, liveNativeTransport(func(req *http.Request) (*http.Response, error) {
		calls++
		assertLiveNativePhase(t, f, r.OperationID, "IN_FLIGHT", "LIVE_IN_FLIGHT")
		wire, err := io.ReadAll(req.Body)
		if err != nil || !bytes.Equal(wire, p.Prepared().ModelWire().ExactWire()) || req.GetBody != nil || !req.Close {
			t.Fatal("different or replayable provider wire", err)
		}
		return liveNativeResponse(`{"id":"unit-deepseek-1","model":"deepseek-v4-pro-0813","choices":[{"index":0,"message":{"role":"assistant","content":"UNIT fake response, not live evidence."},"finish_reason":"stop"}],"usage":{"prompt_tokens":31,"completion_tokens":12,"total_tokens":43}}`, 200), nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	gate, err := modelgateway.NewNativeLiveGateway(liveNativeGate{}, adapter, port)
	if err != nil {
		t.Fatal(err)
	}
	result, err := gate.Complete(b.ctx, request)
	if err != nil || result.Status != modelgateway.Completed || result.Usage.CostStatus != "UNKNOWN" || calls != 1 {
		t.Fatal("native fake DS dispatch failed", err)
	}
	assertLiveNativePhase(t, f, r.OperationID, "UNKNOWN", "LIVE_ATTEMPTED")
	assertLiveNativeViews(t, f, allocated)
	if _, err := b.store.FinishOwnLiveAttempt(b.ctx, a, r.OperationID, result.Usage); err != nil {
		t.Fatal(err)
	}
	assertLiveNativeViews(t, f, allocated)
	if _, err := b.store.BeginOwnLiveAttempt(b.ctx, a, r.OperationID, f.controller, f.projector); err == nil {
		t.Fatal("UNKNOWN operation dispatched twice")
	}
}
