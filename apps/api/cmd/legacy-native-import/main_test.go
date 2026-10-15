package main

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/birdtie/birdtie/apps/api/internal/legacyjourney"
	"github.com/birdtie/birdtie/apps/api/internal/postgres"
)

func TestJourneyDetailsCLIEmitsAggregateFacts(t *testing.T) {
	var out bytes.Buffer
	if e := writeJourneySummary(&out, postgres.LegacyJourneyDetailsResult{RunID: "15800000-0000-4000-8000-000000000001", Status: "PREPARED_NOT_IMPORTED", Counts: legacyjourney.Counts{Journeys: 2, Entries: 4}}); e != nil {
		t.Fatal(e)
	}
	var v map[string]any
	if json.Unmarshal(out.Bytes(), &v) != nil || len(v) != 7 || v["activated"] != false || v["sourceWrites"] != float64(0) || v["mediaImported"] != false || v["countsKnown"] != true {
		t.Fatal("CLI summary widened migration authority")
	}
}
