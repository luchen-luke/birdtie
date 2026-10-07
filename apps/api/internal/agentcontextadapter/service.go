package agentcontextadapter

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/birdtie/birdtie/apps/api/internal/actorref"
	"github.com/birdtie/birdtie/apps/api/internal/agentcognitive"
	"github.com/birdtie/birdtie/apps/api/internal/agentconfidence"
	acb "github.com/birdtie/birdtie/apps/api/internal/agentcontextbuilder"
	"github.com/birdtie/birdtie/apps/api/internal/agentevent"
	"github.com/birdtie/birdtie/apps/api/internal/agentruntime"
)

type candidate struct {
	kind, id string
	fields   []string
	fact     *Fact
	place    *acb.PublicPlace
	activity *acb.PublicActivity
	tie      *acb.ContextTie
	source   acb.Source
}

func sourceMap(b acb.Bundle) (map[string]acb.Source, error) {
	out := map[string]acb.Source{}
	for _, s := range b.Sources {
		if s.ID == "" || !utf8.ValidString(s.ID) || !acb.ValidTime(s.NativeTime) || s.NativeTime.After(b.ObservedAt) {
			return nil, ErrInvalid
		}
		switch s.Kind {
		case "CURRENT_TASK_REQUEST", "PUBLIC_CITY", "PUBLIC_PLACE", "PUBLIC_ACTIVITY", "PURPOSE_RELATIONSHIP_TIE":
			raw, e := hex.DecodeString(s.Version.Token)
			if s.Version.Kind != agentevent.UpdatedAtDigestVersion || s.Version.Revision != 0 || e != nil || len(raw) != 32 || hex.EncodeToString(raw) != s.Version.Token {
				return nil, ErrInvalid
			}
		case "PURPOSE_PRIVATE_PROFILE", "PURPOSE_EXPLICIT_MEMORY", "PURPOSE_POLICY_SETTINGS":
			if s.Version.Kind != agentevent.RevisionVersion || s.Version.Revision <= 0 || s.Version.Token != "" {
				return nil, ErrInvalid
			}
		default:
			return nil, ErrInvalid
		}
		s.RowToken = ""
		key := s.Kind + "\x00" + s.ID
		if _, ok := out[key]; ok {
			return nil, ErrInvalid
		}
		out[key] = s
	}
	return out, nil
}
func provenance(c candidate) Provenance {
	origin := "NATIVE_DOMAIN_DATA"
	if c.fact != nil {
		origin = "UNTRUSTED_DECLARED_DATA"
	}
	return Provenance{Kind: c.kind, ID: c.id, Fields: c.fields, Origin: origin, Source: c.source}
}
func add(v View, c candidate) View {
	// Each tentative choice is independent; no slice/map aliases into the prior
	// budget candidate or native Bundle may change a retained result.
	v.Facts = append([]Fact{}, v.Facts...)
	v.Places = append([]acb.PublicPlace{}, v.Places...)
	v.Activities = append([]acb.PublicActivity{}, v.Activities...)
	v.Relationships = append([]acb.ContextTie{}, v.Relationships...)
	v.Sources = append([]acb.Source{}, v.Sources...)
	v.Provenance = append([]Provenance{}, v.Provenance...)
	v.evidenceSelectors = append([]acb.FieldEvidenceSelector{}, v.evidenceSelectors...)
	if c.fact != nil {
		f := *c.fact
		f.DeclaredSettings = append(json.RawMessage(nil), f.DeclaredSettings...)
		if f.Confidence != nil {
			a, _ := agentconfidence.NormalizeAssessment(*f.Confidence)
			f.Confidence = &a
		}
		v.Facts = append(v.Facts, f)
	}
	if c.place != nil {
		v.Places = append(v.Places, *c.place)
	}
	if c.activity != nil {
		v.Activities = append(v.Activities, *c.activity)
	}
	if c.tie != nil {
		v.Relationships = append(v.Relationships, *c.tie)
	}
	seen := false
	for _, s := range v.Sources {
		if s.Kind == c.source.Kind && s.ID == c.source.ID {
			seen = true
		}
	}
	if !seen {
		v.Sources = append(v.Sources, c.source)
	}
	v.Provenance = append(v.Provenance, provenance(c))
	if v.evidenceInput != nil {
		field := ""
		if c.kind == "profile" && len(c.fields) == 1 {
			field = c.fields[0]
		}
		v.evidenceSelectors = append(v.evidenceSelectors, acb.FieldEvidenceSelector{Kind: c.kind, ID: c.id, Field: field})
		var err error
		v.FieldEvidenceSet, err = acb.FilterFieldEvidence(v.evidenceInput, v.evidenceSelectors, "BUDGETED_CONTEXT")
		if err != nil {
			return View{}
		}
	}
	return v
}
func countFacts(v View, kind string) int {
	n := 0
	for _, f := range v.Facts {
		if f.Kind == kind {
			n++
		}
	}
	return n
}
func finalize(v *View, totals map[string]int, filtered, limited map[string]int) bool {
	if v.evidenceInput != nil && (v.FieldEvidenceSet == nil || acb.ValidateFieldEvidenceShape(*v.FieldEvidenceSet) != nil) {
		return false
	}
	v.Sections = map[string]string{}
	v.Budget.Omitted = map[string]int{}
	if filtered != nil {
		v.Budget.OmissionReasons = map[string]map[string]int{}
	}
	counts := map[string]int{"profile": countFacts(*v, "DECLARED_PROFILE"), "memories": countFacts(*v, "EXPLICIT_MEMORY"), "policies": countFacts(*v, "DECLARED_POLICY"), "places": len(v.Places), "activities": len(v.Activities), "relationships": len(v.Relationships)}
	for k, total := range totals {
		n := counts[k]
		status := Available
		if total == 0 {
			status = NotRequested
			if v.RelevanceExcluded[k] > 0 {
				status = NotRelevant
			}
		} else if n == 0 {
			status = OmittedBudget
			if filtered[k] == total {
				status = OmittedConfidence
			} else if limited[k] == total {
				status = OmittedLimit
			} else if filtered[k]+limited[k] > 0 {
				status = OmittedControls
			}
		}
		v.Sections[k] = status
		v.Budget.Omitted[k] = total - n
		if filtered != nil && total-n > 0 {
			reasons := map[string]int{}
			if filtered[k] > 0 {
				reasons[OmittedConfidence] = filtered[k]
			}
			if limited[k] > 0 {
				reasons[OmittedLimit] = limited[k]
			}
			if left := total - n - filtered[k] - limited[k]; left > 0 {
				reasons[OmittedBudget] = left
			}
			v.Budget.OmissionReasons[k] = reasons
		}
	}
	if counts["profile"] > 0 {
		unknown := 0
		for _, f := range v.Facts {
			if f.Kind == "DECLARED_PROFILE" && f.ValueStatus == Unknown {
				unknown++
			}
		}
		if unknown == counts["profile"] {
			v.Sections["profile"] = Unknown
		}
	}
	v.Answer = fmt.Sprintf("本轮任务是「%s」。已按当前用途与输入限额整理 %d 项资料、%d 条明确记录、%d 个地点和 %d 个活动；%d 条关系仅表示目前的好友状态。城市时区为 %s。未填写的内容标为未知，未纳入的内容不作判断。这些信息不代表到访、出席或新的承诺。", v.Task.Query, counts["profile"], counts["memories"], counts["places"], counts["activities"], counts["relationships"], v.City.TimeZone)
	if filtered != nil {
		v.Answer += " 条目上限、类别优先级和近期权重只决定本轮纳入顺序；置信门槛仅用于有来源的明确记录，缺值不会补为1。明确记录的1表示本人声明，不是正确概率。"
	}
	v.Budget.Used = 0
	for i := 0; i < 8; i++ {
		raw, e := json.Marshal(v)
		if e != nil {
			return false
		}
		n := len(raw)
		if v.Budget.Used == n {
			return n <= v.Budget.Limit
		}
		v.Budget.Used = n
	}
	return false
}

