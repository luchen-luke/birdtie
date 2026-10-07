package agentorganization

import (
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/birdtie/birdtie/apps/api/internal/actorref"
	"github.com/birdtie/birdtie/apps/api/internal/foundation"
	"github.com/birdtie/birdtie/apps/api/internal/organization"
)

const org = "11111111-1111-4111-8111-111111111111"
const principal = "22222222-2222-4222-8222-222222222222"
const agent = "33333333-3333-4333-8333-333333333333"
const source = "44444444-4444-4444-8444-444444444444"

func projection() Projection {
	return Projection{Actor: actorref.ActorRef{Type: actorref.Organization, ID: org}, Principal: actorref.PrincipalRef{Type: actorref.Organization, ID: principal}, AgentID: agent, MetadataVersion: 2, Profile: organization.PublicProfile{ID: org, VerificationStatus: "verified", AgentAvailable: true, Description: "合成组织公开介绍", OfficialLinks: []string{"https://example.invalid/org"}, UpcomingActivities: []foundation.Activity{}}, FAQs: []organization.FAQ{{ID: source, OrganizationID: org, Question: "如何报名", Answer: "请打开活动详情报名。", Published: true}}}
}

func TestOrganizationCapabilityRendererRetainsBoundedPublicAnswers(t *testing.T) {
	for _, c := range []struct{ query, status, kind string }{{"如何报名", "known", "faq"}, {"组织介绍", "known", "profile"}, {"官网", "known", "link"}, {"某私人会员的偏好是什么", "unknown", ""}} {
		t.Run(c.query, func(t *testing.T) {
			p := projection()
			answer, err := Answer(c.query, p)
			if err != nil || answer.Status != c.status || !ValidAnswer(answer, org) {
				t.Fatalf("public renderer: %v %#v", err, answer)
			}
			if c.kind != "" && answer.Sources[0].Type != c.kind {
				t.Fatal("source kind changed")
			}
		})
	}
	p := projection()
	p.Profile.UpcomingActivities = []foundation.Activity{{ID: source, OrganizationID: func() *string { x := org; return &x }(), Organizer: foundation.ActivityOrganizer{Type: "ORGANIZATION", ID: org}, Visibility: "public", Status: "upcoming", Title: "合成羽毛球", Schedule: "10月4日（周日）14:00", PlaceName: "合成公共球馆"}}
	answer, err := Answer("近期有什么活动", p)
	if err != nil || answer.Sources[0].Type != "activity" || answer.Sources[0].ID != source || !strings.Contains(answer.Answer, p.Profile.UpcomingActivities[0].Schedule) {
		t.Fatal("public activity source not retained")
	}
	t.Log("OfflineContract shape/render tests only: fixture strings do not authenticate an organization or Activity")
}

func TestOrganizationCapabilityNativeShapeAndDisclosureLimits(t *testing.T) {
	cases := map[string]func(*Projection){"wrong_actor": func(p *Projection) { p.Actor.Type = actorref.Person }, "wrong_principal": func(p *Projection) { p.Principal.Type = actorref.Business }, "profile_mismatch": func(p *Projection) { p.Profile.ID = principal }, "foreign_faq": func(p *Projection) { p.FAQs[0].OrganizationID = principal }, "unpublished_faq": func(p *Projection) { p.FAQs[0].Published = false }, "duplicate_faq": func(p *Projection) { p.FAQs = append(p.FAQs, p.FAQs[0]) }, "oversized": func(p *Projection) { p.FAQs[0].Answer = strings.Repeat("字", 3001) }}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			p := projection()
			mutate(&p)
			a, err := Answer("如何报名", p)
			if !errors.Is(err, ErrInvalid) || !reflect.DeepEqual(a, organization.AgentAnswer{}) {
				t.Fatal("shape/source violation survived")
			}
		})
	}
	for _, mutate := range []func(*Projection){func(p *Projection) { p.AgentID = "" }, func(p *Projection) { p.MetadataVersion = 0 }, func(p *Projection) { p.Profile.AgentAvailable = false }, func(p *Projection) { p.Profile.VerificationStatus = "unverified" }} {
		p := projection()
		mutate(&p)
		a, err := Answer("如何报名", p)
		if err != nil || a.Status != "unknown" || len(a.Sources) != 0 || strings.Contains(a.Answer, p.FAQs[0].Answer) {
			t.Fatal("unknown claimed known source")
		}
	}
	p := projection()
	if _, err := json.Marshal(p); !errors.Is(err, ErrWire) {
		t.Fatal("projection serialized as authority")
	}
	if err := json.Unmarshal([]byte(`{"AgentID":"approved","Verified":true}`), &p); !errors.Is(err, ErrWire) || !reflect.DeepEqual(p, Projection{}) {
		t.Fatal("wire created projection authority")
	}
}

func TestOrganizationCapabilityStrictQueryWire(t *testing.T) {
	for name, body := range map[string]string{"valid": `{"query":"  如何报名  "}`, "duplicate": `{"query":"如何报名","query":"官网"}`, "case_alias": `{"Query":"如何报名"}`, "owner": `{"query":"如何报名","ownerId":"approved"}`, "null": `{"query":null}`, "trailing": `{"query":"如何报名"} {}`, "array": `["如何报名"]`, "short": `{"query":"好"}`} {
		t.Run(name, func(t *testing.T) {
			q, e := DecodeQuery([]byte(body))
			if name == "valid" {
				if e != nil || q != "如何报名" {
					t.Fatal("valid Chinese query rejected")
				}
			} else if !errors.Is(e, ErrInvalid) || q != "" {
				t.Fatal("invalid selector query survived")
			}
		})
	}
}

func TestOrganizationCapabilityTypedNamespacesAndUnsafeLinks(t *testing.T) {
	p := projection()
	p.Principal.ID = p.Actor.ID
	p.AgentID = p.Actor.ID
	// Same UUID text across independent tables does not merge typed identities.
	// This pure shape check cannot certify the native relationships.
	if a, e := Answer("如何报名", p); e != nil || a.Status != "known" {
		t.Fatal("invented cross-table UUID inequality", e)
	}
	for _, link := range []string{"https://", "https://:443", "https://user:password@example.invalid", "https://example.invalid/" + strings.Repeat("x", 2049), "http://example.invalid"} {
		a := organization.AgentAnswer{OrganizationID: org, Status: "known", Answer: "旧资料链接", Sources: []organization.AnswerSource{{Type: "link", ID: org, Label: "组织链接", URL: link}}, Mode: "verified_rules"}
		if ValidAnswer(a, org) {
			t.Fatal("unsafe legacy source URL passed")
		}
	}
}
