package content

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"io"
	"strings"
	"time"
	"unicode/utf8"
)

const JourneyDetailsSchema = "birdtie.journey-details.v1"
const MaxJourneyEntries = 500

// JourneyDetails is the editable schedule of the existing Journey identity.
// The old Place-only entries projection is empty for this version.
type JourneyDetails struct {
	SchemaVersion string         `json:"schemaVersion"`
	SourceVersion string         `json:"sourceVersion,omitempty"` // authoritative owner read only
	StartDate     string         `json:"startDate"`
	EndDate       string         `json:"endDate"`
	DateLabel     string         `json:"dateLabel"`
	Days          int            `json:"days"`
	Itinerary     []JourneyEntry `json:"itinerary"`
}

// Origin is an immutable source reference. It is returned only to the owner;
// inputs omit it. Source stop links remain visible even when no current City
// or Place can be proven. They never grant publication or location authority.
type JourneyEntryOrigin struct {
	SourceID            string   `json:"sourceId"`
	SourceType          string   `json:"sourceType"`
	SourcePosition      int64    `json:"sourcePosition"`
	SourcePlaceID       string   `json:"sourcePlaceId"`
	SourceCityID        string   `json:"sourceCityId"`
	SourceDestinationID string   `json:"sourceDestinationId"`
	SourceStopID        string   `json:"sourceStopId"`
	SourceFromStopID    string   `json:"sourceFromStopId"`
	SourceToStopID      string   `json:"sourceToStopId"`
	SourceConfirmed     *bool    `json:"sourceConfirmed,omitempty"`
	SourceConfidence    *float64 `json:"sourceConfidence,omitempty"`
}

type JourneyEntry struct {
	ID            string              `json:"id"`
	EntryType     string              `json:"entryType"` // STOP, ARRANGEMENT, NOTE
	Position      int                 `json:"position"`  // current array order, 1-based
	Title         string              `json:"title"`
	Note          string              `json:"note"`
	CityID        string              `json:"cityId"`
	PlaceID       string              `json:"placeId"`
	Kind          string              `json:"kind"`
	DayIndex      *int                `json:"dayIndex"`
	StartDay      *int                `json:"startDay"`
	EndDay        *int                `json:"endDay"`
	StartDate     string              `json:"startDate"`
	EndDate       string              `json:"endDate"`
	StartTime     string              `json:"startTime"`
	EndTime       string              `json:"endTime"`
	TimeLabel     string              `json:"timeLabel"`
	StopID        string              `json:"stopId"`
	FromStopID    string              `json:"fromStopId"`
	ToStopID      string              `json:"toStopId"`
	TransportMode string              `json:"transportMode"`
	ServiceNumber string              `json:"serviceNumber"`
	DepartureName string              `json:"departureName"`
	ArrivalName   string              `json:"arrivalName"`
	CustomAddress string              `json:"customAddress"`
	SourceLabel   string              `json:"sourceLabel"`
	SourceText    string              `json:"sourceText"`
	Origin        *JourneyEntryOrigin `json:"origin,omitempty"`
}

