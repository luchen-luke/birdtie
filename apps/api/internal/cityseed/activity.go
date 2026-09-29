package cityseed

import (
	"context"
	"time"
)

type ActivityInput struct {
	PlaceID     string    `json:"placeId"`
	Title       string    `json:"title"`
	Summary     string    `json:"summary"`
	HostLabel   string    `json:"hostLabel"`
	StartsAt    time.Time `json:"startsAt"`
	EndsAt      time.Time `json:"endsAt"`
	TimeZone    string    `json:"timeZone"`
	SourceLabel string    `json:"sourceLabel"`
	SourceURL   string    `json:"sourceUrl"`
	RightsNote  string    `json:"rightsNote"`
	ExpiresAt   time.Time `json:"expiresAt"`
}

type ActivityCandidate struct {
	ID                 string     `json:"id"`
	CityID             string     `json:"cityId"`
	SubmittedBy        string     `json:"submittedBy"`
	PlaceID            string     `json:"placeId,omitempty"`
	Title              string     `json:"title"`
	Summary            string     `json:"summary"`
	HostLabel          string     `json:"hostLabel"`
	StartsAt           time.Time  `json:"startsAt"`
	EndsAt             time.Time  `json:"endsAt"`
	TimeZone           string     `json:"timeZone"`
	SourceLabel        string     `json:"sourceLabel"`
	SourceURL          string     `json:"sourceUrl"`
	RightsNote         string     `json:"rightsNote"`
	ExpiresAt          time.Time  `json:"expiresAt"`
	Status             string     `json:"status"`
	ReviewedBy         string     `json:"reviewedBy,omitempty"`
	ReviewedAt         *time.Time `json:"reviewedAt,omitempty"`
	ReviewNote         string     `json:"reviewNote,omitempty"`
	ResolvedActivityID string     `json:"resolvedActivityId,omitempty"`
	CreatedAt          time.Time  `json:"createdAt"`
}

type ActivityReviewInput struct {
	Decision string `json:"decision"`
	Note     string `json:"note"`
}

type ActivityStore interface {
	SubmitActivity(context.Context, string, string, ActivityInput) (ActivityCandidate, error)
	ListActivityCandidates(context.Context, string, string) ([]ActivityCandidate, error)
	ReviewActivity(context.Context, string, string, ActivityReviewInput) (ActivityCandidate, error)
}
