package saved

import (
	"context"
	"errors"
	"regexp"
	"time"
	"unicode/utf8"

	"github.com/birdtie/birdtie/apps/api/internal/identity"
)

const PageSize = 20

var ErrInvalidPage = errors.New("invalid saved page")
var ErrPageChanged = errors.New("saved page cursor changed")
var ErrPageUnavailable = errors.New("saved page unavailable")
var savedPageID = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

type Page struct {
	Items         []Item  `json:"items"`
	NextCursor    *string `json:"nextCursor"`
	ResponseProof any     `json:"-"`
}

// These projections serve the current human only. The old Store/ListSaved
// remains free of private notes for existing Agent/source consumers.
type HumanListStore interface {
	ListHumanSaved(context.Context, [32]byte, identity.Actor) ([]Item, error)
}
type HumanPageStore interface {
	ListHumanSavedPage(context.Context, [32]byte, identity.Actor, string) (Page, error)
}

// A page is a visible slice, never the authority for whether another target
// is already bookmarked. Actions can resolve its original owned ID directly.
type HumanTargetStore interface {
	GetHumanSavedTarget(context.Context, [32]byte, identity.Actor, string, string) (*Item, error)
}

func ValidPageCursor(cursor string) bool {
	return cursor == "" || (savedPageID.MatchString(cursor) && cursor != "00000000-0000-0000-0000-000000000000")
}

func ValidatePage(page Page, cursor string) error {
	if !ValidPageCursor(cursor) || len(page.Items) > PageSize {
		return ErrInvalidPage
	}
	seen := map[string]bool{}
	for _, item := range page.Items {
		if !item.ValidPlacePhoto(time.Now().UTC()) || !item.ValidCover(time.Now().UTC()) || !item.ValidCommunityMedia() || !item.ValidPoint(time.Now().UTC()) || !utf8.ValidString(item.Note) || len(item.Note) > MaxNoteBytes || item.NoteRevision < 0 || item.NoteRevision > MaxNoteRevision {
			return ErrInvalidPage
		}
		if !savedPageID.MatchString(item.ID) || item.ID == "00000000-0000-0000-0000-000000000000" || item.ID == cursor || seen[item.ID] || !savedPageID.MatchString(item.TargetID) {
			return ErrInvalidPage
		}
		if item.Kind != "place" && item.Kind != "activity" && item.Kind != "group" {
			return ErrInvalidPage
		}
		if !item.Available && item.LegacyReference == nil && (item.Title != "" || item.Summary != "" || item.CityID != "") {
			return ErrInvalidPage
		}
		seen[item.ID] = true
	}
	if page.NextCursor != nil && (len(page.Items) != PageSize || !ValidPageCursor(*page.NextCursor) || *page.NextCursor == "" || *page.NextCursor != page.Items[len(page.Items)-1].ID) {
		return ErrInvalidPage
	}
	return nil
}
