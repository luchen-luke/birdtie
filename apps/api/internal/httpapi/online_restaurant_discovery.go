package httpapi

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	arp "github.com/birdtie/birdtie/apps/api/internal/agentresultprojection"
	"github.com/birdtie/birdtie/apps/api/internal/agenttool"
	"github.com/birdtie/birdtie/apps/api/internal/agentworkspace"
	"github.com/birdtie/birdtie/apps/api/internal/cityseed"
	"github.com/birdtie/birdtie/apps/api/internal/foundation"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	"golang.org/x/sync/singleflight"
)

const onlineRestaurantAttribution = "© OpenStreetMap contributors (ODbL 1.0)"

// Bound the shared public cache/provider/cache operation independently of any
// caller. The provider keeps its narrower nine-second deadline below.
const onlineRestaurantSharedReadLifetime = 12 * time.Second

type onlineRestaurantHistoryKey struct{}

var onlineRestaurantNode = regexp.MustCompile(`^node/([1-9][0-9]{0,18})$`)

// Only public catalog coordinates reach the provider. The reusable cache is
// independent from owned conversation snapshots and native publication.
func WithPublicOnlinePlaceCache(cache agentworkspace.PublicOnlinePlaceCache) Option {
	return func(s *server) { s.onlinePlaceCache = cache; s.onlinePlaceFlights = &singleflight.Group{} }
}

