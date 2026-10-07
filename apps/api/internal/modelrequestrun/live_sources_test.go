package modelrequestrun

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func liveSourceControlFixture() (Control, time.Time) {
	c, now := fixtureControl()
	c.SchemaVersion = LiveSourceSchemaVersion
	c.Kind = LiveSourceAnswer
	c.Steps[0].Kind = SourceRetrieval
	c.Steps[0].BindingState = Bound
	c.Steps[1].Kind = ModelInference
	c.Steps[1].BindingState = WaitingSource
	c.Steps[1].MaxOutputTokens = 768
	c.Steps[1].PreviewID = ""
	c.Steps[1].RequestDigest = ""
	return c, now
}
func TestLiveSourceMetadataOriginalV1JSONUnchanged(t *testing.T) {
	c, now := fixtureControl()
	if ValidateControl(c, now) != nil {
		t.Fatal("v1 rejected")
	}
	raw, e := json.Marshal(c)
	if e != nil {
		t.Fatal(e)
	}
	for _, field := range []string{"runKind", "stepKind", "bindingState", "sourceEvidenceDigest", "maxOutputTokens"} {
		if strings.Contains(string(raw), field) {
			t.Fatal("new live metadata leaked into original v1", field)
		}
	}
}

func TestLiveSourceMetadataFutureUpdatedClockStillRejected(t *testing.T) {
	c, now := liveSourceControlFixture()
	c.UpdatedAt = now.Add(time.Microsecond)
	if ValidateControl(c, now) == nil {
		t.Fatal("future control timestamp admitted")
	}
	if ValidateControl(c, c.UpdatedAt) != nil {
		t.Fatal("valid control rejected at its actual native clock")
	}
}
func TestLiveSourceMetadataWaitingAndOneBindingObservation(t *testing.T) {
	before, now := liveSourceControlFixture()
	if ValidatePlan(before, now) != nil {
		t.Fatal("initial two typed steps rejected")
	}
	before.State = RunRunning
	before.Revision = 4
	before.Steps[0].State = StepUnknown
	before.Steps[0].ReservationID = before.Steps[0].OperationID
	after := Clone(before)
	after.Revision++
	after.Steps[1].BindingState = Bound
	after.Steps[1].PreviewID = fixtureID(70)
	after.Steps[1].RequestDigest = strings.Repeat("c", 64)
	after.Steps[1].SourceEvidenceDigest = strings.Repeat("d", 64)
	if ValidateControl(after, now) != nil || ValidateObservation(before, after, now) != nil {
		t.Fatal("one exact native binding metadata rejected")
	}
	for _, tc := range []struct {
		name   string
		mutate func(*Control)
	}{
		{"rebind", func(c *Control) { c.Steps[1].PreviewID = fixtureID(71) }},
		{"change evidence", func(c *Control) { c.Steps[1].SourceEvidenceDigest = strings.Repeat("e", 64) }},
		{"change tariff", func(c *Control) { c.Steps[1].PriceVersion = "another-price" }},
		{"change model operation", func(c *Control) { c.Steps[1].OperationID = fixtureID(72) }},
		{"extend deadline", func(c *Control) { c.DeadlineAt = c.DeadlineAt.Add(time.Second) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			changed := Clone(after)
			changed.Revision++
			tc.mutate(&changed)
			if ValidateObservation(after, changed, now) == nil {
				t.Fatal("bound identity changed")
			}
		})
	}
}
func TestLiveSourceMetadataRejectsImpossibleShapes(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(*Control)
	}{
		{"v1 typed spoof", func(c *Control) { c.SchemaVersion = SchemaVersion }},
		{"third request", func(c *Control) { c.Steps = append(c.Steps, c.Steps[1]) }},
		{"wrong retrieval kind", func(c *Control) { c.Steps[0].Kind = ModelInference }},
		{"early preview", func(c *Control) { c.Steps[1].PreviewID = fixtureID(73) }},
		{"waiting reserved", func(c *Control) {
			c.State = RunRunning
			c.Steps[1].State = StepReserved
			c.Steps[1].ReservationID = c.Steps[1].OperationID
		}},
		{"cash settled", func(c *Control) {
			c.State = RunRunning
			c.Steps[0].State = StepSettled
			c.Steps[0].ReservationID = c.Steps[0].OperationID
		}},
		{"too much output", func(c *Control) { c.Steps[1].MaxOutputTokens = 769 }},
		{"UNKNOWN success alone", func(c *Control) {
			c.State = RunFinished
			c.Steps[0].State = StepUnknown
			c.Steps[0].ReservationID = c.Steps[0].OperationID
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, now := liveSourceControlFixture()
			tc.mutate(&c)
			if ValidateControl(c, now) == nil {
				t.Fatal("impossible live metadata accepted")
			}
		})
	}
}
