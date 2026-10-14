package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"reflect"
	"strings"
	"unicode"
	"unicode/utf8"

	arp "github.com/birdtie/birdtie/apps/api/internal/agentresultprojection"
	"github.com/birdtie/birdtie/apps/api/internal/agentworkspace"
	"github.com/birdtie/birdtie/apps/api/internal/foundation"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
)

// Public selectors, never client-provided facts or access to a guest transcript.
// The server rereads the existing public database batch before interpreting an
// ordinal, name or comparison. Its version prevents silently changing subjects.
type onlinePlaceContext struct {
	CacheVersion int64    `json:"cacheVersion"`
	Scope        string   `json:"scope"`
	PlaceIDs     []string `json:"placeIds"`
}

type onlineRestaurantFollowupKey struct{}

func validOnlinePlaceContext(ref *onlinePlaceContext) bool {
	if ref == nil || ref.CacheVersion < 1 || ref.Scope != "city" && ref.Scope != "viewport" || len(ref.PlaceIDs) == 0 || len(ref.PlaceIDs) > 15 {
		return false
	}
	seen := map[string]bool{}
	for _, id := range ref.PlaceIDs {
		if !strings.HasPrefix(id, "osm:node:") || !onlineRestaurantNode.MatchString("node/"+strings.TrimPrefix(id, "osm:node:")) || seen[id] {
			return false
		}
		seen[id] = true
	}
	return true
}

func restaurantFollowupKind(query string) string {
	if restaurantFollowupNewTopic(query) {
		return ""
	}
	return restaurantContinuationKind(query)
}

// An explicit new target takes precedence over generic address, price and
// distance words. Use the current parser for its activity/organization/person
// vocabulary; the Place aliases mirror its existing public taxonomy. Hotel is
// only an unsupported-target fence here, never a newly implemented search.
func restaurantFollowupNewTopic(query string) bool {
	t := strings.ToLower(query)
	intent := agentworkspace.ParseMVPIntent(query, nil)
	switch intent.Target {
	case agentworkspace.FindActivity, agentworkspace.FindOrganization,
		agentworkspace.FindPerson, agentworkspace.FindNewPeople,
		agentworkspace.FindCommunity, agentworkspace.FindBusiness,
		agentworkspace.FindOpportunity, agentworkspace.PersonalRelationshipContext:
		return true
	}
	if containsAnyText(t, "活动", "比赛", "运动", "羽毛球", "篮球", "足球", "文化", "组织", "社团", "协会", "社区", "社群", "公开成员", "公开的人", "商家", "商户", "社交机会", "活动机会", "咖啡馆", "咖啡店", "博物馆", "美术馆", "画廊", "图书馆", "公园", "体育馆", "场馆", "酒店", "旅馆", "宾馆", "coffee shop") {
		return true
	}
	words := strings.FieldsFunc(t, func(r rune) bool { return !unicode.IsLetter(r) })
	for _, word := range words {
		switch word {
		case "cafe", "cafes", "café", "cafés", "museum", "museums",
			"gallery", "galleries", "library", "libraries", "park", "parks",
			"hotel", "hotels", "hostel", "hostels", "venue", "venues",
			"activity", "activities", "event", "events", "organization", "organizations",
			"organisation", "organisations", "society", "societies", "club", "clubs", "community", "communities",
			"badminton", "basketball", "football", "sports", "culture", "tournament", "tournaments",
			"business", "businesses", "people", "person", "opportunity", "opportunities":
			return true
		}
	}
	return false
}

