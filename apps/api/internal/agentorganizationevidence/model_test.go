package agentorganizationevidence

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/actorref"
	"github.com/birdtie/birdtie/apps/api/internal/agentevent"
	"github.com/birdtie/birdtie/apps/api/internal/agentmemory"
)

const testID = "9c000000-0000-4000-8000-000000000001"
const testMemory = "9c000000-0000-4000-8000-000000000002"
const testAgent = "9c000000-0000-4000-8000-000000000003"
const testOwner = "9c000000-0000-4000-8000-000000000004"
const testSource = "9c000000-0000-4000-8000-000000000005"

func exampleEvidence(k agentevent.SourceType) agentmemory.Evidence {
	now := time.Date(2026, 10, 3, 0, 0, 0, 0, time.UTC)
	v := agentevent.SourceVersion{Kind: agentevent.RevisionVersion, Revision: 7}
	if k == Profile || k == PublicContent {
		v = agentevent.SourceVersion{Kind: agentevent.UpdatedAtDigestVersion, Token: strings.Repeat("a", 64)}
	}
	return agentmemory.Evidence{SchemaVersion: agentmemory.EvidenceSchemaV1, ID: testID, MemoryID: testMemory, MemoryVersion: 2, AgentID: testAgent, OwnerType: actorref.Organization, OwnerID: testOwner, Version: 1, Status: agentmemory.EvidenceCurrent, Source: &agentevent.SourceReference{Type: k, ID: testSource, Owner: actorref.PrincipalRef{Type: actorref.Organization, ID: testOwner}, Version: v}, SignalType: agentmemory.SignalManualReference, Weight: 1, ObservedAt: now, CreatedAt: now, EventTime: &now}
}
func TestOrganizationEvidenceClosedNativeKinds(t *testing.T) {
	for _, k := range []agentevent.SourceType{Profile, Activity, AdminInput, PublicContent, Announcement} {
		t.Run(string(k), func(t *testing.T) {
			e := exampleEvidence(k)
			if ValidateEvidence(e) != nil {
				t.Fatal("native metadata rejected")
			}
			if agentmemory.ValidateEvidence(e) == nil {
				t.Fatal("PERSON validator was broadened")
			}
			if _, err := NormalizeReference(agentmemory.EvidenceReferenceInput{ExpectedMemoryVersion: 1, SourceType: k, SourceID: testSource}); err != nil {
				t.Fatal(err)
			}
		})
	}
	for _, k := range []agentevent.SourceType{agentevent.MomentSource, "PUBLIC_URL", "UNKNOWN"} {
		t.Run(string(k), func(t *testing.T) {
			_, e := NormalizeReference(agentmemory.EvidenceReferenceInput{ExpectedMemoryVersion: 1, SourceType: k, SourceID: testSource})
			want := agentmemory.ErrInvalid
			if !errors.Is(e, want) {
				t.Fatal(e)
			}
			if ValidateEvidence(exampleEvidence(k)) == nil {
				t.Fatal("unknown/unavailable native source accepted")
			}
		})
	}
}
func TestOrganizationEvidenceRejectsMetadataAuthorityAndInvalidShape(t *testing.T) {
	cases := map[string]func(*agentmemory.Evidence){
		"person": func(e *agentmemory.Evidence) { e.OwnerType = actorref.Person }, "cross_source": func(e *agentmemory.Evidence) { e.Source.Owner.ID = testAgent }, "person_source": func(e *agentmemory.Evidence) { e.Source.Owner.Type = actorref.Person },
		"self_admin": func(e *agentmemory.Evidence) { e.Source.ID = e.MemoryID }, "zero_revision": func(e *agentmemory.Evidence) { e.Source.Version.Revision = 0 }, "extra_token": func(e *agentmemory.Evidence) { e.Source.Version.Token = strings.Repeat("a", 64) },
		"zero_memory_version": func(e *agentmemory.Evidence) { e.MemoryVersion = 0 }, "zero_version": func(e *agentmemory.Evidence) { e.Version = 0 }, "wrong_schema": func(e *agentmemory.Evidence) { e.SchemaVersion = "verified" }, "signal": func(e *agentmemory.Evidence) { e.SignalType = "VERIFIED" }, "weight": func(e *agentmemory.Evidence) { e.Weight = .9 },
		"future_event": func(e *agentmemory.Evidence) { at := e.ObservedAt.Add(time.Second); e.EventTime = &at }, "zero_event": func(e *agentmemory.Evidence) { e.EventTime = nil }, "created_before_observed": func(e *agentmemory.Evidence) { e.CreatedAt = e.ObservedAt.Add(-time.Second) }, "bad_id": func(e *agentmemory.Evidence) { e.ID = "x" }, "zero_source": func(e *agentmemory.Evidence) { e.Source = nil }, "pending": func(e *agentmemory.Evidence) { e.Status = "PENDING" },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			e := exampleEvidence(AdminInput)
			mutate(&e)
			if ValidateEvidence(e) == nil {
				t.Fatal("invalid metadata accepted")
			}
		})
	}
	for _, token := range []string{strings.Repeat("A", 64), strings.Repeat("z", 64), "", strings.Repeat("a", 63)} {
		t.Run("digest_"+token, func(t *testing.T) {
			e := exampleEvidence(Profile)
			e.Source.Version.Token = token
			if ValidateEvidence(e) == nil {
				t.Fatal("invalid digest")
			}
		})
	}
}
func TestOrganizationEvidenceRemovedScrubsSource(t *testing.T) {
	e := exampleEvidence(AdminInput)
	e.Status = agentmemory.EvidenceRemoved
	e.Version = 2
	e.Source = nil
	e.EventTime = nil
	e.SignalType = ""
	e.Weight = 0
	if ValidateEvidence(e) != nil {
		t.Fatal("valid tombstone rejected")
	}
	e.EventTime = &e.ObservedAt
	if ValidateEvidence(e) == nil {
		t.Fatal("tombstone time leaks source")
	}
}
func TestOrganizationEvidenceStrictHumanWire(t *testing.T) {
	good := `{"expectedMemoryVersion":1,"sourceType":"ORGANIZATION_ADMIN_INPUT","sourceId":"` + testSource + `"}`
	if _, e := DecodeReference([]byte(good)); e != nil {
		t.Fatal(e)
	}
	for name, raw := range map[string]string{"authority": strings.Replace(good, "{", `{"confirmed":true,`, 1), "duplicate": strings.Replace(good, "{", `{"sourceId":"`+testAgent+`",`, 1), "null": strings.Replace(good, `"expectedMemoryVersion":1`, `"expectedMemoryVersion":null`, 1), "missing": strings.Replace(good, `"expectedMemoryVersion":1,`, "", 1), "trailing": good + "{}", "array": "[" + good + "]", "uppercase": strings.Replace(good, testSource, strings.ToUpper(testSource), 1), "overflow": strings.Replace(good, `"expectedMemoryVersion":1`, `"expectedMemoryVersion":9223372036854775808`, 1)} {
		t.Run(name, func(t *testing.T) {
			if _, e := DecodeReference([]byte(raw)); e == nil {
				t.Fatal("untrusted wire accepted")
			}
		})
	}
}
func TestOrganizationEvidenceCurrentFixedProvenance(t *testing.T) {
	now := exampleEvidence(AdminInput).ObservedAt
	kind, key, err := agentmemory.OrganizationMemoryKey(agentmemory.OrgPolicy, "rules")
	if err != nil {
		t.Fatal(err)
	}
	in := agentmemory.PutInput{ExpectedVersion: 1, MemoryType: kind, MemoryKey: key, Summary: "合成人工说明", StructuredValue: json.RawMessage(`{"note":"合成"}`), Visibility: agentmemory.VisibilityPrivate, ValidUntil: now.Add(time.Hour)}
	m, e := agentmemory.NewOrganizationExplicit(testMemory, testAgent, actorref.PrincipalRef{Type: actorref.Organization, ID: testOwner}, 2, in, now, now)
	if e != nil {
		t.Fatal(e)
	}
	rec := exampleEvidence(AdminInput)
	p, e := Build(testID, m, []agentmemory.Evidence{rec}, now)
	if e != nil {
		t.Fatal(e)
	}
	if p.Explanation != "组织管理员关联了1条管理员声明；人工关联不代表已核验事实。" {
		t.Fatal(p.Explanation)
	}
	p.Evidence[0].Source.ID = testAgent
	if rec.Source.ID != testSource {
		t.Fatal("aliased source mutation")
	}
	for _, kind := range []string{"cross_owner", "old_version", "duplicate", "expired", "future_observation", "extra_source", "bad_explanation"} {
		t.Run(kind, func(t *testing.T) {
			m2 := m
			r := exampleEvidence(AdminInput)
			all := []agentmemory.Evidence{r}
			switch kind {
			case "cross_owner":
				r.OwnerID = testAgent
				r.Source.Owner.ID = testAgent
				all = []agentmemory.Evidence{r}
			case "old_version":
				r.MemoryVersion = 1
				all = []agentmemory.Evidence{r}
			case "duplicate":
				all = append(all, r)
			case "expired":
				m2.ValidUntil = now
			case "future_observation":
				r.ObservedAt = now.Add(time.Minute)
				r.CreatedAt = r.ObservedAt
				all = []agentmemory.Evidence{r}
			case "extra_source":
				r.ID = testSource
				all = append(all, r)
			case "bad_explanation":
				out, err := Build(testID, m2, all, now)
				if err != nil {
					t.Fatal(err)
				}
				out.Explanation = "已核验偏好"
				if ValidateProvenance(out, testID, testMemory) == nil {
					t.Fatal("invented explanation")
				}
				return
			}
			if _, e := Build(testID, m2, all, now); e == nil {
				t.Fatal("invalid provenance accepted")
			}
		})
	}
}
