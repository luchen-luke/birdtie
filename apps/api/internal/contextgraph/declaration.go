package contextgraph

import (
	"context"
	"errors"
	"strings"
)

var (
	ErrDeclaration = errors.New("invalid context declaration")
	ErrUnavailable = errors.New("context source unavailable")
	ErrNotFound    = errors.New("context declaration not found")
)

// Declaration is a private statement by a Person, not proof of residence,
// institution enrollment, Community membership, or content access.
type Declaration struct {
	ContextID  string `json:"contextId"`
	Type       Type   `json:"contextType"`
	SourceKey  string `json:"sourceKey"`
	Label      string `json:"label"`
	Relation   string `json:"relation"`
	Visibility string `json:"visibility"`
}

type DeclarationInput struct {
	Type      Type   `json:"contextType"`
	SourceKey string `json:"sourceKey"`
	Relation  string `json:"relation"`
}

type DeclarationStore interface {
	ListOwnContextDeclarations(context.Context, string) ([]Declaration, error)
	DeclareContext(context.Context, string, DeclarationInput) (Declaration, error)
	RemoveContextDeclaration(context.Context, string, string, string) error
}

func NormalizeDeclaration(input DeclarationInput) (DeclarationInput, error) {
	input.Type = Type(strings.ToUpper(strings.TrimSpace(string(input.Type))))
	input.SourceKey = strings.TrimSpace(input.SourceKey)
	input.Relation = strings.ToLower(strings.TrimSpace(input.Relation))
	if len([]rune(input.SourceKey)) == 0 || len([]rune(input.SourceKey)) > 160 ||
		strings.ContainsAny(input.SourceKey, "\r\n\x00") {
		return DeclarationInput{}, ErrDeclaration
	}
	switch input.Type {
	case City:
		if input.Relation != "current" && input.Relation != "past" &&
			input.Relation != "destination" && input.Relation != "home" {
			return DeclarationInput{}, ErrDeclaration
		}
	case Institution:
		if input.Relation != "past" && input.Relation != "affiliation" {
			return DeclarationInput{}, ErrDeclaration
		}
	case Online:
		if input.Relation != "interest" {
			return DeclarationInput{}, ErrDeclaration
		}
	default:
		return DeclarationInput{}, ErrDeclaration
	}
	return input, nil
}