func restaurantContinuationKind(query string) string {
	if !utf8.ValidString(query) || len(query) > 240 {
		return ""
	}
	for _, r := range query {
		if unicode.IsControl(r) || unicode.In(r, unicode.Cf) {
			return ""
		}
	}
	t := strings.ToLower(strings.TrimRight(strings.TrimSpace(query), " ?？!！。."))
	for _, prefix := range []string{"找活动", "找组织", "找人", "找地点", "搜索", "查找", "search ", "find "} {
		if strings.HasPrefix(t, prefix) {
			return ""
		}
	}
	if containsAnyText(t, "住址", "私人", "私密", "记忆", "朋友", "我家", "my home", "private", "memory", "ignore", "忽略", "发布", "创建", "删除") {
		return ""
	}
	if containsAnyText(t, "评分", "最好", "哪家好", "哪个好", "好吃", "人均", "价钱", "价格", "预算", "开门", "营业", "预约", "预订", "订位", "rating", "best", "better", "price", "open now", "opening hours", "book", "reserve", "菜系", "海鲜", "辣", "素食") {
		return "unknown"
	}
	if containsAnyText(t, "比较", "对比", "compare") {
		return "compare"
	}
	if containsAnyText(t, "市中心", "近一点", "更近", "最近", "closer", "city centre", "city center", "nearest") {
		return "distance"
	}
	if containsAnyText(t, "地址", "在哪里", "在哪", "address", "where is", "where are") {
		return "address"
	}
	if containsAnyText(t, "标在地图", "地图上显示", "地图上看", "show on map") {
		return "map"
	}
	for _, prefix := range []string{"它", "这家", "这些", "这批餐厅", "这个店", "第一家", "第二家", "前两家", "this restaurant ", "these restaurants "} {
		if strings.HasPrefix(t, prefix) {
			return "unknown"
		}
	}
	return ""
}

func containsAnyText(t string, values ...string) bool {
	for _, v := range values {
		if strings.Contains(t, v) {
			return true
		}
	}
	return false
}

func (s *server) publicRestaurantContinuation(ctx context.Context, cityID string, ref *onlinePlaceContext) (*agentworkspace.OnlinePlaceSearch, foundation.City, error) {
	city, err := s.catalog.GetCity(ctx, cityID)
	if err != nil {
		return nil, city, err
	}
	if !validOnlinePlaceContext(ref) || city.Map == nil || s.onlinePlaceCache == nil {
		return nil, city, arp.ErrChanged
	}
	scope := agentworkspace.OnlinePlaceCacheScope{CityID: cityID, Provider: "openstreetmap", CategoryCode: "restaurant", Latitude: city.Map.Latitude, Longitude: city.Map.Longitude, RadiusMeters: 6000, PolicyVersion: "osm-odbl-1-restaurant-v2"}
	base, found, err := s.onlinePlaceCache.ReadPublicOnlinePlaceCache(ctx, scope)
	if err != nil {
		return nil, city, err
	}
	if !found || !validRetainedOnlineRestaurant(base, scope) || base.CacheVersion != ref.CacheVersion {
		return nil, city, arp.ErrChanged
	}
	if ref.Scope == "city" && len(ref.PlaceIDs) != len(base.Places) {
		return nil, city, arp.ErrChanged
	}
	selected := make([]agentworkspace.ExternalPlaceCandidate, 0, len(ref.PlaceIDs))
	cursor := 0
	for _, p := range base.Places {
		if cursor < len(ref.PlaceIDs) && p.ID == ref.PlaceIDs[cursor] {
			selected = append(selected, p)
			cursor++
		}
	}
	if cursor != len(ref.PlaceIDs) {
		return nil, city, arp.ErrChanged
	}
	copy := *base
	copy.Places, copy.Scope, copy.Origin = selected, ref.Scope, "CACHE"
	return &copy, city, nil
}

func restaurantContinuationRef(p *agentworkspace.OnlinePlaceSearch) *onlinePlaceContext {
	ref := &onlinePlaceContext{CacheVersion: p.CacheVersion, Scope: p.Scope, PlaceIDs: []string{}}
	for _, place := range p.Places {
		ref.PlaceIDs = append(ref.PlaceIDs, place.ID)
	}
	return ref
}

// Latin names require word boundaries: a saved Park must not turn a new
// "parks near city center" question into a restaurant continuation. Chinese
// field words may immediately follow an English name, as in Park Cafe地址.
func restaurantNameRemainder(query, name string) (string, bool) {
	if name == "" {
		return query, false
	}
	latin := func(r rune) bool { return unicode.In(r, unicode.Latin) && unicode.IsLetter(r) || unicode.IsMark(r) }
	first, _ := utf8.DecodeRuneInString(name)
	last, _ := utf8.DecodeLastRuneInString(name)
	var out strings.Builder
	matched := false
	for cursor := 0; cursor < len(query); {
		rel := strings.Index(query[cursor:], name)
		if rel < 0 {
			out.WriteString(query[cursor:])
			break
		}
		start := cursor + rel
		end := start + len(name)
		before, _ := utf8.DecodeLastRuneInString(query[:start])
		after, _ := utf8.DecodeRuneInString(query[end:])
		bounded := (!latin(first) || start == 0 || !latin(before)) && (!latin(last) || end == len(query) || !latin(after))
		out.WriteString(query[cursor:start])
		if bounded {
			matched = true
		} else {
			out.WriteString(query[start:end])
		}
		cursor = end
	}
	return out.String(), matched
}

