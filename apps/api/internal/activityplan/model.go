package activityplan

import (
	"context"
	"errors"
	"time"
)

var ErrNotFound = errors.New("activity plan not found")

type Plan struct {
	ID         string     `json:"id"`
	ActivityID string     `json:"activityId"`
	Title      string     `json:"title"`
	CityID     string     `json:"cityId"`
	StartsAt   *time.Time `json:"startsAt,omitempty"`
	EndsAt     *time.Time `json:"endsAt,omitempty"`
	Status     string     `json:"status"`
	Available  bool       `json:"available"`
	CreatedAt  time.Time  `json:"createdAt"`
}

type Store interface {
	PlanActivity(context.Context, string, string) (string, error)
	ListActivityPlans(context.Context, string) ([]Plan, error)
	RemoveActivityPlan(context.Context, string, string) error
}