func journeyDate(s string) bool {
	if s == "" {
		return true
	}
	t, e := time.Parse("2006-01-02", s)
	return e == nil && t.Format("2006-01-02") == s
}
func journeyClock(s string) bool {
	if s == "" {
		return true
	}
	for _, layout := range []string{"15:04", "15:04:05"} {
		if t, e := time.Parse(layout, s); e == nil && t.Format(layout) == s {
			return true
		}
	}
	return false
}
func NormalizeJourneyDetails(v JourneyDetails) (JourneyDetails, error) {
	if v.SchemaVersion != JourneyDetailsSchema || v.SourceVersion != "" || !journeyDate(v.StartDate) || !journeyDate(v.EndDate) || v.StartDate != "" && v.EndDate != "" && v.EndDate < v.StartDate || v.Days < 0 || v.Days > 36600 || len(v.Itinerary) > MaxJourneyEntries {
		return JourneyDetails{}, ErrInvalid
	}
	if !journeyText(v.DateLabel, 1000) {
		return JourneyDetails{}, ErrInvalid
	}
	v.Itinerary = append([]JourneyEntry{}, v.Itinerary...)
	ids := map[string]string{}
	for i, e := range v.Itinerary {
		if !ValidCollectionID(e.ID) || ids[e.ID] != "" || e.EntryType != "STOP" && e.EntryType != "ARRANGEMENT" && e.EntryType != "NOTE" || e.Origin != nil || len(e.CityID) > 80 || e.PlaceID != "" && !ValidCollectionID(e.PlaceID) {
			return JourneyDetails{}, ErrInvalid
		}
		for _, s := range []string{e.Title, e.Note, e.CityID, e.Kind, e.TimeLabel, e.TransportMode, e.ServiceNumber, e.DepartureName, e.ArrivalName, e.CustomAddress, e.SourceLabel, e.SourceText} {
			if !journeyText(s, 16000) {
				return JourneyDetails{}, ErrInvalid
			}
		}
		if len(e.Title) > 1000 || len(e.CityID) > 80 || len(e.Kind) > 80 || !journeyDate(e.StartDate) || !journeyDate(e.EndDate) || e.StartDate != "" && e.EndDate != "" && e.EndDate < e.StartDate || !journeyClock(e.StartTime) || !journeyClock(e.EndTime) {
			return JourneyDetails{}, ErrInvalid
		}
		for _, n := range []*int{e.DayIndex, e.StartDay, e.EndDay} {
			if n != nil && (*n < 0 || *n > 36600) {
				return JourneyDetails{}, ErrInvalid
			}
		}
		if e.StartDay != nil && e.EndDay != nil && *e.EndDay < *e.StartDay {
			return JourneyDetails{}, ErrInvalid
		}
		for _, link := range []string{e.StopID, e.FromStopID, e.ToStopID} {
			if link != "" && (!ValidCollectionID(link) || link == e.ID) {
				return JourneyDetails{}, ErrInvalid
			}
		}
		if e.EntryType != "ARRANGEMENT" && (e.StopID != "" || e.FromStopID != "" || e.ToStopID != "") {
			return JourneyDetails{}, ErrInvalid
		}
		e.Position = i + 1
		v.Itinerary[i], ids[e.ID] = e, e.EntryType
	}
	for _, e := range v.Itinerary {
		for _, link := range []string{e.StopID, e.FromStopID, e.ToStopID} {
			if link != "" && ids[link] != "STOP" {
				return JourneyDetails{}, ErrInvalid
			}
		}
	}
	return v, nil
}
func journeyText(s string, max int) bool {
	return utf8.ValidString(s) && !strings.ContainsRune(s, 0) && len(s) <= max
}

// The new material is appended only when present. Existing 147 payload hashes
// and immutable original receipts retain their exact previous digest.
func JourneyDetailsDigest(v *JourneyDetails) string {
	if v == nil {
		return ""
	}
	b64 := func(s string) string { return base64.RawURLEncoding.EncodeToString([]byte(s)) }
	rows := []any{}
	for _, e := range v.Itinerary {
		rows = append(rows, []any{e.ID, e.EntryType, e.Position, b64(e.Title), b64(e.Note), b64(e.CityID), e.PlaceID, b64(e.Kind), e.DayIndex, e.StartDay, e.EndDay, b64(e.StartDate), b64(e.EndDate), b64(e.StartTime), b64(e.EndTime), b64(e.TimeLabel), e.StopID, e.FromStopID, e.ToStopID, b64(e.TransportMode), b64(e.ServiceNumber), b64(e.DepartureName), b64(e.ArrivalName), b64(e.CustomAddress), b64(e.SourceLabel), b64(e.SourceText)})
	}
	return MomentControlHash([]any{JourneyDetailsSchema, b64(v.StartDate), b64(v.EndDate), b64(v.DateLabel), v.Days, rows})
}

func (v *JourneyDetails) UnmarshalJSON(raw []byte) error {
	if !utf8.Valid(raw) || !collectionControlEscapes(raw) {
		return ErrInvalid
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	var check func() error
	check = func() error {
		t, e := d.Token()
		if e != nil {
			return ErrInvalid
		}
		if x, ok := t.(json.Delim); ok {
			if x == '{' {
				seen := map[string]bool{}
				for d.More() {
					k, e := d.Token()
					s, ok := k.(string)
					if e != nil || !ok || seen[s] {
						return ErrInvalid
					}
					seen[s] = true
					if check() != nil {
						return ErrInvalid
					}
				}
				t, e = d.Token()
				if e != nil || t != json.Delim('}') {
					return ErrInvalid
				}
			} else if x == '[' {
				for d.More() {
					if check() != nil {
						return ErrInvalid
					}
				}
				t, e = d.Token()
				if e != nil || t != json.Delim(']') {
					return ErrInvalid
				}
			} else {
				return ErrInvalid
			}
		}
		return nil
	}
	if check() != nil {
		return ErrInvalid
	}
	if _, e := d.Token(); e != io.EOF {
		return ErrInvalid
	}
	type plain JourneyDetails
	var p plain
	d = json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if d.Decode(&p) != nil {
		return ErrInvalid
	}
	*v = JourneyDetails(p)
	return nil
}
