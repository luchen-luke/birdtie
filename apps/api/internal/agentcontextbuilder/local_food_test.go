package agentcontextbuilder

import "testing"

func TestLocalFoodUsesOnlyCurrentTypedDeclarationsAndKnownFields(t *testing.T) {
	p := []LocalFoodPreference{{MemoryID: "native-memory", Version: 2, Topic: "seafood", Value: "LIKE"}, {MemoryID: "native-negative", Version: 1, Topic: "spicy", Value: "DISLIKE"}}
	score, excluded, used := ScoreLocalFood(LocalFoodFacts{Seafood: "yes", Spicy: "no"}, p)
	if score != 3 || excluded || len(used) != 2 || used[0].Version != 2 {
		t.Fatal(score, excluded, used)
	}
	_, excluded, _ = ScoreLocalFood(LocalFoodFacts{Seafood: "yes", Spicy: "yes"}, p)
	if !excluded {
		t.Fatal("explicit exclusion ignored")
	}
	score, excluded, used = ScoreLocalFood(LocalFoodFacts{Seafood: "UNKNOWN", Spicy: ""}, p)
	if score != 0 || excluded || len(used) != 0 {
		t.Fatal("unknown field invented")
	}
	if s, excluded, u := ScoreLocalFood(LocalFoodFacts{Seafood: "yes"}, nil); s != 0 || excluded || len(u) != 0 {
		t.Fatal("missing memory made preference")
	}
}

func TestLocalFoodDeletedCorrectedAndUnrelatedCannotContribute(t *testing.T) {
	for _, p := range []LocalFoodPreference{{MemoryID: "", Version: 1, Topic: "seafood", Value: "LIKE"}, {MemoryID: "old", Version: 0, Topic: "seafood", Value: "LIKE"}, {MemoryID: "other", Version: 2, Topic: "health", Value: "LIKE"}, {MemoryID: "hypothesis", Version: 1, Topic: "seafood", Value: "INFERRED"}} {
		if score, excluded, used := ScoreLocalFood(LocalFoodFacts{Seafood: "yes"}, []LocalFoodPreference{p}); score != 0 || excluded || len(used) != 0 {
			t.Fatal("invalid projection contributed")
		}
	}
	if _, excluded, _ := ScoreLocalFood(LocalFoodFacts{Seafood: "yes"}, []LocalFoodPreference{{MemoryID: "corrected", Version: 3, Topic: "seafood", Value: "DISLIKE"}}); !excluded {
		t.Fatal("correction ignored")
	}
}
