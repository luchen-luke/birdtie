package socialcontext

import (
	"errors"
	"testing"
	"time"
)

func TestSharedHistoryShapeClosed(t *testing.T) {
	a := CurrentAccess{ViewerID: "11111111-1111-4111-8111-111111111111", TargetID: "22222222-2222-4222-8222-222222222222", SessionDigest: [32]byte{1}}
	now := time.Date(2026, 10, 5, 1, 0, 0, 0, time.UTC)
	good := func() Signals {
		return Signals{Schema: HistorySchema, ViewerID: a.ViewerID, TargetID: a.TargetID, ObservedAt: now, ValidUntil: now.Add(30 * time.Second), Attendance: "UNKNOWN", Visit: "UNKNOWN", Communities: []Reference{}, Activities: []Reference{}}
	}
	if ValidateCurrent(good(), a) != nil {
		t.Fatal("valid bounded empty current")
	}
	for _, kind := range []string{"owner", "target", "schema", "attendance", "visit", "expired", "extended", "null", "badid", "unrelatedPlace"} {
		t.Run(kind, func(t *testing.T) {
			v := good()
			switch kind {
			case "owner":
				v.ViewerID = a.TargetID
			case "target":
				v.TargetID = a.ViewerID
			case "schema":
				v.Schema = "legacy"
			case "attendance":
				v.Attendance = "CONFIRMED"
			case "visit":
				v.Visit = "VISITED"
			case "expired":
				v.ValidUntil = now
			case "extended":
				v.ValidUntil = now.Add(31 * time.Second)
			case "null":
				v.Activities = nil
			case "badid":
				v.Communities = []Reference{{ID: "arbitrary", Title: "社群"}}
			case "unrelatedPlace":
				v.Places = []PlaceReference{{ID: a.ViewerID, Title: "地点", ActivityIDs: []string{a.TargetID}}}
			}
			if !errors.Is(ValidateCurrent(v, a), ErrUnavailable) {
				t.Fatal("invalid shape accepted", kind)
			}
		})
	}
}
