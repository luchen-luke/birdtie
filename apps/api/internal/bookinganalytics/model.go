// Package bookinganalytics describes untrusted external-opening telemetry.
// It cannot assert that an external supplier accepted a booking.
package bookinganalytics

import (
	"context"
	"errors"
	"github.com/birdtie/birdtie/apps/api/internal/venue"
	"regexp"
	"time"
)

const EventType = "EXTERNAL_BOOKING_CLICK"
const Outcome = "CLIENT_REPORTED_EXTERNAL_OPEN"
const Schema = "booking-external-event-v1"

var ErrInvalid = errors.New("invalid booking telemetry")
var uuid = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)
var version = regexp.MustCompile(`^[0-9a-f]{64}$`)

type Input struct {
	EventID       string    `json:"eventId"`
	EventType     string    `json:"eventType"`
	Outcome       string    `json:"outcome"`
	SourceVersion string    `json:"sourceVersion"`
	ValidUntil    time.Time `json:"validUntil"`
}

func (in Input) Valid(placeID string) bool {
	return uuid.MatchString(placeID) && uuid.MatchString(in.EventID) && in.EventType == EventType && in.Outcome == Outcome && version.MatchString(in.SourceVersion) && !in.ValidUntil.IsZero()
}

type Result struct {
	SchemaVersion       string    `json:"schemaVersion"`
	EventID             string    `json:"eventId"`
	PlaceID             string    `json:"placeId"`
	EventType           string    `json:"eventType"`
	Outcome             string    `json:"outcome"`
	RecordedAt          time.Time `json:"recordedAt"`
	ConfirmedCapability string    `json:"confirmedCapability"`
	ConfirmedBooking    string    `json:"confirmedBooking"`
}

func Report(id, place string, at time.Time) Result {
	return Result{Schema, id, place, EventType, Outcome, at.UTC(), "UNAVAILABLE", "UNKNOWN"}
}

type Store interface {
	RecordExternalBookingEvent(context.Context, venue.PublicAccess, string, Input) (Result, error)
}
