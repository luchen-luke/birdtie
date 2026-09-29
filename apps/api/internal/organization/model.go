package organization

import (
	"context"
	"errors"
)

var ErrForbidden = errors.New("organization workspace forbidden")

type Organization struct {
	ID               string `json:"id"`
	AccountID        string `json:"accountId"`
	OrganizationType string `json:"organizationType"`
	Name             string `json:"name"`
	Role             string `json:"role"`
}

type CreateInput struct {
	OrganizationType string `json:"organizationType"`
	Name             string `json:"name"`
}

type Store interface {
	CreateOrganization(context.Context, string, CreateInput) (Organization, error)
	ListOrganizations(context.Context, string) ([]Organization, error)
	ResolveWorkspace(context.Context, string, string) (string, string, error)
}
