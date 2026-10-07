package agentworkspace

import (
	"encoding/json"
	"github.com/birdtie/birdtie/apps/api/internal/actorref"
	arp "github.com/birdtie/birdtie/apps/api/internal/agentresultprojection"
	"math"
	"strconv"
	"strings"
	"time"
)

// WithContract projects the same verified entities returned by the legacy
// arrays into the versioned ResultSet shape. No IDs or map pins are inferred
// from the user's words.
func WithContract(result Results, task Task, requestID string) Results {
	// A person can appear on the map only after explicitly choosing a broad
	// public zone. Discard coordinates from any store response lacking that
	// choice before the response is serialized, not just from map effects.
	for i := range result.People {
		person := &result.People[i]
		if !publicPersonMapZone(person.PublicMapZone) || person.MapLatitude == nil ||
			person.MapLongitude == nil || *person.MapLatitude < -90 || *person.MapLatitude > 90 ||
			*person.MapLongitude < -180 || *person.MapLongitude > 180 ||
			math.IsNaN(*person.MapLatitude) || math.IsNaN(*person.MapLongitude) ||
			math.IsInf(*person.MapLatitude, 0) || math.IsInf(*person.MapLongitude, 0) {
			person.MapLatitude = nil
			person.MapLongitude = nil
		}
	}
	result.RequestID = requestID
	result.ConversationID = task.ID
	result.Actions = safeNavigationActions(result, task)
	if result.Task != nil {
		responseTask := SanitizeTaskForResponse(task)
		result.Task = &responseTask
	}
	if result.FollowUps == nil {
		result.FollowUps = []string{}
	}
	items := projectAuthorizedEntities(result)
	if result.NativeProjection {
		// Native compatibility arrays and typed Items are from one sealed read.
		// Do not project the same authorized ref twice and mark it ambiguous.
		items = []arp.Item{}
		if arp.ValidateItems(result.ProjectionItems) == nil {
			items = result.ProjectionItems
		}
	}
	refs, pins := arp.Refs(items), arp.PinIDs(items)
	generatedAt := task.UpdatedAt
	resultSetID := requestID
	if task.ID != "" {
		resultSetID = task.ID + ":" + strconv.FormatInt(task.UpdatedAt.UnixNano(), 10)
	}
	if generatedAt.IsZero() {
		generatedAt = time.Now().UTC()
	}
	filters := explicitResponseFilters(task.Filters)
	status := "ready"
	if len(refs) == 0 {
		status = "empty"
	}
	if task.Status == TaskFailed {
		status = "unsupported"
	}
	result.ResultSet = ResultSet{Schema: arp.Schema, ID: resultSetID, TaskID: task.ID, Query: result.Query,
		CityID: result.CityID, Entities: refs, Items: items, Filters: filters,
		GeneratedAt: generatedAt, Status: status, Sources: []AnswerSource{}}
	if result.NativeProjection && task.Status != TaskFailed && task.ContextType != "ONLINE" {
		owner, ownerErr := actorref.ParsePrincipal(result.PrincipalType, result.PrincipalID)
		taskOwner, taskErr := task.PrincipalRef()
		rawTask, marshalErr := json.Marshal(task)
		if ownerErr == nil && taskErr == nil && marshalErr == nil && owner.Type == actorref.Person && owner == taskOwner {
			result.ResultSet.PublicFieldEvidence = result.PublicFieldEvidence.ForResponse(owner, task.ID, rawTask, items, result.PublicCommercialRefs)
		}
	}
	result.MapEffects = MapEffects{Camera: "preserve", PinEntityIDs: pins}
	return result
}

// Compatibility arrays are projected once. New clients consume Items for
// order, map effects, details and sharing rather than independently rebuilding
// four differently scoped copies. Current domain authorization still belongs
// to the native resolver and every actual detail/share endpoint.
func projectAuthorizedEntities(result Results) []arp.Item {
	items := []arp.Item{}
	seen, ambiguous := map[string]bool{}, map[string]bool{}
	appendItem := func(i arp.Item) {
		if arp.ValidateItem(i) != nil {
			return
		}
		key := i.Entity.Key()
		if seen[key] {
			ambiguous[key] = true
			return
		}
		seen[key] = true
		items = append(items, i)
	}
	makeItem := func(kind, id, title, summary string) arp.Item {
		ref := arp.Ref{Type: kind, ID: id}
		title = strings.TrimSpace(title)
		if title == "" {
			title = "已授权实体"
		}
		summary = strings.TrimSpace(summary)
		if len([]rune(summary)) > 1000 {
			summary = string([]rune(summary)[:999]) + "…"
		}
		return arp.Item{Entity: ref, Title: title, Summary: summary, Scope: arp.AuthorizedView, Detail: &ref, Share: &ref}
	}
	point := func(kind, system, precision string, lat, lon *float64) *arp.Anchor {
		if lat == nil || lon == nil {
			return nil
		}
		a := &arp.Anchor{CoordinateSystem: system, Precision: precision, Latitude: *lat, Longitude: *lon}
		if !arp.ValidAnchor(kind, a) {
			return nil
		}
		return a
	}
	for _, a := range result.Activities {
		i := makeItem("activity", a.ID, a.Title, a.Summary)
		if a.Location != nil && a.Modality != "online" && a.Modality != "ONLINE" {
			i.Anchor = point("activity", a.Location.CoordinateSystem, a.Location.Precision, a.Location.Latitude, a.Location.Longitude)
			if i.Anchor != nil {
				i.Anchor.PlaceID = a.PlaceID
			}
		}
		appendItem(i)
	}
	for _, o := range result.Organizations {
		appendItem(makeItem("organization", o.ID, o.Name, o.Description))
	}
	for _, p := range result.Places {
		i := makeItem("place", p.ID, p.Name, p.Summary)
		i.Anchor = point("place", p.Location.CoordinateSystem, p.Location.Precision, p.Location.Latitude, p.Location.Longitude)
		if i.Anchor != nil {
			i.Anchor.PlaceID = p.ID
		}
		appendItem(i)
	}
	for _, p := range result.People {
		i := makeItem("person", p.AccountID, p.DisplayName, p.Topic)
		if publicPersonMapZone(p.PublicMapZone) && p.MapLatitude != nil && p.MapLongitude != nil {
			i.Anchor = &arp.Anchor{CoordinateSystem: "wgs84", Precision: "area", Latitude: *p.MapLatitude, Longitude: *p.MapLongitude, PublicZone: p.PublicMapZone}
		}
		appendItem(i)
	}
	for _, g := range result.Groups {
		kind := "group"
		if g.EntityType == "community" {
			kind = "community"
		}
		i := makeItem(kind, g.ID, g.Name, g.Summary)
		if kind == "group" {
			i.Detail = nil
			i.Share = nil
		} else if g.Location != nil {
			i.Anchor = point(kind, g.Location.CoordinateSystem, g.Location.Precision, g.Location.Latitude, g.Location.Longitude)
		}
		appendItem(i)
	}
	for _, i := range result.ProjectionItems {
		appendItem(i)
	}
	// Conflicting projections do not choose the more permissive source by order.
	filtered := []arp.Item{}
	for _, i := range items {
		if !ambiguous[i.Entity.Key()] {
			filtered = append(filtered, i)
		}
	}
	return filtered
}

func publicPersonMapZone(zone string) bool {
	switch zone {
	case "city_centre", "north", "south", "east", "west":
		return true
	default:
		return false
	}
}
