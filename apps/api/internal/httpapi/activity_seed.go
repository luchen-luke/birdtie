package httpapi

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/cityseed"
)

// These legacy routes accept only a Person's explicit city-editor command.
// A workspace header or query selector cannot replace the authenticated actor.
func (s *server) cityActivityActor(w http.ResponseWriter, r *http.Request) (string, bool) {
	a, _, err := s.actor(r, true)
	if authFailed(w, err) {
		return "", false
	}
	if a.AccountType != "person" || len(r.Header.Values("X-Birdtie-Organization-Workspace")) != 0 {
		respondError(w, http.StatusForbidden, "person_account_required")
		return "", false
	}
	if r.URL.RawQuery != "" {
		respondError(w, http.StatusBadRequest, "invalid_query")
		return "", false
	}
	if s.seed == nil {
		respondError(w, http.StatusServiceUnavailable, "city_activity_unavailable")
		return "", false
	}
	return a.ID, true
}

func cityActivityOrganizerError(w http.ResponseWriter, err error) bool {
	var code, message string
	status := http.StatusConflict
	switch {
	case errors.Is(err, cityseed.ErrOrganizerRequired):
		code, message = "organizer_required", "请由原提交者明确选择有权管理的主办方后重新提交此线索。"
	case errors.Is(err, cityseed.ErrOrganizerUnavailable):
		code, message = "organizer_unavailable", "主办方当前不可用或管理权限已变更，请原提交者重新检查后提交。"
	case errors.Is(err, cityseed.ErrInvalidOrganizer):
		status, code, message = http.StatusBadRequest, "invalid_organizer", "主办方信息不完整或格式不正确。"
	default:
		return false
	}
	respond(w, status, map[string]any{"error": map[string]string{"code": code, "message": message}})
	return true
}

// Keep duplicate/key-case enforcement local to the candidate routes. The
// standard legacy decoder's last-key-wins behavior is unsafe for selectors.
func decodeCityActivityJSON(w http.ResponseWriter, r *http.Request, output any, review bool) bool {
	media, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || media != "application/json" {
		respondError(w, http.StatusUnsupportedMediaType, "json_required")
		return false
	}
	raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 8192))
	if err != nil || !cityActivityJSONKeys(raw, review) {
		respondError(w, http.StatusBadRequest, "invalid_body")
		return false
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err = decoder.Decode(output); err != nil {
		respondError(w, http.StatusBadRequest, "invalid_body")
		return false
	}
	return true
}

func cityActivityJSONKeys(raw []byte, review bool) bool {
	allowed := map[string]bool{"placeId": true, "title": true, "summary": true, "hostLabel": true, "startsAt": true, "endsAt": true, "timeZone": true, "sourceLabel": true, "sourceUrl": true, "rightsNote": true, "expiresAt": true, "organizer": true}
	if review {
		allowed = map[string]bool{"decision": true, "note": true}
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	var object func(map[string]bool) bool
	object = func(keys map[string]bool) bool {
		token, err := d.Token()
		if err != nil || token != json.Delim('{') {
			return false
		}
		seen := map[string]bool{}
		for d.More() {
			token, err = d.Token()
			key, ok := token.(string)
			if err != nil || !ok || !keys[key] || seen[key] {
				return false
			}
			seen[key] = true
			if key == "organizer" {
				var value json.RawMessage
				if d.Decode(&value) != nil {
					return false
				}
				if bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
					continue // legacy unknown-organizer pending submission only
				}
				nested := json.NewDecoder(bytes.NewReader(value))
				nestedToken, e := nested.Token()
				if e != nil || nestedToken != json.Delim('{') {
					return false
				}
				nestedSeen := map[string]bool{}
				for nested.More() {
					nestedToken, e = nested.Token()
					nk, valid := nestedToken.(string)
					if e != nil || !valid || (nk != "type" && nk != "id") || nestedSeen[nk] {
						return false
					}
					nestedSeen[nk] = true
					var text string
					var field json.RawMessage
					if nested.Decode(&field) != nil || bytes.Equal(bytes.TrimSpace(field), []byte("null")) || json.Unmarshal(field, &text) != nil {
						return false
					}
				}
				end, e := nested.Token()
				if e != nil || end != json.Delim('}') || !nestedSeen["type"] || !nestedSeen["id"] {
					return false
				}
				if _, e := nested.Token(); e != io.EOF {
					return false
				}
				continue
			}
			var scalar json.RawMessage
			if d.Decode(&scalar) != nil || bytes.Equal(bytes.TrimSpace(scalar), []byte("null")) {
				return false
			}
		}
		token, err = d.Token()
		return err == nil && token == json.Delim('}')
	}
	if !object(allowed) {
		return false
	}
	_, err := d.Token()
	return err == io.EOF
}

