package content

import (
	"encoding/json"
	mm "github.com/birdtie/birdtie/apps/api/internal/momentmedia"
	"strings"
	"testing"
	"time"
)

func TestMomentUnplacedPrivateDraftBoundaries(t *testing.T) {
	input := MomentInput{CityID: "  ", Title: " 无已知城市的旧记录 ", Body: " 本人正文 ", TimePrecision: "unknown", LocationPrecision: "none"}
	normalized, err := NormalizeMomentInput(input)
	if err != nil || normalized.CityID != "" || normalized.Title != "无已知城市的旧记录" || normalized.PlaceID != "" {
		t.Fatal("private unknown city was rejected or replaced", err)
	}
	for _, action := range []string{"CREATE", "UPDATE"} {
		in := MomentControlInput{OperationID: momentControlTestOp, Action: action, Input: &normalized}
		if action == "UPDATE" {
			in.MomentID, in.ExpectedRevision = momentControlTestID, 1
		}
		raw, _ := json.Marshal(in)
		out, e := DecodeMomentControl(raw, false)
		if e != nil || out.Input.CityID != "" || out.Input.LocationPrecision != "none" {
			t.Fatal("closed private input invented a city", action, e)
		}
	}
	empty := ""
	contextID := momentControlTestID
	normalized.ActivityID, normalized.CommunityID, normalized.OrganizationID = &empty, &empty, &empty
	if _, err = NormalizeMomentInput(normalized); err != nil {
		t.Fatal("explicit empty context removal should remain distinguishable", err)
	}
	for _, mutate := range []func(*MomentInput){
		func(v *MomentInput) { v.PlaceID = momentControlTestID },
		func(v *MomentInput) { v.LocationPrecision = "city" },
		func(v *MomentInput) { v.ActivityID = &contextID },
		func(v *MomentInput) { v.CommunityID = &contextID },
		func(v *MomentInput) { v.OrganizationID = &contextID },
	} {
		value := input
		mutate(&value)
		if _, err = NormalizeMomentInput(value); err == nil {
			t.Fatal("unknown city accepted a location or external context")
		}
	}
	for _, action := range []string{"PUBLISH", "UPDATE_PUBLIC"} {
		value := input
		in := MomentControlInput{OperationID: momentControlTestOp, Action: action, MomentID: momentControlTestID, ExpectedRevision: 1, Input: &value}
		if _, err = NormalizeMomentControl(in, false); err != nil {
			t.Fatal("body-only public input required geography", action, err)
		}
		value.LocationPrecision = "city"
		if _, err = NormalizeMomentControl(in, false); err == nil {
			t.Fatal("public context precision supplied an absent city", action)
		}
	}
}

