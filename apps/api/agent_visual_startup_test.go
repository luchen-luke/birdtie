package main

import (
	"context"
	av "github.com/birdtie/birdtie/apps/api/internal/agentvisual"
	"path/filepath"
	"strings"
	"testing"
)

func TestAgentVisualStartupOffAndTrustedLocalBoundary(t *testing.T) {
	gateway, err := loadAgentVisualStartup("", "0.0.0.0:18112", false, nil, nil)
	if err != nil || gateway == nil {
		t.Fatal("OFF startup must expose closed unavailable gateway")
	}
	input := av.PreviewInput{OperationID: "12600000-0000-4000-8000-000000000001", ClientDraftID: "12600000-0000-4000-8000-000000000002", ClientDraftRevision: 1, SourceContextType: "CITY", SourceContextID: "test-city", Purpose: av.Purpose, Image: av.ImageInput{MIME: "image/png", ContentBase64: "AA==", InputSHA256: strings.Repeat("1", 64), PixelRisk: "UNKNOWN"}}
	if _, err = gateway.Preview(context.Background(), av.Access{}, input); err != av.ErrUnavailable {
		t.Fatal("default preview admitted")
	}
	path := filepath.Join(t.TempDir(), ".env.agent.visual.local.json")
	for _, tc := range []struct {
		path, address string
		dev           bool
	}{
		{".env.agent.visual.local.json", "127.0.0.1:18112", true},
		{path, "0.0.0.0:18112", true},
		{path, "127.0.0.1:18112", false},
		{filepath.Join(t.TempDir(), "other.json"), "127.0.0.1:18112", true},
		{path, "127.0.0.1:18112", true},
	} {
		if g, e := loadAgentVisualStartup(tc.path, tc.address, tc.dev, nil, nil); e == nil || g != nil {
			t.Fatal("untrusted/missing vision configuration admitted")
		}
	}
}