func restaurantFollowupSelection(query string, places []agentworkspace.ExternalPlaceCandidate) []int {
	t := strings.ToLower(query)
	selected := []int{}
	for i, p := range places {
		if strings.Contains(t, strings.ToLower(p.Name)) {
			selected = append(selected, i)
		}
	}
	if len(selected) > 0 {
		return selected
	}
	for i, ordinal := range []string{"一", "二", "三", "四", "五", "六", "七", "八", "九", "十", "十一", "十二", "十三", "十四", "十五"} {
		if containsAnyText(t, "第"+ordinal+"家", "第"+ordinal+"个", fmt.Sprintf("第%d家", i+1), fmt.Sprintf("第%d个", i+1)) {
			if i >= len(places) {
				return []int{-1}
			}
			selected = append(selected, i)
		}
	}
	if len(selected) == 0 && len(places) == 1 {
		return []int{0}
	}
	return selected
}

func restaurantFollowupMessage(query, kind string, search *agentworkspace.OnlinePlaceSearch, city foundation.City) string {
	selected := restaurantFollowupSelection(query, search.Places)
	if len(selected) > 0 && selected[0] == -1 {
		return "这次保存的结果没有这个序号。可以直接说卡片上的店名；原餐厅结果仍保留。"
	}
	address := func(p agentworkspace.ExternalPlaceCandidate) string {
		if p.AddressLabel == "" {
			return "来源暂未提供完整地址"
		}
		return p.AddressLabel
	}
	distance := func(p agentworkspace.ExternalPlaceCandidate) string {
		meters := onlineRestaurantDistance(city.Map.Latitude, city.Map.Longitude, *p.Location.Latitude, *p.Location.Longitude)
		return fmt.Sprintf("距市中心约 %.1f 公里（直线）", meters/1000)
	}
	message := ""
	switch kind {
	case "unknown":
		message = "这批资料只有餐厅名称、地址和地图位置，尚未提供评分、菜系、人均价格、即时营业状态或预订规则，无法据此判断哪家最好、符合这些条件或现在开门。已保留原餐厅卡片；可以查看地址、比较位置或打开原始来源核实。这里不会自动预订。"
	case "distance":
		if len(selected) == 0 {
			selected = []int{0}
			for i, p := range search.Places {
				best := search.Places[selected[0]]
				if onlineRestaurantDistance(city.Map.Latitude, city.Map.Longitude, *p.Location.Latitude, *p.Location.Longitude) < onlineRestaurantDistance(city.Map.Latitude, city.Map.Longitude, *best.Location.Latitude, *best.Location.Longitude) {
					selected[0] = i
				}
			}
		}
		parts := []string{}
		for _, i := range selected {
			parts = append(parts, search.Places[i].Name+"："+distance(search.Places[i]))
		}
		message = strings.Join(parts, "；") + "。这是到当前城市公共中心的距离，不是到你所在位置的距离或实际路线。"
	case "compare":
		if len(selected) == 0 && containsAnyText(strings.ToLower(query), "前两家", "前两个", "first two") && len(search.Places) >= 2 {
			selected = []int{0, 1}
		}
		if len(selected) < 2 {
			message = "想比较哪两家？可以说卡片上的店名，或输入“比较前两家”。原餐厅结果仍保留。"
			break
		}
		parts := []string{}
		if len(selected) > 2 {
			message = "每次先比较两家餐厅，请指定两家店名或输入“比较前两家”。原餐厅结果仍保留。"
			break
		}
		for _, i := range selected {
			p := search.Places[i]
			parts = append(parts, p.Name+"："+address(p)+"，"+distance(p))
		}
		message = strings.Join(parts, "；") + "。可以比较地址和位置；当前资料不足以比较评分、价格或用餐体验。"
	case "address":
		if len(selected) == 0 && containsAnyText(strings.ToLower(query), "有哪些地址", "所有地址", "这些餐厅的地址", "addresses", "全部地址") {
			for i := range search.Places {
				selected = append(selected, i)
			}
		}
		if len(selected) == 0 {
			message = "想看哪家餐厅的地址？可以说卡片上的店名，或输入“第一家地址”。原餐厅结果仍保留。"
			break
		}
		parts := []string{}
		for _, i := range selected {
			p := search.Places[i]
			if len(strings.Join(parts, "；"))+len(p.Name)+len(address(p)) > 1600 {
				break
			}
			parts = append(parts, p.Name+"："+address(p))
		}
		message = strings.Join(parts, "；") + "。可点击对应卡片在地图上查看。"
		if len(parts) < len(selected) {
			message += "其余完整地址保留在对应卡片中。"
		}
	case "map":
		message = fmt.Sprintf("已保留这 %d 家餐厅的地图位置；点击卡片上的地图操作即可定位到对应餐厅。", len(search.Places))
	}
	message += "本轮复用已保存的公开资料，没有重新联网搜索。"
	if search.Freshness == "STALE" {
		message += "资料已超过更新周期，原采集时间保留；需要最新信息时可更新餐厅信息。"
	}
	return message
}

