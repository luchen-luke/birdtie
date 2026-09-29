package saved

import (
	"context"
	"errors"
	"time"
)

var ErrNotFound = errors.New("saved item not found")

type Item struct {
	ID        string    `json:"id"`
	Kind      string    `json:"kind"`
	TargetID  string    `json:"targetId"`
	Title     string    `json:"title"`
	Summary   string    `json:"summary"`
	CityID    string    `json:"cityId"`
	Available bool      `json:"available"`
	SavedAt   time.Time `json:"savedAt"`
}

type Store interface {
	Save(context.Context, string, string, string) (string, error)
	ListSaved(context.Context, string) ([]Item, error)
	RemoveSaved(context.Context, string, string) error
}
