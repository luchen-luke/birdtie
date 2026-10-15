package content

import (
	"math"
	"testing"
)

func TestMapContentBoundsAndSourceScopeAreClosed(t *testing.T) {
	if !(MapContentQuery{"aberdeen-gb", MapContentBounds{-2.4, 57, -1.9, 57.3}}).Valid() {
		t.Fatal("valid query rejected")
	}
	if (MapContentBounds{math.NaN(), 0, 1, 1}).Valid() || (MapContentBounds{-2, 0, -3, 1}).Valid() {
		t.Fatal("nonfinite/reversed bounds accepted")
	}
	if MapContentVisibility("PUBLIC") || !MapContentVisibility("CIVU_PUBLIC_MOMENT") {
		t.Fatal("legacy grant confused with native publication")
	}
}
