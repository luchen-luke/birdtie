package connection

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

const contactOwner = "11111111-1111-4111-8111-111111111111"
const contactPeer = "22222222-2222-4222-8222-222222222222"
const contactID = "33333333-3333-4333-8333-333333333333"

func TestContactPageCursorOwnerKindSnapshotAndCanonicalBoundary(t *testing.T) {
	stamp := time.Date(2026, 10, 13, 0, 0, 0, 0, time.UTC)
	c := ContactCursor{1, contactOwner, ContactTies, stamp, stamp.Add(-time.Second), contactID}
	raw, e := EncodeContactCursor(c)
	if e != nil {
		t.Fatal(e)
	}
	back, e := DecodeContactCursor(raw, contactOwner, ContactTies)
	if e != nil || *back != c {
		t.Fatal("cursor lost boundary")
	}
	for _, pair := range [][2]string{{contactPeer, ContactTies}, {contactOwner, ContactFollows}, {contactOwner, ContactRequests}} {
		if _, e = DecodeContactCursor(raw, pair[0], pair[1]); !errors.Is(e, ErrInvalidContactPage) {
			t.Fatal("foreign cursor accepted")
		}
	}
	bytes, _ := base64.RawURLEncoding.DecodeString(raw)
	for _, bad := range []string{strings.TrimSuffix(string(bytes), "}") + `,"grant":true}`, strings.TrimSuffix(string(bytes), "}") + `,"owner":"` + contactOwner + `"}`, string(bytes) + " ", strings.Replace(string(bytes), `"v":1`, `"v":2`, 1), strings.Replace(string(bytes), "Z\"", "+00:00\"", 1)} {
		if _, e = DecodeContactCursor(base64.RawURLEncoding.EncodeToString([]byte(bad)), contactOwner, ContactTies); e == nil {
			t.Fatal("noncanonical cursor accepted")
		}
	}
	c.Time = stamp.Add(time.Second)
	if _, e = EncodeContactCursor(c); e == nil {
		t.Fatal("boundary newer than snapshot")
	}
	for _, limit := range []int{-1, 51, 1000} {
		if _, _, e = NormalizeContactPage(PageRequest{Limit: limit}, contactOwner, ContactTies); e == nil {
			t.Fatal("unbounded page")
		}
	}
	q, cursor, e := NormalizeContactPage(PageRequest{}, contactOwner, ContactTies)
	if e != nil || q.Limit != 20 || cursor != nil {
		t.Fatal("default first20")
	}
	if _, e = DecodeContactCursor(strings.Repeat("A", 1025), contactOwner, ContactTies); e == nil {
		t.Fatal("oversize cursor")
	}
}
func TestContactPageWireAndBoundaryRejectsDuplicatesAndWrongNext(t *testing.T) {
	stamp := time.Date(2026, 10, 13, 0, 0, 0, 0, time.UTC)
	row := Tie{ID: contactID, OtherAccountID: contactPeer, OtherName: "合成联系人", CreatedAt: stamp.Add(-time.Second)}
	key := func(x Tie) (string, time.Time, bool) { return ContactTieRow(x, contactOwner) }
	raw, _ := EncodeContactCursor(ContactCursor{1, contactOwner, ContactTies, stamp, row.CreatedAt, row.ID})
	page := ContactPage[Tie]{Items: []Tie{row}, NextCursor: &raw, ResponseProof: "never-wire"}
	if e := ValidateContactPage(page, PageRequest{Limit: 1}, contactOwner, ContactTies, key); e != nil {
		t.Fatal(e)
	}
	data, _ := json.Marshal(page)
	var fields map[string]json.RawMessage
	if json.Unmarshal(data, &fields) != nil || len(fields) != 2 || fields["items"] == nil || fields["nextCursor"] == nil || strings.Contains(string(data), "never-wire") {
		t.Fatal("closed wire shape")
	}
	page.Items = append(page.Items, row)
	if ValidateContactPage(page, PageRequest{Limit: 2}, contactOwner, ContactTies, key) == nil {
		t.Fatal("duplicate accepted")
	}
	page.Items = []Tie{}
	if ValidateContactPage(page, PageRequest{Limit: 1}, contactOwner, ContactTies, key) == nil {
		t.Fatal("empty next cursor")
	}
	page.Items = []Tie{row}
	if ValidateContactPage(page, PageRequest{Limit: 1, Cursor: raw}, contactOwner, ContactTies, key) == nil {
		t.Fatal("non-progressing boundary")
	}
}
