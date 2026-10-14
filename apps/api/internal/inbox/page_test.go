package inbox

import (
	"encoding/base64"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestInboxPageCursorClosedOwnerViewAndSortHint(t *testing.T) {
	owner := "11111111-1111-4111-8111-111111111111"
	id := "22222222-2222-4222-8222-222222222222"
	original := PageCursor{Version: 1, OwnerID: owner, View: NotificationsView, Priority: 50, Time: time.Date(2026, 10, 12, 0, 1, 2, 123000, time.UTC), ID: id}
	raw, err := EncodePageCursor(original)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := DecodePageCursor(raw, owner, NotificationsView)
	if err != nil || parsed == nil || !reflect.DeepEqual(*parsed, original) {
		t.Fatal("cursor lost exact sort tuple", err)
	}
	bytes, _ := base64.RawURLEncoding.DecodeString(raw)
	for _, value := range []string{strings.TrimSuffix(string(bytes), "}") + `,"grant":true}`, strings.TrimSuffix(string(bytes), "}") + `,"owner":"` + owner + `"}`, string(bytes) + " ", strings.Replace(string(bytes), `"v":1`, `"v":2`, 1), strings.Replace(string(bytes), `"priority":50`, `"priority":51`, 1), strings.Replace(string(bytes), "Z\"", "+00:00\"", 1)} {
		if _, err = DecodePageCursor(base64.RawURLEncoding.EncodeToString([]byte(value)), owner, NotificationsView); !errors.Is(err, ErrInvalidPage) {
			t.Fatal("unknown/noncanonical hint accepted")
		}
	}
	for _, q := range []struct{ raw, owner, view string }{{raw, id, NotificationsView}, {raw, owner, "messages"}, {raw + "=", owner, NotificationsView}, {strings.Repeat("a", 1025), owner, NotificationsView}} {
		if _, err = DecodePageCursor(q.raw, q.owner, q.view); !errors.Is(err, ErrInvalidPage) {
			t.Fatal("foreign/unbounded hint accepted")
		}
	}
	for _, limit := range []int{-1, 51, 10000} {
		if _, _, err = NormalizePage(PageRequest{View: NotificationsView, Limit: limit}, owner); !errors.Is(err, ErrInvalidPage) {
			t.Fatal("unbounded page")
		}
	}
	in, cursor, err := NormalizePage(PageRequest{View: NotificationsView}, owner)
	if err != nil || in.Limit != 20 || cursor != nil {
		t.Fatal("default first20")
	}
	if _, _, err = NormalizePage(PageRequest{}, owner); !errors.Is(err, ErrInvalidPage) {
		t.Fatal("view must be explicit")
	}
}
