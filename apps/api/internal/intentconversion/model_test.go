package intentconversion

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestIntentConversionHumanShapeExcludesServerRevalidation(t *testing.T) {
	v := Preview{Envelope: Envelope{SchemaVersion: Schema}, Revalidate: func(context.Context) error { return nil }}
	raw, e := json.Marshal(v)
	if e != nil || strings.Contains(string(raw), "Revalidate") || strings.Contains(string(raw), "Session") {
		t.Fatal("server only read proof leak", e)
	}
}
