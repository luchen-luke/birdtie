package httpapi

import (
	"encoding/json"
	si "github.com/birdtie/birdtie/apps/api/internal/socialintent"
	"testing"
	"time"
)

func TestSocialIntentCreationLegacyValidationRemainsTimeBound(t *testing.T) {
	in := si.DraftInput{Type: "FIND_ACTIVITY", Title: "旧客户端私人意图", Constraints: json.RawMessage(`{}`), Modality: "ONLINE", Audience: "PRIVATE", ExpiresAt: time.Now().Add(time.Hour)}
	if !validSocialIntentDraft(&in) {
		t.Fatal("legacy valid input rejected")
	}
	in.ExpiresAt = time.Now().Add(-time.Minute)
	if validSocialIntentDraft(&in) {
		t.Fatal("legacy current expiry changed")
	}
	in.OperationID = "11111111-1111-4111-8111-111111111111"
	if _, e := si.NormalizeCreation(in, ""); e != nil {
		t.Fatal("keyed replay shape must not use current expiry", e)
	}
}
