package organization

import "time"

type MapProposal struct {
	ID                string  `json:"id"`
	OrganizationID    string  `json:"organizationId"`
	CityID            string  `json:"cityId"`
	Latitude          float64 `json:"latitude"`
	Longitude         float64 `json:"longitude"`
	Status            string  `json:"status"`
	Version           int64   `json:"version"`
	CanonicalRevision int64   `json:"canonicalRevision"`
	SubmittedBy       string  `json:"submittedBy"`
	CanApprove        bool    `json:"canApprove"`
	CanReject         bool    `json:"canReject"`
	CanWithdraw       bool    `json:"canWithdraw"`
}
type MapControl struct {
	OrganizationID   string        `json:"organizationId"`
	AuthorityVersion int64         `json:"authorityVersion"`
	Role             string        `json:"role"`
	Canonical        *MapLocation  `json:"canonical"`
	Proposals        []MapProposal `json:"proposals"`
	ObservedAt       time.Time     `json:"observedAt"`
	ExpiresAt        time.Time     `json:"expiresAt"`
	Proof            string        `json:"-"`
}
