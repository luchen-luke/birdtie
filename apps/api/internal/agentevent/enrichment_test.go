package agentevent

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

func enrichmentResolved(d Descriptor) ResolvedSource {
	f := testResolved(MomentCreated)
	f.Source.Type = d.Source
	f.NativeStatus = d.NativeStatus
	f.TextPresent = d.RequiresText
	if d.Type == UserQuery {
		return testResolved(UserQuery)
	}
	if d.VersionKind == RevisionVersion {
		f.Source.Version = SourceVersion{Kind: RevisionVersion, Revision: d.MinRevision}
	} else {
		f.Source.Version, _ = SnapshotVersion(d.Source, d.VersionKind, f.OccurredAt, []byte(`{"native":"private-state-fixture"}`))
	}
	return f
}

func TestEnrichmentClosedCatalogAndUnavailableSources(t *testing.T) {
	seen := map[Type]bool{}
	for _, d := range Catalog() {
		t.Run(string(d.Type), func(t *testing.T) {
			if seen[d.Type] || d.SchemaVersion != SchemaVersion || d.ReceiptSemantics != CurrentRetainedState {
				t.Fatal("catalog duplicated or misstated semantics")
			}
			seen[d.Type] = true
			if d.Type == ActivityCompleted || d.Type == PlaceVisited {
				if d.Support != SourceUnavailable || d.VersionKind != NoNativeVersion || d.UnsupportedReason == "" {
					t.Fatal("unavailable native fact not explicit")
				}
				stub := &sourceResolverStub{facts: testResolved(MomentCreated)}
				p, _ := NewProducer(stub)
				e, err := p.Produce(context.Background(), testAccess(), testRequest(d.Type))
				if !errors.Is(err, ErrUnavailable) || e != (Envelope{}) || stub.calls != 0 {
					t.Fatal("unsupported type reached fabricated resolver")
				}
				e = validEnvelope(MomentCreated)
				e.EventType = d.Type
				e.Source.Type = d.Source
				e.PayloadRef.Type = d.Source
				e.Purpose = d.Purpose
				e.EventID = StableEventID(e)
				if _, err = Encode(e, fixedNow()); !errors.Is(err, ErrUnavailable) {
					t.Fatal("wire fixture made unsupported source available")
				}
				data, _ := json.Marshal(e)
				if out, err := Decode(data, fixedNow()); !errors.Is(err, ErrUnavailable) || out != (Envelope{}) {
					t.Fatal("unsupported decoded payload escaped")
				}
				return
			}
			if d.Support != CurrentNativeSource || d.UnsupportedReason != "" {
				t.Fatal("support is not structural")
			}
			stub := &sourceResolverStub{facts: enrichmentResolved(d)}
			p, _ := NewProducer(stub)
			e, err := p.Produce(context.Background(), testAccess(), testRequest(d.Type))
			if err != nil {
				t.Fatal(err)
			}
			if e.ProcessingStatus != Unavailable {
				t.Fatal("ingress activated runtime")
			}
			wire, err := Encode(e, fixedNow())
			if err != nil {
				t.Fatal(err)
			}
			out, err := Decode(wire, fixedNow())
			if err != nil || out != e {
				t.Fatal("new metadata roundtrip failed", err)
			}
			for _, forbidden := range []string{"native", "private-state-fixture", "consent_epoch", "receipt_semantics", "requires_text", "source_support"} {
				if strings.Contains(string(wire), forbidden) {
					t.Fatal("new field/body changed old v1 wire")
				}
			}
			if err = p.Revalidate(context.Background(), testAccess(), e); err != nil {
				t.Fatal(err)
			}
			duplicate, err := p.Produce(context.Background(), testAccess(), testRequest(d.Type))
			if err != nil || duplicate.EventID != e.EventID || !duplicate.ExpiresAt.Equal(e.ExpiresAt) {
				t.Fatal("retry identity/lifetime changed")
			}
			stub.facts.ResolvedAt = stub.facts.OccurredAt.Add(MaxEventTTL)
			if current, err := p.Produce(context.Background(), testAccess(), testRequest(d.Type)); !errors.Is(err, ErrExpired) || current != (Envelope{}) {
				t.Fatal("current snapshot extended expired lifetime")
			}
		})
	}
	if len(seen) != 13 {
		t.Fatal("expected twelve enrichment events plus UserQuery")
	}
}

