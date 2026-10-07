package agentbusiness

import (
	"context"
	"encoding/json"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/actorref"
	"github.com/birdtie/birdtie/apps/api/internal/agentprofile"
	"github.com/birdtie/birdtie/apps/api/internal/businessconsole"
)

const IdentitySchema = "business-agent-identity-v1"

// EstablishIdentity creates metadata only. Its version is the current native
// merchant claim, not a model permission or an Agent Profile version.
type EstablishIdentity struct {
	ExpectedClaimVersion int64 `json:"expectedClaimVersion"`
}

func DecodeEstablishIdentity(raw []byte) (EstablishIdentity, error) {
	var v EstablishIdentity
	if len(raw) > 4096 {
		return v, businessconsole.ErrInvalid
	}
	n, e := businessconsole.StrictObject(raw, "expectedClaimVersion")
	if e != nil || json.Unmarshal(n, &v) != nil || v.ExpectedClaimVersion < 1 {
		return EstablishIdentity{}, businessconsole.ErrInvalid
	}
	return v, nil
}

type BusinessIdentityAgent struct {
	ID      string              `json:"id"`
	Type    actorref.Type       `json:"type"`
	Status  string              `json:"status"`
	Profile agentprofile.Record `json:"profile"`
}

// No knowledge, private data or capabilities are included in this response.
type BusinessIdentity struct {
	Schema           string                 `json:"schema"`
	BusinessID       string                 `json:"businessId"`
	BusinessName     string                 `json:"businessName"`
	Principal        actorref.PrincipalRef  `json:"principal"`
	Role             string                 `json:"role"`
	ClaimStatus      string                 `json:"claimStatus"`
	ClaimState       string                 `json:"claimState"`
	ClaimVersion     int64                  `json:"claimVersion"`
	Agent            *BusinessIdentityAgent `json:"agent"`
	ObservedAt       time.Time              `json:"observedAt"`
	ValidUntil       time.Time              `json:"validUntil"`
	MetadataOnly     bool                   `json:"metadataOnly"`
	RuntimeAvailable bool                   `json:"runtimeAvailable"`
	Tools            []string               `json:"tools"`
	// SourceVersion is a native current-row binding, never client authority.
	SourceVersion string `json:"-"`
}

func ValidateBusinessIdentity(v BusinessIdentity) error {
	state := func(s string) bool { return s == "pending" || s == "verified" || s == "rejected" || s == "revoked" }
	if v.Schema != IdentitySchema || !businessconsole.ValidID(v.BusinessID) || len(v.BusinessName) == 0 || v.Principal.Type != actorref.Business || !businessconsole.ValidID(v.Principal.ID) || (v.Role != "owner" && v.Role != "admin") || !state(v.ClaimStatus) || !state(v.ClaimState) || v.ClaimVersion < 0 || !finite(v.ObservedAt) || !finite(v.ValidUntil) || !v.ValidUntil.After(v.ObservedAt) || v.ValidUntil.Sub(v.ObservedAt) > 30*time.Second || !v.MetadataOnly || v.RuntimeAvailable || v.Tools == nil || len(v.Tools) != 0 || !ValidVersion(v.SourceVersion) {
		return businessconsole.ErrUnavailable
	}
	if v.Agent != nil {
		a := v.Agent
		if !businessconsole.ValidID(a.ID) || a.Type != actorref.Business || (a.Status != "suspended" && a.Status != "retired") || agentprofile.Validate(a.Profile) != nil || a.Profile.AgentID != a.ID || a.Profile.OwnerType != actorref.Business || a.Profile.OwnerID != v.Principal.ID || a.Profile.UpdatedAt.After(v.ObservedAt) {
			return businessconsole.ErrUnavailable
		}
	}
	return nil
}

type IdentityStore interface {
	ReadBusinessAgentIdentity(context.Context, businessconsole.Access) (BusinessIdentity, error)
	EstablishBusinessAgentIdentity(context.Context, businessconsole.Access, EstablishIdentity) (BusinessIdentity, error)
}