func onlineRestaurantSnapshotID() string {
	var id [16]byte
	if _, err := rand.Read(id[:]); err != nil {
		panic("public snapshot entropy unavailable")
	}
	id[6] = (id[6] & 0x0f) | 0x40
	id[8] = (id[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", id[:4], id[4:6], id[6:8], id[8:10], id[10:])
}

func (s *server) discoverOnlineRestaurants(ctx context.Context, cityID, query, requestID string, bounds *agentworkspace.MapBounds, refresh ...bool) (*agentworkspace.OnlinePlaceSearch, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	city, err := s.catalog.GetCity(ctx, cityID)
	if err != nil {
		return nil, err
	}
	if city.Map == nil || !onlineRestaurantPoint(city.Map.Latitude, city.Map.Longitude) {
		return bindOnlineRestaurantSnapshot(unavailableOnlineRestaurantBase(cityID), query, requestID, bounds), nil
	}
	scope := agentworkspace.OnlinePlaceCacheScope{CityID: cityID, Provider: "openstreetmap", CategoryCode: "restaurant", Latitude: city.Map.Latitude, Longitude: city.Map.Longitude, RadiusMeters: 6000, PolicyVersion: "osm-odbl-1-restaurant-v2"}
	force := len(refresh) > 0 && refresh[0]
	var base *agentworkspace.OnlinePlaceSearch
	if s.onlinePlaceFlights != nil {
		// Share this in-flight public read, including failure, then forget it.
		// A subsequent explicit retry remains possible; each caller binds its
		// own private request/query/snapshot only after the shared read finishes.
		flight := s.onlinePlaceFlights.DoChan(fmt.Sprintf("%s|refresh:%t", scope.CacheKey(), force), func() (any, error) {
			// This operation carries public catalog scope only. A caller's
			// cancellation or private context values must not affect peers.
			// It may finish this bounded public cache fill after callers leave;
			// owned conversation writes remain on their original request context.
			publicCtx, cancel := context.WithTimeout(context.Background(), onlineRestaurantSharedReadLifetime)
			defer cancel()
			return s.readOnlineRestaurantBase(publicCtx, scope, force)
		})
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case completed := <-flight:
			if completed.Err != nil {
				return nil, completed.Err
			}
			base = completed.Val.(*agentworkspace.OnlinePlaceSearch)
		}
	} else {
		base, err = s.readOnlineRestaurantBase(ctx, scope, force)
		if err != nil {
			return nil, err
		}
	}
	// A completed flight and cancellation can become ready together. Never bind
	// a private turn just because select happened to choose the completed flight.
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return bindOnlineRestaurantSnapshot(base, query, requestID, bounds), nil
}

func unavailableOnlineRestaurantBase(cityID string) *agentworkspace.OnlinePlaceSearch {
	now := time.Now().UTC()
	return &agentworkspace.OnlinePlaceSearch{Schema: "birdtie-online-places-v2", CityID: cityID, Status: "UNAVAILABLE", Provider: "openstreetmap", Attribution: onlineRestaurantAttribution,
		Scope: "city", ObservedAt: now, ExpiresAt: now.Add(24 * time.Hour), Places: []agentworkspace.ExternalPlaceCandidate{}, Origin: "LIVE", Freshness: "FRESH", LicenseURL: "https://opendatacommons.org/licenses/odbl/1-0/"}
}

func (s *server) readOnlineRestaurantBase(ctx context.Context, scope agentworkspace.OnlinePlaceCacheScope, force bool) (*agentworkspace.OnlinePlaceSearch, error) {
	result := unavailableOnlineRestaurantBase(scope.CityID)
	var err error
	now := result.ObservedAt
	var cached *agentworkspace.OnlinePlaceSearch
	if s.onlinePlaceCache != nil {
		var found bool
		cached, found, err = s.onlinePlaceCache.ReadPublicOnlinePlaceCache(ctx, scope)
		if err != nil {
			return nil, err
		}
		if found && !validRetainedOnlineRestaurant(cached, scope) {
			return nil, arp.ErrChanged
		}
		if !found {
			cached = nil
		}
	}
	if cached != nil && !force {
		cached.Origin = "CACHE"
		return cached, nil
	}
	if s.openPlaces == nil {
		return onlineRestaurantRefreshFallback(cached, result, force), nil
	}
	providerCtx, cancel := context.WithTimeout(ctx, 9*time.Second)
	defer cancel()
	facts, err := s.openPlaces.Search(providerCtx, agenttool.OpenPlaceQuery{Latitude: scope.Latitude, Longitude: scope.Longitude, RadiusMeters: scope.RadiusMeters, Category: scope.CategoryCode})
	if err != nil || len(facts) > agenttool.OpenStreetMapPlacesMaxResults {
		return onlineRestaurantRefreshFallback(cached, result, force), nil
	}
	now = time.Now().UTC()
	result.ObservedAt, result.ExpiresAt = now, now.Add(24*time.Hour)
	seen := map[string]bool{}
	for _, fact := range facts {
		candidate, valid := onlineRestaurantCandidate(fact, scope.CityID, now, result.ExpiresAt)
		if !valid || seen[candidate.ID] || onlineRestaurantDistance(scope.Latitude, scope.Longitude, *candidate.Location.Latitude, *candidate.Location.Longitude) > float64(scope.RadiusMeters) {
			result.Places = []agentworkspace.ExternalPlaceCandidate{}
			return onlineRestaurantRefreshFallback(cached, result, force), nil
		}
		seen[candidate.ID] = true
		result.Places = append(result.Places, candidate)
	}
	sort.SliceStable(result.Places, func(i, j int) bool {
		a, b := result.Places[i].Location, result.Places[j].Location
		return onlineRestaurantDistance(scope.Latitude, scope.Longitude, *a.Latitude, *a.Longitude) < onlineRestaurantDistance(scope.Latitude, scope.Longitude, *b.Latitude, *b.Longitude)
	})
	result.Status = "SUCCESS"
	for _, p := range result.Places {
		if p.ExpiresAt.Before(result.ExpiresAt) {
			result.ExpiresAt = p.ExpiresAt
		}
	}
	for i := range result.Places {
		result.Places[i].ExpiresAt = result.ExpiresAt
	}
	if len(result.Places) == 0 {
		result.Status = "EMPTY"
	}
	// Cache the full city batch before selecting any phone viewport.
	if s.onlinePlaceCache != nil {
		result, err = s.onlinePlaceCache.StorePublicOnlinePlaceCache(ctx, scope, result)
		if err != nil {
			return nil, err
		}
	}
	result.Origin = "LIVE"
	return result, nil
}

func bindOnlineRestaurantSnapshot(base *agentworkspace.OnlinePlaceSearch, query, requestID string, bounds *agentworkspace.MapBounds) *agentworkspace.OnlinePlaceSearch {
	result := *base
	result.Places = make([]agentworkspace.ExternalPlaceCandidate, 0, len(base.Places))
	for _, p := range base.Places {
		if bounds == nil || pointInBounds(p.Location, bounds) {
			result.Places = append(result.Places, p)
		}
	}
	result.Query, result.RequestID, result.SnapshotID = query, requestID, onlineRestaurantSnapshotID()
	result.TurnDigest, result.TaskID, result.MessageIndex = agentworkspace.OnlinePlaceTurnDigest(query), "", 1
	result.Scope = "city"
	if bounds != nil {
		result.Scope = "viewport"
	}
	result.Freshness = "FRESH"
	if !result.ExpiresAt.After(time.Now().UTC()) {
		result.Freshness = "STALE"
	}
	if result.Status == "SUCCESS" && len(result.Places) == 0 {
		result.Status = "EMPTY"
	}
	return &result
}

func onlineRestaurantRefreshFallback(cached, unavailable *agentworkspace.OnlinePlaceSearch, force bool) *agentworkspace.OnlinePlaceSearch {
	if cached != nil {
		copy := *cached
		copy.Origin, copy.RefreshFailed = "CACHE", force
		return &copy
	}
	return unavailable
}

// Retained open data has independent provenance validation. Freshness ending
// must never delete a conversation or rewrite the original observation time.
func validRetainedOnlineRestaurant(search *agentworkspace.OnlinePlaceSearch, scope agentworkspace.OnlinePlaceCacheScope) bool {
	if search == nil || search.Schema != "birdtie-online-places-v2" || search.CityID != scope.CityID || search.Provider != "openstreetmap" || search.Attribution != onlineRestaurantAttribution || search.LicenseURL != "https://opendatacommons.org/licenses/odbl/1-0/" || search.ObservedAt.IsZero() || search.ObservedAt.After(time.Now().UTC().Add(time.Second)) || !search.ExpiresAt.After(search.ObservedAt) || search.ExpiresAt.Sub(search.ObservedAt) > 24*time.Hour || len(search.Places) > agenttool.OpenStreetMapPlacesMaxResults || (search.Status != "SUCCESS" && search.Status != "EMPTY") || (search.Status == "SUCCESS" && len(search.Places) == 0) || (search.Status == "EMPTY" && len(search.Places) != 0) {
		return false
	}
	seen := map[string]bool{}
	for _, p := range search.Places {
		id := strings.TrimPrefix(p.ID, "osm:node:")
		if !onlineRestaurantNode.MatchString("node/"+id) || p.ID != "osm:node:"+id || seen[p.ID] || p.CityID != search.CityID || p.CategoryCode != "restaurant" || p.SourceURL != "https://www.openstreetmap.org/node/"+id || p.Attribution != search.Attribution || p.ReviewStatus != "UNREVIEWED" || !cityseed.SourceTextValid(p.Name, 150) || (p.AddressLabel != "" && !cityseed.SourceTextValid(p.AddressLabel, 240)) || p.Location.Precision != "point" || p.Location.CoordinateSystem != "wgs84" || p.Location.Latitude == nil || p.Location.Longitude == nil || !onlineRestaurantPoint(*p.Location.Latitude, *p.Location.Longitude) || !p.ObservedAt.Equal(search.ObservedAt) || !p.ExpiresAt.Equal(search.ExpiresAt) || (scope.RadiusMeters > 0 && onlineRestaurantDistance(scope.Latitude, scope.Longitude, *p.Location.Latitude, *p.Location.Longitude) > float64(scope.RadiusMeters)) {
			return false
		}
		seen[p.ID] = true
	}
	return true
}

func onlineRestaurantCandidate(fact cityseed.SubmitInput, cityID string, now, expires time.Time) (agentworkspace.ExternalPlaceCandidate, bool) {
	matches := onlineRestaurantNode.FindStringSubmatch(fact.ProviderPlaceID)
	if len(matches) != 2 || fact.ProviderCode != "openstreetmap" || fact.SourceURL != "https://www.openstreetmap.org/node/"+matches[1] ||
		fact.CategoryCode != "restaurant" || fact.Attribution != onlineRestaurantAttribution || !cityseed.SourceTextValid(fact.Name, 150) ||
		(fact.AddressLabel != "" && !cityseed.SourceTextValid(fact.AddressLabel, 240)) || fact.LocationPrecision != "point" || fact.Latitude == nil || fact.Longitude == nil ||
		!onlineRestaurantPoint(*fact.Latitude, *fact.Longitude) || !fact.ExpiresAt.After(now) {
		return agentworkspace.ExternalPlaceCandidate{}, false
	}
	if _, _, err := cityseed.NormalizeSourcePermission(fact, now); err != nil {
		return agentworkspace.ExternalPlaceCandidate{}, false
	}
	rights := fact.Rights
	if rights.Acquisition != "OPEN_LICENSE" || rights.OriginProvider != "OPEN_DATA_DOWNLOAD" || rights.LicenseVersion != "ODbL-1.0" || rights.LicenseURL != "https://opendatacommons.org/licenses/odbl/1-0/" || rights.Attribution != onlineRestaurantAttribution || now.Sub(rights.AcquiredAt) > time.Minute {
		return agentworkspace.ExternalPlaceCandidate{}, false
	}
	fields := map[string]bool{}
	for _, field := range fact.Fields {
		if field.Key != "name" && field.Key != "coordinates" && field.Key != "address" {
			continue
		}
		if fields[field.Key] || field.ObservedAt.IsZero() || field.ObservedAt.After(now) || now.Sub(field.ObservedAt) > time.Minute || field.ValidFrom.After(now) || !field.ValidTo.After(now) || !field.RefreshDue.After(now) || field.ConflictState != "NONE" {
			return agentworkspace.ExternalPlaceCandidate{}, false
		}
		fields[field.Key] = true
		if field.Key == "coordinates" {
			var point struct {
				CoordinateSystem    string `json:"coordinateSystem"`
				Latitude, Longitude float64
			}
			if json.Unmarshal(field.Value, &point) != nil || point.CoordinateSystem != "wgs84" || point.Latitude != *fact.Latitude || point.Longitude != *fact.Longitude {
				return agentworkspace.ExternalPlaceCandidate{}, false
			}
		} else {
			var text string
			expected := fact.Name
			if field.Key == "address" {
				expected = fact.AddressLabel
			}
			if json.Unmarshal(field.Value, &text) != nil || text != expected {
				return agentworkspace.ExternalPlaceCandidate{}, false
			}
		}
		if field.ValidTo.Before(expires) {
			expires = field.ValidTo
		}
		if field.RefreshDue.Before(expires) {
			expires = field.RefreshDue
		}
	}
	if !fields["name"] || !fields["coordinates"] || (fact.AddressLabel != "" && !fields["address"]) {
		return agentworkspace.ExternalPlaceCandidate{}, false
	}
	if fact.ExpiresAt.Before(expires) {
		expires = fact.ExpiresAt
	}
	lat, lon := *fact.Latitude, *fact.Longitude
	return agentworkspace.ExternalPlaceCandidate{ID: "osm:node:" + matches[1], CityID: cityID, Name: fact.Name, CategoryCode: "restaurant", AddressLabel: fact.AddressLabel,
		Location: foundation.Location{Precision: "point", CoordinateSystem: "wgs84", Latitude: &lat, Longitude: &lon}, SourceURL: fact.SourceURL, Attribution: fact.Attribution,
		ReviewStatus: "UNREVIEWED", ObservedAt: now, ExpiresAt: expires}, true
}

func onlineRestaurantPoint(lat, lon float64) bool {
	return !math.IsNaN(lat) && !math.IsNaN(lon) && !math.IsInf(lat, 0) && !math.IsInf(lon, 0) && lat >= -90 && lat <= 90 && lon >= -180 && lon <= 180
}
func onlineRestaurantDistance(lat1, lon1, lat2, lon2 float64) float64 {
	dlat, dlon := (lat2-lat1)*math.Pi/180, (lon2-lon1)*math.Pi/180
	h := math.Sin(dlat/2)*math.Sin(dlat/2) + math.Cos(lat1*math.Pi/180)*math.Cos(lat2*math.Pi/180)*math.Sin(dlon/2)*math.Sin(dlon/2)
	return 6371000 * 2 * math.Asin(math.Sqrt(math.Max(0, math.Min(1, h))))
}

func onlineRestaurantMessage(search *agentworkspace.OnlinePlaceSearch, native int) string {
	scope := "当前城市中心周边 6 公里"
	if search.Scope == "viewport" {
		scope += "内的这片地图区域"
	}
	switch search.Status {
	case "SUCCESS":
		names := []string{}
		for i, p := range search.Places {
			if i == 3 {
				break
			}
			names = append(names, p.Name)
		}
		prefix := "已联网找到"
		if search.Origin == "CACHE" {
			prefix = "已复用已保存资料，找到"
		}
		if search.Origin == "HISTORY" {
			prefix = "这次查询保存了"
		}
		message := fmt.Sprintf("可以先看看 %s。%s %d 家餐厅，并标在地图上。范围是%s；按离市中心的距离整理，尚未核验评分和当前营业状态。", strings.Join(names, "、"), prefix, len(search.Places), scope)
		if search.Freshness == "STALE" {
			message += "资料已超过更新周期，可以查看原结果或点击更新餐厅信息。"
		}
		if search.RefreshFailed {
			message += "本次更新暂未成功，已保留上次保存的餐厅资料。"
		}
		return message
	case "EMPTY":
		prefix := "已联网查询"
		if search.Origin == "CACHE" || search.Origin == "HISTORY" {
			prefix = "已保存的查询范围是"
		}
		message := fmt.Sprintf("%s%s，当前来源没有返回餐厅。可以移动地图或换个城市后再查，也可以更新餐厅信息。", prefix, scope)
		if search.Freshness == "STALE" {
			message += "这是上次查询保存的空结果，资料待更新。"
		}
		if search.RefreshFailed {
			message += "本次更新暂未成功，已保留原结果。"
		}
		return message
	default:
		if native > 0 {
			return fmt.Sprintf("已显示 %d 个站内餐厅；联网餐厅来源暂时不可用，请稍后重新查询。", native)
		}
		return "联网餐厅来源暂时不可用，暂时无法给出推荐。请稍后重新查询。"
	}
}

// Store actual text and an immutable public-data snapshot in the same owned
// reply transaction. Public cache records have no conversation ownership.
func (s *server) finishOnlineRestaurantQuery(r *http.Request, digest [32]byte, task agentworkspace.Task, search *agentworkspace.OnlinePlaceSearch, message string) (agentworkspace.Task, error) {
	if task.PrincipalID != "" {
		if native, ok := s.agent.(agentworkspace.OwnOnlinePlaceReplyStore); ok {
			// Compare the ACTIVE task captured before the network/cache read.
			// A later query must never be overwritten to manufacture a fresh CAS.
			raw, err := json.Marshal(agentworkspace.SanitizeTaskForResponse(task))
			if err != nil {
				return task, err
			}
			return native.CompleteOwnOnlinePlaceReply(r.Context(), arp.Access{Actor: identity.Actor{ID: task.PrincipalID, AccountType: "person"}, SessionDigest: digest, TaskID: task.ID, ExpectedTask: raw}, *search, message)
		}
	}
	copy := *search
	copy.Places = append([]agentworkspace.ExternalPlaceCandidate{}, search.Places...)
	copy.TaskID, copy.MessageIndex = task.ID, len(task.Conversation)
	task.Status = agentworkspace.TaskCompleted
	task.Conversation = append(task.Conversation, agentworkspace.Message{Role: "assistant", Text: message, ExternalPlaceReply: &copy})
	if task.PrincipalID != "" {
		return s.agent.UpdateTask(r.Context(), task)
	}
	return task, nil
}

func isOnlineRestaurantRefresh(query string) bool {
	text := strings.TrimRight(strings.ToLower(strings.TrimSpace(query)), " ?？!！。.")
	switch text {
	case "更新餐厅信息", "刷新餐厅", "重新联网查询餐厅", "refresh restaurants":
		return true
	}
	return false
}

func (s *server) retainedOnlineRestaurantTask(ctx context.Context, task agentworkspace.Task) (*agentworkspace.OnlinePlaceSearch, error) {
	_, err := s.catalog.GetCity(ctx, task.CityID)
	if err != nil {
		return nil, err
	}
	for i, m := range task.Conversation {
		p := m.ExternalPlaceReply
		if p == nil {
			continue
		}
		if m.Role != "assistant" || i < 1 || task.Conversation[i-1].Role != "user" || p.Query != task.Conversation[i-1].Text || p.TurnDigest != agentworkspace.OnlinePlaceTurnDigest(p.Query) || p.TaskID != task.ID || p.MessageIndex != i || !uuidPath.MatchString(p.SnapshotID) || p.RequestID == "" || p.Scope != "city" && p.Scope != "viewport" || p.Origin != "LIVE" && p.Origin != "CACHE" || p.CacheVersion < 0 {
			return nil, arp.ErrChanged
		}
		// Historical coordinates describe the original query, independent of
		// later changes to the catalog city center. Cache reads still enforce 6km.
		scope := agentworkspace.OnlinePlaceCacheScope{CityID: task.CityID}
		if p.Status == "UNAVAILABLE" && len(p.Places) == 0 {
			copy := *p
			copy.Status = "EMPTY"
			if !validRetainedOnlineRestaurant(&copy, scope) {
				return nil, arp.ErrChanged
			}
		} else if !validRetainedOnlineRestaurant(p, scope) {
			return nil, arp.ErrChanged
		}
	}
	last := len(task.Conversation) - 1
	if last < 0 || task.Conversation[last].ExternalPlaceReply == nil {
		return nil, nil
	}
	copy := *task.Conversation[last].ExternalPlaceReply
	copy.Places = append([]agentworkspace.ExternalPlaceCandidate{}, copy.Places...)
	copy.Origin, copy.Freshness = "HISTORY", "FRESH"
	if !copy.ExpiresAt.After(time.Now().UTC()) {
		copy.Freshness = "STALE"
	}
	return &copy, nil
}

func restaurantDiscoveryInCity(query string, city foundation.City) (agentworkspace.MVPIntent, bool) {
	if !utf8.ValidString(query) {
		return agentworkspace.MVPIntent{}, false
	}
	for _, r := range query {
		if unicode.IsControl(r) || unicode.In(r, unicode.Cf) {
			return agentworkspace.MVPIntent{}, false
		}
	}
	aliases := []string{city.Name}
	if city.ID == "aberdeen-gb" {
		aliases = append(aliases, "阿伯丁", "阿布丁")
	}
	text := strings.TrimRight(strings.TrimSpace(strings.ToLower(query)), "?？.。!！")
	for _, name := range aliases {
		name = strings.ToLower(strings.TrimSpace(name))
		if name == "" {
			continue
		}
		for _, prefix := range []string{name, "在" + name, "in " + name} {
			if tail, ok := strings.CutPrefix(text, prefix); ok {
				tail = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(tail), "的"))
				if intent, yes := agentworkspace.ParseRestaurantDiscoveryQuery(tail); yes {
					return intent, true
				}
			}
		}
		if head, ok := strings.CutSuffix(text, " in "+name); ok {
			if intent, yes := agentworkspace.ParseRestaurantDiscoveryQuery(strings.TrimSpace(head)); yes {
				return intent, true
			}
		}
	}
	return agentworkspace.MVPIntent{}, false
}
