package agentparticipationsignal

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/birdtie/birdtie/apps/api/internal/actorref"
	"github.com/birdtie/birdtie/apps/api/internal/agentevent"
	"strings"
	"testing"
	"time"
)

func testSignal() Signal {
	now := time.Now().UTC()
	subject := actorref.PrincipalRef{Type: actorref.Person, ID: "78000000-0000-4000-8000-000000000001"}
	s := Signal{SchemaVersion: SchemaVersion, Kind: RSVPGoing, Subject: subject, AgentID: "78000000-0000-4000-8000-000000000002", LogicalOperationID: "78000000-0000-4000-8000-000000000003", Source: agentevent.SourceReference{Type: agentevent.ParticipationSource, ID: "78000000-0000-4000-8000-000000000004", Owner: subject, Version: agentevent.SourceVersion{Kind: agentevent.UpdatedAtDigestVersion, Token: strings.Repeat("a", 64)}}, SourceUpdatedAt: now.Add(-time.Second), ObservedAt: now, ExpiresAt: now.Add(-time.Second).Add(agentevent.MaxEventTTL), ActivityID: "78000000-0000-4000-8000-000000000005", ActivityRevision: 2, ActivitySnapshotDigest: strings.Repeat("b", 64), MetadataCreatedAt: now.Add(-time.Hour), ReceiptSemantics: string(agentevent.CurrentRetainedState), Attendance: "UNKNOWN", StableInterest: "UNKNOWN", ProcessingStatus: "UNAVAILABLE"}
	s.SignalID = stableID(s)
	return s
}
func TestParticipationSignalStatesAndUnknownAssertions(t *testing.T) {
	for _, tc := range []struct {
		status string
		kind   Kind
	}{{"going", RSVPGoing}, {"pending", RSVPPending}, {"cancelled", RSVPCancelled}} {
		t.Run(tc.status, func(t *testing.T) {
			kind, e := stateKind(tc.status)
			if e != nil || kind != tc.kind {
				t.Fatal(e)
			}
			s := testSignal()
			s.Kind = kind
			if kind == RSVPCancelled {
				s.ActivityID = ""
				s.ActivityRevision = 0
				s.ActivitySnapshotDigest = ""
			}
			s.SignalID = stableID(s)
			if e = Validate(s, s.ObservedAt); e != nil {
				t.Fatal(e)
			}
		})
	}
	for _, s := range []string{"WAITLIST", "waitlist", "attended", "completed", "finished", "joined", "left", ""} {
		if _, e := stateKind(s); !errors.Is(e, ErrUnavailable) {
			t.Fatal("invented status", s, e)
		}
	}
	s := testSignal()
	id := s.SignalID
	s.ObservedAt = s.ObservedAt.Add(time.Second)
	if stableID(s) != id {
		t.Fatal("retry changed identity")
	}
	if e := Validate(s, s.ObservedAt); e != nil {
		t.Fatal(e)
	}
	if e := Validate(s, s.ExpiresAt); !errors.Is(e, ErrExpired) {
		t.Fatal("expired lease accepted", e)
	}
	if raw, e := json.Marshal(s); e != nil || strings.Contains(string(raw), "attended\":true") {
		t.Fatal("bad metadata output", e)
	}
	var caller Signal
	if e := json.Unmarshal([]byte(`{"confirmed":true,"attendance":"CONFIRMED"}`), &caller); !errors.Is(e, ErrAuthorityJSON) || caller.SchemaVersion != "" {
		t.Fatal("JSON manufactured server signal")
	}
}
func TestParticipationSignalRejectsTamperedMetadata(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*Signal)
	}{
		{"attendance", func(s *Signal) { s.Attendance = "CONFIRMED" }}, {"stable_interest", func(s *Signal) { s.StableInterest = "BADMINTON" }}, {"processing", func(s *Signal) { s.ProcessingStatus = "ACCEPTED" }},
		{"unknown_kind", func(s *Signal) { s.Kind = "WAITLIST" }}, {"missing_target", func(s *Signal) { s.ActivityID = "" }}, {"fake_revision", func(s *Signal) { s.ActivityRevision = 0 }},
		{"wrong_owner", func(s *Signal) { s.Source.Owner.ID = s.AgentID }}, {"zero_source", func(s *Signal) { s.Source.ID = "00000000-0000-0000-0000-000000000000" }},
		{"forged_source_version", func(s *Signal) { s.Source.Version.Revision = 1 }}, {"wrong_version_kind", func(s *Signal) { s.Source.Version.Kind = agentevent.RevisionVersion }}, {"bad_token", func(s *Signal) { s.Source.Version.Token = strings.Repeat("G", 64) }},
		{"future_source", func(s *Signal) { s.SourceUpdatedAt = s.ObservedAt.Add(time.Hour) }}, {"retry_extension", func(s *Signal) { s.ExpiresAt = s.ExpiresAt.Add(time.Minute) }},
		{"missing_metadata", func(s *Signal) { s.MetadataCreatedAt = time.Time{} }}, {"cancelled_private_target", func(s *Signal) { s.Kind = RSVPCancelled }},
		{"bad_target_digest", func(s *Signal) { s.ActivitySnapshotDigest = strings.Repeat("Z", 64) }},
		{"unserializable_metadata_time", func(s *Signal) { s.MetadataCreatedAt = time.Date(-1, 1, 1, 0, 0, 0, 0, time.UTC) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := testSignal()
			tc.change(&s)
			s.SignalID = stableID(s)
			if e := Validate(s, s.ObservedAt); e == nil {
				t.Fatal("tampered source inference accepted")
			}
		})
	}
}

func TestParticipationSignalReaderUnavailableAndInputGuards(t *testing.T) {
	request := Request{"78000000-0000-4000-8000-000000000004", "78000000-0000-4000-8000-000000000003"}
	ctx := context.Background()
	for _, reader := range []*Reader{nil, NewReader(nil, false)} {
		if signal, err := reader.Collect(ctx, agentevent.Access{}, request); !errors.Is(err, ErrUnavailable) || signal.SignalID != "" {
			t.Fatal("unconfigured reader accepted source", err)
		}
	}
	reader := NewReader(nil, false)
	if _, err := reader.Collect(nil, agentevent.Access{}, request); !errors.Is(err, ErrInvalid) {
		t.Fatal(err)
	}
	for _, bad := range []Request{{"", request.LogicalOperationID}, {request.ParticipationID, ""}, {"00000000-0000-0000-0000-000000000000", request.LogicalOperationID}, {"look-alike-agent", request.LogicalOperationID}} {
		if _, err := reader.Collect(ctx, agentevent.Access{}, bad); !errors.Is(err, ErrInvalid) {
			t.Fatal("invalid selector accepted", err)
		}
	}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if signal, err := reader.Collect(canceled, agentevent.Access{}, request); !errors.Is(err, context.Canceled) || signal.SignalID != "" {
		t.Fatal(err)
	}
}