// Project only bounds already-selected data. Shape checks are not authorization.
// The actual caller must Build and finally Revalidate the complete sealed native
// source set, including sources omitted by relevance or budget, before returning.
func Project(b acb.Bundle, budget Budget) (View, error) {
	return project(b, budget, nil)
}

// ProjectRelated accepts server-produced count-only selection metadata. These
// counts explain omission; they are not source references or permissions.
func ProjectRelated(b acb.Bundle, budget Budget, excluded map[string]int) (View, error) {
	return project(b, budget, excluded)
}

func project(b acb.Bundle, budget Budget, excluded map[string]int) (View, error) {
	if acb.FieldEvidenceMatchesBundle(b) != nil {
		return View{}, ErrInvalid
	}
	itemLimit, priority, controls, e := normalizeBudget(budget)
	if e != nil {
		return View{}, e
	}
	if agentcognitive.ValidateAgentReference(b.Agent) != nil || b.Agent.Principal.Type != actorref.Person || b.Agent.Role != agentruntime.PersonalAgent || len(b.Sources) > 20 || !acb.ValidTime(b.ExpiresAt) || b.Task != nil && (!acb.ValidTime(b.Task.UpdatedAt) || b.Task.UpdatedAt.After(b.ObservedAt)) {
		return View{}, ErrInvalid
	}
	if budget.MaxEncodedBytes < 1 || budget.MaxEncodedBytes > MaxEncodedBytes || b.SchemaVersion != acb.SchemaVersion || b.Mode != acb.MachineTaskContext || b.ModelAccess != "UNAVAILABLE" || b.MemoryPromotionAllowed || b.City == nil || b.Task == nil || b.City.ID != b.CityID || b.Task.ID != b.TaskID || b.Task.Query != b.CurrentQuery || b.Task.UpdatedAt.IsZero() || !utf8.ValidString(b.CurrentQuery) || len(b.CurrentQuery) > 240 || strings.TrimSpace(b.CurrentQuery) == "" || !acb.ValidTime(b.ObservedAt) || !b.ExpiresAt.After(b.ObservedAt) || b.ExpiresAt.Sub(b.ObservedAt) > acb.MaxLease || len(b.Profile) > 3 || len(b.Memories) > 3 || len(b.Policies) > 1 || len(b.Places) > 5 || len(b.Activities) > 5 || len(b.Relationships) > 3 {
		return View{}, ErrInvalid
	}
	sources, e := sourceMap(b)
	if e != nil {
		return View{}, e
	}
	required := func(kind, id string) (acb.Source, error) {
		s, ok := sources[kind+"\x00"+id]
		if !ok {
			return acb.Source{}, ErrInvalid
		}
		return s, nil
	}
	taskSource, e := required("CURRENT_TASK_REQUEST", b.TaskID)
	if e != nil {
		return View{}, e
	}
	citySource, e := required("PUBLIC_CITY", b.CityID)
	if e != nil {
		return View{}, e
	}
	totals := map[string]int{"profile": len(b.Profile), "memories": len(b.Memories), "policies": len(b.Policies), "places": len(b.Places), "activities": len(b.Activities), "relationships": len(b.Relationships)}
	statuses := map[string]string{"profile": b.Sections.Profile, "memories": b.Sections.Memories, "policies": b.Sections.Policies, "places": b.Sections.Places, "activities": b.Sections.Activities, "relationships": b.Sections.Relationships}
	limits := map[string]int{"profile": 3, "memories": 3, "policies": 1, "places": 5, "activities": 5, "relationships": 3}
	relevanceExcluded := map[string]int{}
	for key, n := range excluded {
		limit, ok := limits[key]
		if !ok || n < 0 || n > limit || totals[key]+n > limit || key == "policies" && n != 0 {
			return View{}, ErrInvalid
		}
		relevanceExcluded[key] = n
	}
	for key, n := range totals {
		status := statuses[key]
		if status != "" && status != Available && status != NotRequested && status != NotRelevant {
			return View{}, ErrInvalid
		}
		if status == NotRequested && n > 0 || status == NotRelevant && (n != 0 || excluded == nil || excluded[key] <= 0) || n == 0 && excluded[key] > 0 && status != NotRelevant {
			return View{}, ErrInvalid
		}
		if _, ok := relevanceExcluded[key]; !ok {
			relevanceExcluded[key] = 0
		}
	}
	v := View{SchemaVersion: "agent-task-context-response-v1", AdapterVersion: Version, Purpose: acb.TaskContextRead, TaskID: b.TaskID, Task: *b.Task, City: *b.City, Facts: []Fact{}, Places: []acb.PublicPlace{}, Activities: []acb.PublicActivity{}, Relationships: []acb.ContextTie{}, Sources: []acb.Source{}, Provenance: []Provenance{}, ObservedAt: b.ObservedAt, ExpiresAt: b.ExpiresAt, ModelAccess: "UNAVAILABLE", Budget: BudgetResult{Unit: BudgetUnit, Limit: budget.MaxEncodedBytes, TokenCountStatus: "UNKNOWN_NOT_TOKENIZED"}}
	if b.FieldEvidenceSet != nil {
		v.evidenceInput = b.FieldEvidenceSet
		v.evidenceSelectors = []acb.FieldEvidenceSelector{{Kind: "task", ID: b.TaskID}, {Kind: "city", ID: b.CityID}}
		v.FieldEvidenceSet, e = acb.FilterFieldEvidence(b.FieldEvidenceSet, v.evidenceSelectors, "BUDGETED_CONTEXT")
		if e != nil {
			return View{}, ErrInvalid
		}
	}
	if controls {
		v.Budget.ItemLimit = &itemLimit
		v.Budget.Priority = append([]string(nil), priority...)
		v.Budget.RecencyWeight = budget.RecencyWeight
		if budget.ConfidenceThreshold != nil {
			value := *budget.ConfidenceThreshold
			v.Budget.ConfidenceThreshold = &value
		}
	}
	v.RelevanceExcluded = relevanceExcluded
	v = add(v, candidate{kind: "task", id: b.TaskID, fields: []string{"query", "updatedAt"}, source: taskSource})
	v = add(v, candidate{kind: "city", id: b.CityID, fields: []string{"timeZone"}, source: citySource})
	for _, p := range b.Policies {
		source, e := required("PURPOSE_POLICY_SETTINGS", b.Agent.AgentID+":"+string(p.Family))
		if e != nil || source.Version.Revision != p.NativeRevision || !json.Valid(p.Settings) || string(p.Settings) == "null" {
			return View{}, ErrInvalid
		}
		label := map[string]string{"ATTENTION": "注意范围", "SOCIAL": "社交偏好", "AUTONOMY": "自主操作边界"}[string(p.Family)]
		if label == "" {
			return View{}, ErrInvalid
		}
		text := "本轮读取你当前的" + label + "设置；任何提交仍需单独授权"
		if string(p.Family) == "AUTONOMY" {
			var setting struct {
				Level string `json:"level"`
			}
			if json.Unmarshal(p.Settings, &setting) != nil {
				return View{}, ErrInvalid
			}
			level := map[string]string{"LEVEL_0_OBSERVE": "仅观察", "LEVEL_1_ASSIST": "提供建议", "LEVEL_2_PREPARE": "准备待确认内容", "LEVEL_3_DELEGATE": "委托范围"}[setting.Level]
			if level == "" {
				return View{}, ErrInvalid
			}
			text = "你当前的自主操作边界为「" + level + "」；本轮只整理信息，不执行或提交操作"
		}
		f := Fact{Kind: "DECLARED_POLICY", SourceID: source.ID, Text: text, ValueStatus: Available, DeclaredSettings: p.Settings}
		v = add(v, candidate{kind: "policies", id: source.ID, fields: []string{"settings"}, fact: &f, source: source})
	}
	candidates := []candidate{}
	labels := map[string]string{"personalPreferences": "你填写的个人偏好", "socialPreferences": "你填写的社交偏好", "availability": "你填写的可用时间", "preferredActivityTypes": "你填写的活动类型", "travelPreferences": "你填写的出行偏好", "interactionPreferences": "你填写的互动偏好", "privateCityHistory": "你填写的城市记录", "languagePreferences": "你填写的语言偏好", "agentNotes": "你填写的补充说明"}
	processed := 0
	for _, key := range acb.PrivateFieldKeys() {
		raw, ok := b.Profile[key]
		if !ok {
			continue
		}
		processed++
		source, e := required("PURPOSE_PRIVATE_PROFILE", b.Agent.AgentID)
		if e != nil {
			return View{}, e
		}
		var text string
		if len(raw) == 0 || string(raw) == "null" {
			return View{}, ErrInvalid
		}
		if json.Unmarshal(raw, &text) != nil {
			var list []string
			if json.Unmarshal(raw, &list) != nil || list == nil {
				return View{}, ErrInvalid
			}
			text = strings.Join(list, "、")
		}
		if !utf8.ValidString(text) {
			return View{}, ErrInvalid
		}
		state := Available
		if strings.TrimSpace(text) == "" {
			state = Unknown
			text = "尚未填写（未知）"
		}
		f := Fact{Kind: "DECLARED_PROFILE", SourceID: b.Agent.AgentID, Field: key, Text: labels[key] + "：" + text, ValueStatus: state}
		candidates = append(candidates, candidate{kind: "profile", id: b.Agent.AgentID, fields: []string{key}, fact: &f, source: source})
	}
	if processed != len(b.Profile) {
		return View{}, ErrInvalid
	}
	seen := map[string]bool{}
	for _, m := range b.Memories {
		source, e := required("PURPOSE_EXPLICIT_MEMORY", m.ID)
		if e != nil || source.Version.Revision != m.Version || !m.ValidUntil.After(b.ObservedAt) || b.ExpiresAt.After(m.ValidUntil) || !utf8.ValidString(m.Summary) || seen["memory:"+m.ID] {
			return View{}, ErrInvalid
		}
		seen["memory:"+m.ID] = true
		var confidence *agentconfidence.Assessment
		if m.Confidence != nil {
			a, e := agentconfidence.NormalizeAssessment(*m.Confidence)
			if e != nil || a.Semantics != agentconfidence.DirectDeclaration {
				return View{}, ErrInvalid
			}
			confidence = &a
		}
		f := Fact{Kind: "EXPLICIT_MEMORY", SourceID: m.ID, Text: "你明确记录的内容：" + m.Summary, ValueStatus: Available, Confidence: confidence}
		candidates = append(candidates, candidate{kind: "memories", id: m.ID, fields: []string{"summary"}, fact: &f, source: source})
	}
	for _, p := range b.Places {
		source, e := required("PUBLIC_PLACE", p.ID)
		if e != nil || seen["place:"+p.ID] {
			return View{}, ErrInvalid
		}
		seen["place:"+p.ID] = true
		item := p
		candidates = append(candidates, candidate{kind: "places", id: p.ID, fields: []string{"name", "category"}, place: &item, source: source})
	}
	for _, a := range b.Activities {
		source, e := required("PUBLIC_ACTIVITY", a.ID)
		if e != nil || seen["activity:"+a.ID] {
			return View{}, ErrInvalid
		}
		seen["activity:"+a.ID] = true
		item := a
		candidates = append(candidates, candidate{kind: "activities", id: a.ID, fields: []string{"title", "category", "StartsAt", "EndsAt", "OrganizerType", "OrganizerID"}, activity: &item, source: source})
	}
	for _, t := range b.Relationships {
		source, e := required("PURPOSE_RELATIONSHIP_TIE", t.ID)
		if e != nil || t.State != "ACCEPTED" || seen["tie:"+t.ID] {
			return View{}, ErrInvalid
		}
		seen["tie:"+t.ID] = true
		item := t
		candidates = append(candidates, candidate{kind: "relationships", id: t.ID, fields: []string{"peerAccountId", "state"}, tie: &item, source: source})
	}
	var filtered, limited map[string]int
	if controls {
		filtered = map[string]int{}
		limited = map[string]int{}
	}
	eligible := make([]candidate, 0, len(candidates))
	for _, c := range candidates {
		if c.kind == "memories" && budget.ConfidenceThreshold != nil && (c.fact.Confidence == nil || c.fact.Confidence.Value == nil || *c.fact.Confidence.Value < *budget.ConfidenceThreshold) {
			filtered[c.kind]++
			continue
		}
		eligible = append(eligible, c)
	}
	// Rank only already-related selected entries. Times are retained native source
	// update times relative to the Builder's final PostgreSQL observation, not
	// attendance, future event times or the process wall clock.
	if controls {
		rank := map[string]int{}
		for i, key := range priority {
			rank[key] = i
		}
		score := func(c candidate) float64 {
			base := float64(len(priority)-1-rank[c.kind]) / float64(len(priority)-1)
			// Unix seconds cover the validated year range without Duration's
			// roughly 292-year subtraction saturation.
			ageSeconds := float64(b.ObservedAt.Unix()-c.source.NativeTime.Unix()) + float64(b.ObservedAt.Nanosecond()-c.source.NativeTime.Nanosecond())/1e9
			ageDays := ageSeconds / 86400
			recent := 1 / (1 + ageDays/30)
			return (1-budget.RecencyWeight)*base + budget.RecencyWeight*recent
		}
		sort.SliceStable(eligible, func(i, j int) bool {
			a, z := eligible[i], eligible[j]
			if x, y := score(a), score(z); x != y {
				return x > y
			}
			if rank[a.kind] != rank[z.kind] {
				return rank[a.kind] < rank[z.kind]
			}
			// The upstream family order already resolves relevance then ID.
			// Preserve that order for equal scores rather than ranking by ID
			// again when only limit or confidence controls were requested.
			return false
		})
	}
	if itemLimit == 0 {
		for _, c := range eligible {
			limited[c.kind]++
		}
		eligible = nil
	}
	if !finalize(&v, totals, filtered, limited) {
		return View{}, ErrBudget
	}
	retained := 0
	anchors := v
	selected := []candidate{}
	for _, c := range eligible {
		if retained >= itemLimit {
			limited[c.kind]++
			continue
		}
		next := add(v, c)
		if finalize(&next, totals, filtered, limited) {
			v = next
			retained++
			selected = append(selected, c)
		}
	}
	// Reasons may grow after an earlier successful tentative addition. Rebuild
	// from mandatory anchors and withdraw whole lowest-ranked retained units
	// until the final envelope fits. Omitted items cannot survive as old facts,
	// source IDs or a stale Used value. If none fit, all remaining omissions are
	// due to bytes, since the configured item limit is no longer reached.
	for !finalize(&v, totals, filtered, limited) {
		if len(selected) == 0 {
			return View{}, ErrBudget
		}
		selected = selected[:len(selected)-1]
		if limited != nil {
			limited = map[string]int{}
		}
		v = anchors
		for _, c := range selected {
			v = add(v, c)
		}
	}
	return v, nil
}

