package saved

import (
	"fmt"
	"testing"
)

func TestSavedPageBoundsDuplicateAndUnavailableProjection(t *testing.T) {
	page := Page{Items: []Item{}}
	for i := 1; i <= PageSize; i++ {
		page.Items = append(page.Items, Item{ID: fmt.Sprintf("10000000-0000-0000-0000-%012d", i), TargetID: fmt.Sprintf("20000000-0000-0000-0000-%012d", i), Kind: "place", Available: true})
	}
	last := page.Items[PageSize-1].ID
	page.NextCursor = &last
	if ValidatePage(page, "") != nil {
		t.Fatal("bounded original IDs refused")
	}
	duplicate := page
	duplicate.Items = append([]Item{}, page.Items...)
	duplicate.Items[1] = duplicate.Items[0]
	if ValidatePage(duplicate, "") == nil {
		t.Fatal("duplicate saved ID accepted")
	}
	huge := page
	huge.Items = append(append([]Item{}, page.Items...), page.Items[0])
	if ValidatePage(huge, "") == nil {
		t.Fatal("unbounded page accepted")
	}
	stale := page
	stale.Items = append([]Item{}, page.Items...)
	stale.Items[0].Available = false
	stale.Items[0].Title = "withdrawn source title"
	if ValidatePage(stale, "") == nil {
		t.Fatal("unavailable source leaked title")
	}
	stale.Items[0].Title = ""
	stale.Items[0].Note = "本人私人备注"
	if ValidatePage(stale, "") != nil {
		t.Fatal("owner note incorrectly treated as source visibility")
	}
	if ValidatePage(page, page.Items[0].ID) == nil {
		t.Fatal("cursor row repeated")
	}
	foreign := page.Items[0].ID
	page.NextCursor = &foreign
	if ValidatePage(page, "") == nil {
		t.Fatal("cursor not bound to last displayed row")
	}
	for _, cursor := range []string{"x", " 10000000-0000-0000-0000-000000000001", "00000000-0000-0000-0000-000000000000"} {
		if ValidPageCursor(cursor) {
			t.Fatal("invalid cursor", cursor)
		}
	}
}
