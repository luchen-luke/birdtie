package postgres

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/agentworkspace"
	"github.com/birdtie/birdtie/apps/api/internal/content"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	"github.com/birdtie/birdtie/apps/api/internal/media"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

func TestHumanTaskImageTaskLookupFailureClassification(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
		want error
	}{
		{"missing_task", agentworkspace.ErrNotFound, content.ErrNotFound},
		{"wrapped_missing_task", fmt.Errorf("task lookup: %w", agentworkspace.ErrNotFound), content.ErrNotFound},
		{"raw_missing_row", pgx.ErrNoRows, content.ErrNotFound},
		{"wrapped_missing_row", fmt.Errorf("task lookup: %w", pgx.ErrNoRows), content.ErrNotFound},
		{"database_failure", &pgconn.PgError{Code: "08006"}, content.ErrUnavailable},
		{"wrapped_database_failure", fmt.Errorf("task lookup: %w", &pgconn.PgError{Code: "08006"}), content.ErrUnavailable},
		{"cancelled", context.Canceled, content.ErrUnavailable},
		{"deadline", context.DeadlineExceeded, content.ErrUnavailable},
		{"identity_rejected", identity.ErrUnauthorized, identity.ErrUnauthorized},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := taskImageTaskFailure(tc.err); !errors.Is(got, tc.want) {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
		})
	}
}

type taskImageLookupRow func(...any) error

func (r taskImageLookupRow) Scan(dest ...any) error { return r(dest...) }

func TestHumanTaskImageScanTaskLookupFailureClassification(t *testing.T) {
	for _, tc := range []struct {
		name string
		row  taskImageLookupRow
		want error
	}{
		{"missing_row_becomes_agent_not_found", func(...any) error { return pgx.ErrNoRows }, content.ErrNotFound},
		{"wrapped_missing_row_becomes_agent_not_found", func(...any) error {
			return fmt.Errorf("task lookup: %w", pgx.ErrNoRows)
		}, content.ErrNotFound},
		{"database_failure_remains_unavailable", func(...any) error {
			return &pgconn.PgError{Code: "08006"}
		}, content.ErrUnavailable},
		{"corrupt_task_data_remains_unavailable", func(dest ...any) error {
			*dest[10].(*[]byte) = []byte(`{`)
			*dest[11].(*[]byte) = []byte(`[]`)
			return nil
		}, content.ErrUnavailable},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := scanAgentTask(tc.row)
			if err == nil {
				t.Fatal("failed task lookup unexpectedly succeeded")
			}
			if got := taskImageTaskFailure(err); !errors.Is(got, tc.want) {
				t.Fatalf("got %v after task scan, want %v", got, tc.want)
			}
		})
	}
}

func TestHumanTaskImageRejectsMalformedSelectionBeforeSQL(t *testing.T) {
	var s *Store
	ctx := context.Background()
	a := identity.Actor{}
	if _, e := s.PreviewHumanTaskImage(ctx, [32]byte{}, a, "bad", media.TaskImageInput{}); e == nil {
		t.Fatal("bad preview")
	}
	if _, e := s.SaveHumanTaskImage(ctx, [32]byte{}, a, "bad", "bad", "image/png", nil); e == nil {
		t.Fatal("bad save")
	}
	if _, e := s.ReadHumanTaskImages(ctx, [32]byte{}, a, "bad", "", true, false, nil); e == nil {
		t.Fatal("bad read")
	}
	if e := s.DeleteHumanTaskImage(ctx, [32]byte{}, a, "bad", "bad", 0); e == nil {
		t.Fatal("bad delete")
	}
}

func TestHumanTaskImageInvalidatedPreviewNeverRenewsUploadProof(t *testing.T) {
	at := time.Now().UTC()
	v := taskImageRow{source: "native-source", session: "native-session", sessionGeneration: 3,
		receipt: media.TaskImageReceipt{Status: "preview", PreviewExpiresAt: at.Add(time.Minute)}}
	if !taskImagePreviewCurrent(v, v.source, v.session, 3, at) {
		t.Fatal("current proof lost")
	}
	for _, c := range []struct {
		source, session string
		generation      int64
		at              time.Time
	}{
		{"changed-task", v.session, 3, at}, {v.source, "fresh-session", 3, at},
		{v.source, v.session, 4, at}, {v.source, v.session, 3, v.receipt.PreviewExpiresAt},
	} {
		if taskImagePreviewCurrent(v, c.source, c.session, c.generation, c.at) {
			t.Fatal("invalid preview became upload approval")
		}
	}
	if media.TaskImageInvalidatedPreview == "preview" {
		t.Fatal("disposition must remain distinct from upload approval")
	}
}
func TestHumanTaskImageSourceIsNativeNotClientAgentSelector(t *testing.T) {
	for _, s := range []string{"", `[]`, `["owner","generation","bad"]`} {
		if taskImageAgentSQLArgument(s) != "" {
			t.Fatal("bad source accepted")
		}
	}
	source := `["owner","generation","11111111-1111-4111-8111-111111111111","ag-generation","task","task-generation","updated","city","city-generation","cc-generation","context","context-generation"]`
	if taskImageAgentSQLArgument(source) != "11111111-1111-4111-8111-111111111111" {
		t.Fatal("native argument lost")
	}
	for _, required := range []string{"t.principal_type='person'", "t.context_type='CITY'", "cx.city_id=c.id", "c.publication_status='published'", "cc.status='active'", "c.expires_at>clock_timestamp()", "a.xmin::text", "ag.xmin::text", "t.xmin::text"} {
		if !strings.Contains(taskImageSourceSQL, required) {
			t.Fatalf("missing native gate %s", required)
		}
	}
}

func TestHumanTaskImageNativeAssetInsertHasOneUUIDParameterType(t *testing.T) {
	if strings.Count(taskImageAssetInsertSQL, "$1::uuid") != 2 || strings.Contains(taskImageAssetInsertSQL, "$1::text") {
		t.Fatal("native UUID storage key must not change PostgreSQL parameter type")
	}
}