func normalizeBudget(b Budget) (int, []string, bool, error) {
	defaults := []string{"profile", "memories", "places", "activities", "relationships"}
	limit := MaxOptionalItems
	if b.ItemLimit != nil {
		limit = *b.ItemLimit
	}
	if limit < 0 || limit > MaxOptionalItems || math.IsNaN(b.RecencyWeight) || math.IsInf(b.RecencyWeight, 0) || b.RecencyWeight < 0 || b.RecencyWeight > 1 {
		return 0, nil, false, ErrInvalid
	}
	if b.ConfidenceThreshold != nil && (math.IsNaN(*b.ConfidenceThreshold) || math.IsInf(*b.ConfidenceThreshold, 0) || *b.ConfidenceThreshold < 0 || *b.ConfidenceThreshold > 1) {
		return 0, nil, false, ErrInvalid
	}
	priority := defaults
	if b.Priority != nil {
		if len(b.Priority) != len(defaults) {
			return 0, nil, false, ErrInvalid
		}
		seen := map[string]bool{}
		for _, k := range b.Priority {
			valid := false
			for _, allowed := range defaults {
				if k == allowed {
					valid = true
				}
			}
			if !valid || seen[k] {
				return 0, nil, false, ErrInvalid
			}
			seen[k] = true
		}
		priority = append([]string(nil), b.Priority...)
	}
	return limit, priority, b.ItemLimit != nil || b.Priority != nil || b.ConfidenceThreshold != nil || b.RecencyWeight != 0, nil
}