// Returns true only for a recognized continuation or a rejected public selector.
// Ordinary new searches retain their existing domain paths and permissions.
func (s *server) tryOnlineRestaurantFollowup(w http.ResponseWriter, r *http.Request, digest [32]byte, actor identity.Actor, task agentworkspace.Task, ref *onlinePlaceContext, query, workspace, role string, permissions []string) bool {
	if ref != nil && !validOnlinePlaceContext(ref) {
		respondError(w, http.StatusBadRequest, "invalid_online_place_context")
		return true
	}
	kind := restaurantFollowupKind(query)
	if kind == "" && restaurantFollowupNewTopic(query) {
		// A real saved restaurant may itself be named Park Cafe or contain an
		// activity keyword. Only disregard target words inside that exact name;
		// "Nando's附近有哪些咖啡馆" still starts a new topic. Guest facts come
		// from the public database, never from client names or someone else's Task.
		var saved *agentworkspace.OnlinePlaceSearch
		if actor.ID != "" && len(task.Conversation) > 0 {
			last := task.Conversation[len(task.Conversation)-1]
			if last.Role == "assistant" {
				saved = last.ExternalPlaceReply
			}
		} else if ref != nil {
			saved, _, _ = s.publicRestaurantContinuation(r.Context(), task.CityID, ref)
		}
		if saved != nil {
			t := strings.ToLower(query)
			for _, place := range saved.Places {
				name := strings.ToLower(place.Name)
				remaining, named := restaurantNameRemainder(t, name)
				if named && !restaurantFollowupNewTopic(remaining) {
					kind = restaurantContinuationKind(query)
					break
				}
			}
		}
	}
	if _, discovery := agentworkspace.ParseRestaurantDiscoveryQuery(query); discovery {
		return false
	}
	if kind == "" || r.Header.Get("X-Birdtie-Organization-Workspace") != "" {
		return false
	}
	if city, err := s.catalog.GetCity(r.Context(), task.CityID); err == nil {
		if _, discovery := restaurantDiscoveryInCity(query, city); discovery {
			return false
		}
	}
	if actor.ID != "" && task.ID != "" && task.Intent == agentworkspace.FindPlace {
		p, err := s.retainedOnlineRestaurantTask(r.Context(), task)
		if err != nil {
			resultProjectionFailure(w, err)
			return true
		}
		if p == nil {
			return false
		}
		owned := restaurantContinuationRef(p)
		if ref != nil {
			a, _ := json.Marshal(ref)
			b, _ := json.Marshal(owned)
			if string(a) != string(b) {
				respondError(w, http.StatusConflict, "online_place_context_changed")
				return true
			}
		}
		ref = owned
	}
	if ref == nil {
		return false
	}
	base, city, err := s.publicRestaurantContinuation(r.Context(), task.CityID, ref)
	if err != nil {
		respond(w, http.StatusConflict, map[string]any{"error": map[string]string{"code": "online_place_context_changed", "message": "保存的餐厅资料版本或城市范围已变化。原对话和卡片仍可查看；请重新搜索后继续提问。"}})
		return true
	}
	search := bindOnlineRestaurantSnapshot(base, query, w.Header().Get("X-Request-ID"), nil)
	search.Scope = ref.Scope
	message := restaurantFollowupMessage(query, kind, search, city)
	verified, _, err := s.publicRestaurantContinuation(r.Context(), task.CityID, ref)
	if err != nil || !verified.ObservedAt.Equal(base.ObservedAt) || !verified.ExpiresAt.Equal(base.ExpiresAt) || !reflect.DeepEqual(verified.Places, base.Places) {
		respond(w, http.StatusConflict, map[string]any{"error": map[string]string{"code": "online_place_context_changed", "message": "餐厅资料在本轮读取期间发生变化。原回答和卡片仍保留，请更新餐厅信息后继续。"}})
		return true
	}
	if actor.ID != "" {
		if native, ok := s.agent.(agentworkspace.OwnOnlinePlaceFollowupStore); ok {
			// The CAS refers to the old completed Task, before appending even
			// the user message. Late replies cannot manufacture a fresh CAS.
			raw, marshalErr := json.Marshal(agentworkspace.SanitizeTaskForResponse(task))
			if marshalErr != nil {
				serverError(w, marshalErr)
				return true
			}
			task, err = native.CompleteOwnOnlinePlaceFollowup(r.Context(), arp.Access{Actor: actor, SessionDigest: digest, TaskID: task.ID, ExpectedTask: raw}, query, *search, message)
			if err != nil {
				resultProjectionFailure(w, err)
				return true
			}
			s.respondOnlineRestaurantContinuation(w, r, digest, actor, task, message, query, workspace, role, permissions)
			return true
		}
	}
	filters := make(map[string]string, len(task.Filters)+6)
	for k, v := range task.Filters {
		filters[k] = v
	}
	task.Filters = filters
	agentworkspace.ClearPlaceFollowupContext(&task)
	task.Intent, task.Status = agentworkspace.FindPlace, agentworkspace.TaskActive
	task.Filters["targetIntent"], task.Filters["category"], task.Filters["reason"] = agentworkspace.FindPlace, "restaurant", agentworkspace.RestaurantDiscovery
	task.Filters["currentQuery"], task.Filters["locationPreference"], task.Filters["searchTerm"] = query, ref.Scope, ""
	task.Conversation = append(append([]agentworkspace.Message(nil), task.Conversation...), agentworkspace.Message{Role: "user", Text: query})
	if actor.ID != "" {
		task, err = s.agent.UpdateTask(r.Context(), task)
		if err != nil {
			agentTaskWriteFailure(w, err, false)
			return true
		}
	}
	task, err = s.finishOnlineRestaurantQuery(r, digest, task, search, message)
	if err != nil {
		agentTaskWriteFailure(w, err, false)
		return true
	}
	s.respondOnlineRestaurantContinuation(w, r, digest, actor, task, message, query, workspace, role, permissions)
	return true
}

func (s *server) respondOnlineRestaurantContinuation(w http.ResponseWriter, r *http.Request, digest [32]byte, actor identity.Actor, task agentworkspace.Task, message, query, workspace, role string, permissions []string) {
	result := emptyAgentResults(task.CityID, query)
	result.OnlinePlaceSearch = task.Conversation[len(task.Conversation)-1].ExternalPlaceReply
	result.Message, result.FollowUps = message, []string{"比较前两家", "第一家地址", "哪个离市中心近"}
	result = workspaceEnvelope(result, task, actor.ID != "", "person", workspace, role, permissions, w.Header().Get("X-Request-ID"))
	r = r.WithContext(context.WithValue(r.Context(), onlineRestaurantFollowupKey{}, true))
	w.Header().Set("Cache-Control", "no-store")
	s.respondCurrentAgentTask(w, r, result, digest, actor)
}
