package postgres

import (
	"context"
	"errors"
	av "github.com/birdtie/birdtie/apps/api/internal/agentvisual"
	"testing"
)

func TestAgentVisualDefaultGatewayNeverBorrowsTextOrHumanAttachment(t *testing.T) {
	g := NewAgentVisualGateway(nil, AgentVisualOptions{})
	in := av.PreviewInput{OperationID: "12600000-0000-4000-8000-000000000001", ClientDraftID: "12600000-0000-4000-8000-000000000002", ClientDraftRevision: 1, SourceContextType: "CITY", SourceContextID: "test-city", Purpose: av.Purpose, Image: av.ImageInput{MIME: "image/png", InputSHA256: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", PixelRisk: "UNKNOWN"}}
	if _, e := g.Preview(context.Background(), av.Access{}, in); !errors.Is(e, av.ErrUnavailable) {
		t.Fatal("unconfigured default", e)
	}
	in.Purpose = "HUMAN_TASK_MESSAGE_ATTACHMENT"
	if _, e := g.Preview(context.Background(), av.Access{}, in); !errors.Is(e, av.ErrInvalid) {
		t.Fatal("human-only attachment purpose upgraded", e)
	}
}
