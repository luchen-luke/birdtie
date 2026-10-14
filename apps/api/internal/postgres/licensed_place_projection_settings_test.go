package postgres

import (
	"context"
	"errors"
	"testing"

	arp "github.com/birdtie/birdtie/apps/api/internal/agentresultprojection"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type licensedProjectionUnitTx struct {
	pgx.Tx
	queries []string
	failure error
}

func (x *licensedProjectionUnitTx) Exec(_ context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	if len(args) != 0 {
		panic("projection settings must contain no task or session data")
	}
	x.queries = append(x.queries, sql)
	return pgconn.CommandTag{}, x.failure
}

func TestLicensedReplyProjectionUsesOriginalNativeTransactionSetting(t *testing.T) {
	x := &licensedProjectionUnitTx{}
	if licensedReplyProjectionSettings(context.Background(), x) != nil || len(x.queries) != 1 || x.queries[0] != "SET LOCAL jit=off" {
		t.Fatal("bounded native read did not keep its existing transaction setting")
	}
}

func TestLicensedReplyProjectionSettingsFailureWithholdsPublication(t *testing.T) {
	x := &licensedProjectionUnitTx{failure: errors.New("unit-only-setting-failure")}
	if !errors.Is(licensedReplyProjectionSettings(context.Background(), x), arp.ErrUnavailable) || !errors.Is(licensedReplyProjectionSettings(context.Background(), nil), arp.ErrUnavailable) {
		t.Fatal("missing/failed transaction settings were accepted")
	}
}
