package agentcontextbuilder

// LocalFoodPreference is a projection of one current owner declaration under
// its explicit LOCAL_NOW_RECOMMENDATION scope. It is never a model payload,
// public CityContext, shared cache entry or approval of an external action.
type LocalFoodPreference struct {
	MemoryID string `json:"memoryId"`
	Version  int64  `json:"version"`
	Topic    string `json:"topic"`
	Value    string `json:"value"`
}

type LocalFoodFacts struct {
	Seafood, Spicy string
}

// ScoreLocalFood compares only independently permitted structured Place
// fields. Unknown facts stay unknown; an explicit exclusion is not guessed
// from a restaurant name, photos or a private user's behaviour.
func ScoreLocalFood(f LocalFoodFacts, preferences []LocalFoodPreference) (score int, excluded bool, used []LocalFoodPreference) {
	used = []LocalFoodPreference{}
	for _, p := range preferences {
		if p.MemoryID == "" || p.Version < 1 {
			continue
		}
		var fact string
		if p.Topic == "seafood" {
			fact = f.Seafood
		} else if p.Topic == "spicy" {
			fact = f.Spicy
		} else {
			continue
		}
		if fact != "yes" && fact != "no" {
			continue
		}
		if p.Value != "LIKE" && p.Value != "DISLIKE" {
			continue
		}
		used = append(used, p)
		if p.Value == "DISLIKE" && fact == "yes" {
			excluded = true
		}
		if p.Value == "LIKE" && fact == "yes" {
			score += 2
		}
		if p.Value == "DISLIKE" && fact == "no" {
			score++
		}
	}
	return
}
