package agentworkspace

import (
	"reflect"
	"testing"
)

func TestTermsFiltersCommonWordsAndDuplicates(t *testing.T) {
	got := Terms("Find someone to play Badminton, badminton this weekend near Aberdeen!")
	want := []string{"badminton", "aberdeen"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Terms() = %v, want %v", got, want)
	}
}

func TestTermsKeepsChineseTextAndLimitsTerms(t *testing.T) {
	got := Terms("羽毛球 周末 朋友 a b c d e f g")
	want := []string{"羽毛球", "周末", "朋友"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Terms() = %v, want %v", got, want)
	}
	if len(Terms("alpha beta gamma delta epsilon zeta eta theta")) != 6 {
		t.Fatal("Terms() must cap query terms")
	}
}
