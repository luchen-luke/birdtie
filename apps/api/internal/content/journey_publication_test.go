package content

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

const journeyPubEntry = "18000000-0000-4000-8000-000000000001"
const journeyPubMoment = "18000000-0000-4000-8000-000000000002"
const journeyPubID = "18000000-0000-4000-8000-000000000003"
const journeyPubOperation = "18000000-0000-4000-8000-000000000004"

func TestJourneyPublicationClosedApprovalAndOriginalDigest(t *testing.T) {
	snapshot := JourneyPublicationInput{Title: "明确公开的旅程", Summary: "仅批准摘要", Stops: []JourneyPublicationStopInput{{EntryID: journeyPubEntry, Position: 3, Title: "批准站点", Note: "明确公开", LocationVisibility: "hidden", MomentID: journeyPubMoment, MomentRevision: 2, CommentMomentID: journeyPubMoment}}}
	in := CollectionPublicationInput{OperationID: journeyPubOperation, Revision: 4, Entries: []CollectionPublicationEntry{}, JourneySnapshot: &snapshot}
	raw, _ := json.Marshal(in)
	decoded, e := DecodeJourneyOrCollectionPublication(raw)
	if e != nil || decoded.JourneySnapshot.Stops[0].Position != 3 {
		t.Fatal("exact original STOP selection", e)
	}
	before := JourneyPublicationDigest("journey", journeyPubID, "PUBLISH", 4, &snapshot)
	changed := snapshot
	changed.Stops = append([]JourneyPublicationStopInput{}, snapshot.Stops...)
	changed.Stops[0].MomentRevision = 3
	if before == JourneyPublicationDigest("journey", journeyPubID, "PUBLISH", 4, &changed) {
		t.Fatal("Moment revision missing from approval")
	}
	for _, bad := range []string{strings.Replace(string(raw), `"position":3`, `"position":3,"position":4`, 1), strings.Replace(string(raw), `"note":"明确公开"`, `"note":"明确公开","customAddress":"PRIVATE"`, 1), strings.Replace(string(raw), `"momentRevision":2`, `"momentRevision":0`, 1), strings.Replace(string(raw), `"commentMomentId":"`+journeyPubMoment+`"`, `"commentMomentId":"`+journeyPubID+`"`, 1)} {
		if _, e := DecodeJourneyOrCollectionPublication([]byte(bad)); e == nil {
			t.Fatal("unapproved field/relationship accepted")
		}
	}
	legacy := CollectionPublicationInput{OperationID: journeyPubOperation, Revision: 1, Entries: []CollectionPublicationEntry{{PlaceID: journeyPubEntry, LocationVisibility: "hidden"}}}
	raw, _ = json.Marshal(legacy)
	old, e := DecodeJourneyOrCollectionPublication(raw)
	if e != nil || old.JourneySnapshot != nil || CollectionPublicationCopyDigest("journey", journeyPubID, "PUBLISH", 1, old.Entries, false) != CollectionPublicationDigest("journey", journeyPubID, "PUBLISH", 1, legacy.Entries) {
		t.Fatal("legacy approval changed", e)
	}
	large := snapshot
	large.Stops = nil
	for i := 1; i <= 12; i++ {
		id := "18000000-0000-4000-8000-" + strings.Repeat("0", 10) + string("0123456789ab"[i-1]) + "1"
		large.Stops = append(large.Stops, JourneyPublicationStopInput{EntryID: id, Position: i, Note: strings.Repeat("x", 1800), LocationVisibility: "hidden"})
	}
	in.JourneySnapshot = &large
	raw, _ = json.Marshal(in)
	if len(raw) < 16384 {
		t.Fatal("fixture not beyond old small publication limit")
	}
	if _, e = DecodeJourneyOrCollectionPublication(raw); e != nil {
		t.Fatal("bounded approved large snapshot rejected", e)
	}
}

func TestJourneyPublicationUnavailableGapHasNoPrivateFallback(t *testing.T) {
	v := PublicCollection{ID: journeyPubID, Kind: "journey", Title: "批准旅程", Revision: 2, SourceExpiresAt: time.Now().UTC().Add(time.Minute), SourceBinding: strings.Repeat("a", 64), Entries: []PublicCollectionEntry{}, JourneySnapshot: &PublicJourneySnapshot{SchemaVersion: JourneyPublicationSchema, PublicationRevision: 2, SourceRevision: 1, PublishedAt: time.Now().UTC(), Stops: []PublicJourneyStop{{EntryID: journeyPubEntry, Position: 3, Available: false}}}}
	if ValidatePublicCollection(v, "journey", journeyPubID) != nil {
		t.Fatal("unavailable original position rejected")
	}
	v.JourneySnapshot.Stops[0].Note = "PRIVATE"
	if ValidatePublicCollection(v, "journey", journeyPubID) == nil {
		t.Fatal("unavailable station leaked private fallback")
	}
	v.JourneySnapshot.Stops[0].Note = ""
	v.JourneySnapshot.Stops[0].MomentID = journeyPubMoment
	if ValidatePublicCollection(v, "journey", journeyPubID) == nil {
		t.Fatal("unavailable station leaked associated ID")
	}
}