func validActivityCandidate(input *cityseed.ActivityInput) bool {
	input.Title = strings.TrimSpace(input.Title)
	input.Summary = strings.TrimSpace(input.Summary)
	input.HostLabel = strings.TrimSpace(input.HostLabel)
	input.TimeZone = strings.TrimSpace(input.TimeZone)
	input.SourceLabel = strings.TrimSpace(input.SourceLabel)
	input.SourceURL = strings.TrimSpace(input.SourceURL)
	input.RightsNote = strings.TrimSpace(input.RightsNote)
	if input.PlaceID != "" && !uuidPath.MatchString(input.PlaceID) {
		return false
	}
	source, err := url.Parse(input.SourceURL)
	if err != nil || source.Scheme != "https" || source.Host == "" || source.User != nil {
		return false
	}
	if _, err := time.LoadLocation(input.TimeZone); err != nil {
		return false
	}
	now := time.Now()
	return len(input.Title) >= 1 && len(input.Title) <= 160 &&
		len(input.Summary) <= 3000 && len(input.HostLabel) >= 1 &&
		len(input.HostLabel) <= 160 && len(input.TimeZone) <= 80 &&
		len(input.SourceLabel) >= 1 && len(input.SourceLabel) <= 120 &&
		len(input.SourceURL) <= 1000 && len(input.RightsNote) >= 10 &&
		len(input.RightsNote) <= 1000 && !input.StartsAt.IsZero() &&
		input.EndsAt.After(input.StartsAt) &&
		input.EndsAt.Sub(input.StartsAt) <= 31*24*time.Hour &&
		input.ExpiresAt.After(now.Add(time.Hour)) &&
		input.ExpiresAt.Before(now.Add(365*24*time.Hour))
}

func (s *server) submitActivityCandidate(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.cityActivityActor(w, r)
	if !ok {
		return
	}
	cityID := r.PathValue("cityID")
	if cityID == "" || len(cityID) > 80 {
		respondError(w, http.StatusBadRequest, "invalid_city_id")
		return
	}
	var input cityseed.ActivityInput
	if !decodeCityActivityJSON(w, r, &input, false) {
		return
	}
	if !validActivityCandidate(&input) {
		respondError(w, http.StatusBadRequest, "invalid_activity_candidate")
		return
	}
	organizer, err := cityseed.NormalizeActivityOrganizer(input.Organizer)
	if err != nil {
		cityActivityOrganizerError(w, err)
		return
	}
	input.Organizer = organizer
	if organizer != nil && organizer.Type == "PERSON" && organizer.ID != actor {
		respondError(w, http.StatusForbidden, "organizer_permission_required")
		return
	}
	candidate, err := s.seed.SubmitActivity(r.Context(), actor, cityID, input)
	if cityActivityOrganizerError(w, err) {
		return
	}
	switch {
	case errors.Is(err, cityseed.ErrForbidden):
		respondError(w, http.StatusForbidden, "city_editor_required")
	case errors.Is(err, cityseed.ErrConflict):
		respondError(w, http.StatusConflict, "place_unavailable")
	case err != nil:
		serverError(w, err)
	default:
		respond(w, http.StatusCreated, map[string]any{"data": candidate})
	}
}

func (s *server) listActivityCandidates(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.cityActivityActor(w, r)
	if !ok {
		return
	}
	cityID := r.PathValue("cityID")
	if cityID == "" || len(cityID) > 80 {
		respondError(w, http.StatusBadRequest, "invalid_city_id")
		return
	}
	candidates, err := s.seed.ListActivityCandidates(
		r.Context(), actor, cityID)
	if errors.Is(err, cityseed.ErrForbidden) {
		respondError(w, http.StatusForbidden, "city_editor_required")
	} else if err != nil {
		serverError(w, err)
	} else {
		respond(w, http.StatusOK, map[string]any{"data": candidates})
	}
}

func (s *server) reviewActivityCandidate(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.cityActivityActor(w, r)
	if !ok {
		return
	}
	candidateID := r.PathValue("candidateID")
	if !uuidPath.MatchString(candidateID) {
		respondError(w, http.StatusBadRequest, "invalid_candidate_id")
		return
	}
	var input cityseed.ActivityReviewInput
	if !decodeCityActivityJSON(w, r, &input, true) {
		return
	}
	input.Note = strings.TrimSpace(input.Note)
	if len(input.Note) < 10 || len(input.Note) > 1000 ||
		(input.Decision != "publish" && input.Decision != "reject") {
		respondError(w, http.StatusBadRequest, "invalid_review")
		return
	}
	candidate, err := s.seed.ReviewActivity(r.Context(), actor, candidateID, input)
	if cityActivityOrganizerError(w, err) {
		return
	}
	switch {
	case errors.Is(err, cityseed.ErrForbidden):
		respondError(w, http.StatusForbidden, "reviewer_required")
	case errors.Is(err, cityseed.ErrConflict):
		respondError(w, http.StatusConflict, "review_conflict")
	case err != nil:
		serverError(w, err)
	default:
		respond(w, http.StatusOK, map[string]any{"data": candidate})
	}
}
