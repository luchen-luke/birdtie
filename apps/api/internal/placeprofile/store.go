package placeprofile

import "context"

// Access is assembled by Authenticate in the server, never request JSON.
// A live city editor role is rechecked by the native Store; this is not a grant.
type Access struct {
	SessionDigest [32]byte `json:"-"`
	ActorID       string   `json:"-"`
	AccountType   string   `json:"-"`
}

func ValidateAccess(a Access) error {
	if !ValidID(a.ActorID) || a.SessionDigest == ([32]byte{}) || (a.AccountType != "person" && a.AccountType != "organization" && a.AccountType != "business") {
		return ErrDenied
	}
	return nil
}

type Revision struct {
	PlaceID string `json:"placeId"`
	Version int64  `json:"version"`
	State   string `json:"state"`
}
type Store interface {
	SubmitPlaceSemanticCandidate(context.Context, Access, string, string, SubmitInput) (Candidate, bool, error)
	ListPlaceSemanticCandidates(context.Context, Access, string) ([]Candidate, error)
	ReviewPlaceSemanticCandidate(context.Context, Access, string, ReviewInput) (Candidate, error)
	WithdrawPlaceSemanticProfile(context.Context, Access, string, WithdrawInput) (Revision, error)
	GetPublicPlaceSemanticProfile(context.Context, string) (Public, error)
}
