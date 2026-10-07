package agentmemorycandidate

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestMemoryCandidateHumanWireHasNoAuthorityOrProbability(t *testing.T) {
	raw, e := json.Marshal(HumanDraft{Category: "sports"})
	if e != nil {
		t.Fatal(e)
	}
	for _, key := range []string{"assessment", "probability", "confirmed", "owner", "agentId", "memoryId", "model"} {
		if strings.Contains(string(raw), key) {
			t.Fatal("authority added", key)
		}
	}
}
