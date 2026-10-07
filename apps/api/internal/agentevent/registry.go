// Package agentevent defines metadata-only, server-controlled AIR ingress.
// The catalog is not a consumer, a source-purpose grant or a model permission.
package agentevent

import "time"

const SchemaVersion = "air.event.v1"
const MaxEnvelopeBytes = 16 * 1024
const MaxEventTTL = 15 * time.Minute

type Type string
type SourceType string
type VersionKind string
type Purpose string
type ProcessingStatus string
type SourceSupport string
type ReceiptSemantics string

const (
	MomentCreated             Type       = "MomentCreated"
	UserQuery                 Type       = "UserQuery"
	MomentUpdated             Type       = "MomentUpdated"
	MomentDeleted             Type       = "MomentDeleted"
	ActivityJoined            Type       = "ActivityJoined"
	ActivityLeft              Type       = "ActivityLeft"
	ActivityCompleted         Type       = "ActivityCompleted"
	PlaceSaved                Type       = "PlaceSaved"
	PlaceVisited              Type       = "PlaceVisited"
	CommunityJoined           Type       = "CommunityJoined"
	CommunityLeft             Type       = "CommunityLeft"
	ProfileUpdated            Type       = "ProfileUpdated"
	PreferenceUpdated         Type       = "PreferenceUpdated"
	MomentSource              SourceType = "MOMENT"
	QuerySource               SourceType = "AGENT_QUERY"
	ParticipationSource       SourceType = "ACTIVITY_PARTICIPATION"
	SavedPlaceSource          SourceType = "SAVED_PLACE"
	CommunityMembershipSource SourceType = "COMMUNITY_MEMBERSHIP"
	ProfileSource             SourceType = "USER_PROFILE"
	PrivatePreferenceSource   SourceType = "PRIVATE_PREFERENCE"
	// These two source kinds are reserved, not backed by native facts.
	CompletionSource       SourceType       = "ACTIVITY_COMPLETION"
	VisitSource            SourceType       = "PLACE_VISIT"
	RevisionVersion        VersionKind      = "REVISION"
	UpdatedAtDigestVersion VersionKind      = "UPDATED_AT_DIGEST"
	CreatedAtDigestVersion VersionKind      = "CREATED_AT_DIGEST"
	NoNativeVersion        VersionKind      = "UNAVAILABLE"
	MemoryCandidate        Purpose          = "memory_candidate"
	ActivityQuery          Purpose          = "activity_query"
	SourceInvalidation     Purpose          = "source_invalidation"
	Unavailable            ProcessingStatus = "UNAVAILABLE"
	CurrentNativeSource    SourceSupport    = "CURRENT_NATIVE_SOURCE"
	SourceUnavailable      SourceSupport    = "UNAVAILABLE"
	CurrentRetainedState   ReceiptSemantics = "CURRENT_RETAINED_STATE"
)

// Descriptor is a closed structural catalog, never authority. Semantics remain
// server-side catalog metadata: the old v1 envelope has no new required fields.
// A current retained membership/RSVP snapshot does not prove a historical join,
// attendance, the action's author, or the transition which changed updated_at.
type Descriptor struct {
	Type              Type
	Source            SourceType
	VersionKind       VersionKind
	Purpose           Purpose
	SchemaVersion     string
	Support           SourceSupport
	ReceiptSemantics  ReceiptSemantics
	NativeStatus      string
	RequiresText      bool
	MinRevision       int64
	UnsupportedReason string
}

func Lookup(kind Type) (Descriptor, bool) {
	d := Descriptor{Type: kind, SchemaVersion: SchemaVersion, Support: CurrentNativeSource, ReceiptSemantics: CurrentRetainedState, Purpose: MemoryCandidate}
	switch kind {
	case MomentCreated, MomentUpdated, MomentDeleted:
		d.Source, d.VersionKind, d.MinRevision = MomentSource, RevisionVersion, 1
		d.NativeStatus, d.RequiresText = "draft", true
		if kind != MomentCreated {
			d.MinRevision = 2
		}
		if kind == MomentDeleted {
			d.NativeStatus, d.RequiresText, d.Purpose = "withdrawn", false, SourceInvalidation
		}
	case UserQuery:
		d.Source, d.VersionKind, d.Purpose, d.RequiresText = QuerySource, UpdatedAtDigestVersion, ActivityQuery, true
	case ActivityJoined, ActivityLeft:
		d.Source, d.VersionKind, d.NativeStatus = ParticipationSource, UpdatedAtDigestVersion, "going"
		if kind == ActivityLeft {
			d.NativeStatus, d.Purpose = "cancelled", SourceInvalidation
		}
	case PlaceSaved:
		d.Source, d.VersionKind, d.NativeStatus = SavedPlaceSource, CreatedAtDigestVersion, "saved"
	case CommunityJoined, CommunityLeft:
		d.Source, d.VersionKind, d.NativeStatus = CommunityMembershipSource, UpdatedAtDigestVersion, "active"
		if kind == CommunityLeft {
			d.NativeStatus, d.Purpose = "left", SourceInvalidation
		}
	case ProfileUpdated:
		d.Source, d.VersionKind, d.NativeStatus = ProfileSource, UpdatedAtDigestVersion, "profile_current"
	case PreferenceUpdated:
		d.Source, d.VersionKind, d.NativeStatus, d.MinRevision = PrivatePreferenceSource, RevisionVersion, "explicit_preferences", 2
	case ActivityCompleted, PlaceVisited:
		d.Support, d.VersionKind = SourceUnavailable, NoNativeVersion
		if kind == ActivityCompleted {
			d.Source, d.UnsupportedReason = CompletionSource, "NO_NATIVE_ATTENDANCE_OR_COMPLETION_FACT"
		} else {
			d.Source, d.UnsupportedReason = VisitSource, "NO_NATIVE_EXPLICIT_VISIT_FACT"
		}
	default:
		return Descriptor{}, false
	}
	return d, true
}

func Catalog() []Descriptor {
	// Preserve the initial AIR014 order and append the AGE064 enrichment types.
	kinds := []Type{MomentCreated, UserQuery, MomentUpdated, MomentDeleted, ActivityJoined, ActivityLeft, ActivityCompleted, PlaceSaved, PlaceVisited, CommunityJoined, CommunityLeft, ProfileUpdated, PreferenceUpdated}
	out := make([]Descriptor, 0, len(kinds))
	for _, kind := range kinds {
		d, _ := Lookup(kind)
		out = append(out, d)
	}
	return out
}
