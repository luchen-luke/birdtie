package cityseed

import (
	"context"
	"errors"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/actorref"
)

var (
	ErrOrganizerRequired    = errors.New("请重新提交活动并明确选择有权代表的真实主办方")
	ErrOrganizerUnavailable = errors.New("主办方不可公开或代表权限已失效")
	ErrInvalidOrganizer     = errors.New("主办方类型或编号无效")
)

// A selector is not organizer authorization. Missing selectors keep historical
// candidates reviewable but cannot authorize publication.
func NormalizeActivityOrganizer(input *actorref.ActorRef) (*actorref.ActorRef, error) {
	if input == nil {
		return nil, nil
	}
	ref, err := actorref.Parse(string(input.Type), input.ID)
	if err != nil || ref.ID == "00000000-0000-0000-0000-000000000000" {
		return nil, ErrInvalidOrganizer
	}
	return &ref, nil
}

type ActivityInput struct {
	Organizer   *actorref.ActorRef `json:"organizer,omitempty"`
	PlaceID     string             `json:"placeId"`
	Title       string             `json:"title"`
	Summary     string             `json:"summary"`
	HostLabel   string             `json:"hostLabel"`
	StartsAt    time.Time          `json:"startsAt"`
	EndsAt      time.Time          `json:"endsAt"`
	TimeZone    string             `json:"timeZone"`
	SourceLabel string             `json:"sourceLabel"`
	SourceURL   string             `json:"sourceUrl"`
	RightsNote  string             `json:"rightsNote"`
	ExpiresAt   time.Time          `json:"expiresAt"`
}

type ActivityCandidate struct {
	Organizer          *actorref.ActorRef `json:"organizer,omitempty"`
	ID                 string             `json:"id"`
	CityID             string             `json:"cityId"`
	SubmittedBy        string             `json:"submittedBy"`
	PlaceID            string             `json:"placeId,omitempty"`
	Title              string             `json:"title"`
	Summary            string             `json:"summary"`
	HostLabel          string             `json:"hostLabel"`
	StartsAt           time.Time          `json:"startsAt"`
	EndsAt             time.Time          `json:"endsAt"`
	TimeZone           string             `json:"timeZone"`
	SourceLabel        string             `json:"sourceLabel"`
	SourceURL          string             `json:"sourceUrl"`
	RightsNote         string             `json:"rightsNote"`
	ExpiresAt          time.Time          `json:"expiresAt"`
	Status             string             `json:"status"`
	ReviewedBy         string             `json:"reviewedBy,omitempty"`
	ReviewedAt         *time.Time         `json:"reviewedAt,omitempty"`
	ReviewNote         string             `json:"reviewNote,omitempty"`
	ResolvedActivityID string             `json:"resolvedActivityId,omitempty"`
	CreatedAt          time.Time          `json:"createdAt"`
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