func TestMomentOptionalContentAtomicMediaAndClosedSelectors(t *testing.T) {
	value := MomentInput{Title: "", Body: "", TimePrecision: "unknown", LocationPrecision: "none"}
	base := MomentControlInput{OperationID: momentControlTestOp, Action: "CREATE", Input: &value}
	if _, e := NormalizeMomentControl(base, false); e != nil {
		t.Fatal("empty draft", e)
	}
	base.Action, base.MomentID, base.ExpectedRevision = "PUBLISH", momentControlTestID, 1
	for _, title := range []string{"", "title alone"} {
		value.Title = title
		if _, e := NormalizeMomentControl(base, false); e == nil {
			t.Fatal("title counted as publication content")
		}
	}
	value.Title = ""
	value.Body = " 自写正文 "
	if _, e := NormalizeMomentControl(base, false); e != nil {
		t.Fatal("body-only publication", e)
	}
	oldDigest := MomentControlHash([]any{"human-moment-control-v1", base.OperationID, base.Action, base.MomentID, base.ExpectedRevision, MomentControlInputHash(base.Input)})
	if MomentControlDigest(base) != oldDigest {
		t.Fatal("omitted media changed original digest")
	}
	value.Body = ""
	inputs := []mm.AttachmentInput{{MediaID: momentControlTestOp, MediaVersion: 1, Rights: mm.Rights{Basis: "OWNED", CanRetain: true, CanPublish: true, RetentionUntil: time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC)}}}
	base.Media, base.LocalScope = &inputs, momentControlTestID
	if _, e := NormalizeMomentControl(base, false); e != nil {
		t.Fatal("media-only atomic publication", e)
	}
	raw, _ := json.Marshal(base)
	decoded, e := DecodeMomentControl(raw, false)
	if e != nil || MomentControlDigest(decoded) != MomentControlDigest(base) {
		t.Fatal("atomic media wire", e)
	}
	for _, bad := range []string{strings.Replace(string(raw), `"mediaVersion":1`, `"mediaVersion":1,"canPublish":true`, 1), strings.Replace(string(raw), `"mediaVersion":1`, `"mediaVersion":1,"mediaVersion":1`, 1), strings.Replace(string(raw), `"canPublish":true`, `"canPublish":true,"scanClean":true`, 1)} {
		if _, e := DecodeMomentControl([]byte(bad), false); e == nil {
			t.Fatal("unclosed media authority")
		}
	}
	base.Action = "UPDATE_PUBLIC"
	base.Media = nil
	base.LocalScope = ""
	if _, e := NormalizeMomentControl(base, false); e != nil {
		t.Fatal("current-media preservation must be proven by native source", e)
	}
	empty := []mm.AttachmentInput{}
	base.Media = &empty
	if _, e := NormalizeMomentControl(base, false); e == nil {
		t.Fatal("explicit last-media removal left an empty public input")
	}
	value.Body = "有正文可移除媒体"
	if _, e := NormalizeMomentControl(base, false); e != nil {
		t.Fatal("explicit body-backed removal", e)
	}
	value.CityID = ""
	value.PlaceID = momentControlTestOp
	value.LocationPrecision = "place"
	if _, e := NormalizeMomentControl(base, false); e != nil {
		t.Fatal("independently selected Place required a fabricated City", e)
	}
	thread := MomentThreadInput{OperationID: momentControlTestOp, Action: "CREATE_PUBLISH", Audience: "PUBLIC", LocalScope: momentControlTestID, Items: []MomentThreadItem{{Input: MomentInput{TimePrecision: "unknown", LocationPrecision: "none"}, Media: inputs}, {Input: MomentInput{Body: "第二条正文", TimePrecision: "unknown", LocationPrecision: "none"}, Media: []mm.AttachmentInput{}}}}
	if _, e := NormalizeMomentThread(thread, false); e != nil {
		t.Fatal("media-only child rejected by nested public normalizer", e)
	}
	thread.Items = thread.Items[:1]
	if _, e := NormalizeMomentThread(thread, false); e == nil {
		t.Fatal("ordinary atomic publishing changed thread minimum")
	}
}

func TestMomentOptionalPublicReadersKeepEmptyTitleAndGeography(t *testing.T) {
	now := time.Now().UTC()
	v := PublicMomentWithContext{PublicMomentWithPhoto: PublicMomentWithPhoto{PublicMomentWithAuthor: PublicMomentWithAuthor{PublicMoment: PublicMoment{SchemaVersion: PublicMomentContextSchemaVersion, ID: momentControlTestID, Body: "正文", Revision: 2, PublishedAt: now, SourceBinding: "proof"}}, ObservedAt: now, ValidUntil: now.Add(time.Minute)}, LocationPrecision: "none"}
	if e := ValidatePublicMomentWithContext(v, v.ID); e != nil {
		t.Fatal("no-title/no-location v4", e)
	}
	v.LocationPrecision = "place"
	v.PlaceID = momentControlTestOp
	v.PlaceName = "真实地点"
	if e := ValidatePublicMomentWithContext(v, v.ID); e != nil {
		t.Fatal("Place-only v4", e)
	}
	v.LocationPrecision = "none"
	if e := ValidatePublicMomentWithContext(v, v.ID); e == nil {
		t.Fatal("hidden place metadata leaked as no-location")
	}
}