func TestEnrichmentRegisteredStateGuards(t *testing.T) {
	for _, d := range Catalog() {
		if d.Support != CurrentNativeSource || d.Type == UserQuery {
			continue
		}
		t.Run(string(d.Type), func(t *testing.T) {
			cases := []struct {
				name   string
				mutate func(*ResolvedSource)
			}{
				{"other_status", func(f *ResolvedSource) { f.NativeStatus = "pending" }},
				{"wrong_source", func(f *ResolvedSource) { f.Source.Type = QuerySource }},
				{"no_metadata", func(f *ResolvedSource) { f.MetadataBound = false }},
				{"other_owner", func(f *ResolvedSource) { f.Source.Owner.ID = otherID }},
				{"wrong_version", func(f *ResolvedSource) { f.Source.Version.Kind = NoNativeVersion }},
			}
			if d.RequiresText {
				cases = append(cases, struct {
					name   string
					mutate func(*ResolvedSource)
				}{"no_text", func(f *ResolvedSource) { f.TextPresent = false }})
			}
			if d.MinRevision > 1 {
				cases = append(cases, struct {
					name   string
					mutate func(*ResolvedSource)
				}{"no_prior_write", func(f *ResolvedSource) { f.Source.Version.Revision = 1 }})
			}
			for _, tc := range cases {
				t.Run(tc.name, func(t *testing.T) {
					f := enrichmentResolved(d)
					tc.mutate(&f)
					p, _ := NewProducer(&sourceResolverStub{facts: f})
					e, err := p.Produce(context.Background(), testAccess(), testRequest(d.Type))
					if !errors.Is(err, ErrDenied) || e != (Envelope{}) {
						t.Fatal("invalid current native state accepted", err)
					}
				})
			}
		})
	}
}

func TestEnrichmentSnapshotVersionNamespaceAndNativeClocks(t *testing.T) {
	stamp := fixedNow()
	content := []byte(`{"private":"real-row-canonical-representation"}`)
	first, err := SnapshotVersion(ParticipationSource, UpdatedAtDigestVersion, stamp, content)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name    string
		source  SourceType
		kind    VersionKind
		stamp   time.Time
		content []byte
	}{
		{"same", ParticipationSource, UpdatedAtDigestVersion, stamp, content},
		{"namespace", CommunityMembershipSource, UpdatedAtDigestVersion, stamp, content},
		{"created", SavedPlaceSource, CreatedAtDigestVersion, stamp, content},
		{"timestamp", ParticipationSource, UpdatedAtDigestVersion, stamp.Add(time.Microsecond), content},
		{"same_clock_content_change", ParticipationSource, UpdatedAtDigestVersion, stamp, []byte(`{"private":"modified"}`)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			v, err := SnapshotVersion(tc.source, tc.kind, tc.stamp, tc.content)
			if err != nil || !validVersion(v, tc.kind) {
				t.Fatal(err)
			}
			if (tc.name == "same") != (v == first) {
				t.Fatal("snapshot namespace/time/content binding wrong")
			}
		})
	}
	for _, tc := range []struct {
		name    string
		source  SourceType
		kind    VersionKind
		stamp   time.Time
		content []byte
	}{
		{"unknown", SourceType("UNKNOWN"), UpdatedAtDigestVersion, stamp, content},
		{"no_fake_saved_updated_at", SavedPlaceSource, UpdatedAtDigestVersion, stamp, content},
		{"no_fake_profile_created_at", ProfileSource, CreatedAtDigestVersion, stamp, content},
		{"missing_clock", ParticipationSource, UpdatedAtDigestVersion, time.Time{}, content},
		{"missing_content", ParticipationSource, UpdatedAtDigestVersion, stamp, nil},
		{"oversize", ParticipationSource, UpdatedAtDigestVersion, stamp, make([]byte, 1024*1024+1)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			v, err := SnapshotVersion(tc.source, tc.kind, tc.stamp, tc.content)
			if !errors.Is(err, ErrInvalid) || v != (SourceVersion{}) {
				t.Fatal("fake native version accepted")
			}
		})
	}
	old, _ := QueryVersion(stamp, content)
	if old == first {
		t.Fatal("legacy task namespace changed")
	}
}

func TestEnrichmentCreatedDigestStrictTaggedUnion(t *testing.T) {
	d, _ := Lookup(PlaceSaved)
	p, _ := NewProducer(&sourceResolverStub{facts: enrichmentResolved(d)})
	e, err := p.Produce(context.Background(), testAccess(), testRequest(PlaceSaved))
	if err != nil {
		t.Fatal(err)
	}
	wire, _ := Encode(e, fixedNow())
	for _, suffix := range []string{`,"revision":0`, `,"revision":null`, `,"body":"private"`, `,"token":"override"`} {
		t.Run(suffix, func(t *testing.T) {
			bad := strings.Replace(string(wire), `"kind":"CREATED_AT_DIGEST"`, `"kind":"CREATED_AT_DIGEST"`+suffix, 1)
			if v, err := Decode([]byte(bad), fixedNow()); !errors.Is(err, ErrInvalid) || v != (Envelope{}) {
				t.Fatal("opaque tagged union accepted extraneous variant")
			}
		})
	}
}
