package agentfeature

import (
	"errors"
	"sync"
	"testing"
)

func enabledConfig(t *testing.T, enabled ...Feature) Config {
	t.Helper()
	config, err := ParseConfig(configBody(enabled...))
	if err != nil {
		t.Fatal(err)
	}
	return config
}

func TestFeatureControllerEveryIndependentKillSwitch(t *testing.T) {
	for _, feature := range features {
		t.Run(string(feature), func(t *testing.T) {
			controller, err := NewController(enabledConfig(t, Enrichment, feature))
			if err != nil {
				t.Fatal(err)
			}
			ticket, err := controller.Capture(feature)
			if err != nil || !controller.Current(ticket) {
				t.Fatal("staged feature could not be checked")
			}
			before := controller.Revision()
			if controller.Disable(feature) != nil || controller.Revision() != before+1 || controller.Current(ticket) {
				t.Fatal("kill switch failed to invalidate in-flight ticket")
			}
			if _, err = controller.Capture(feature); !errors.Is(err, ErrDisabled) {
				t.Fatal("disabled feature could still be entered")
			}
			if feature != Enrichment {
				if _, err = controller.Capture(Enrichment); err != nil {
					t.Fatal("child brake disabled unrelated parent setting")
				}
			}
			if controller.Disable(feature) != nil || controller.Revision() != before+1 {
				t.Fatal("repeated disable must be idempotent")
			}
		})
	}
}

func TestFeatureControllerParentOffReplayAndInstanceIsolation(t *testing.T) {
	config := enabledConfig(t, Enrichment, Memory, SocialPolicy)
	controller, _ := NewController(config)
	other, _ := NewController(config)
	ticket, _ := controller.Capture(Memory)
	if other.Current(ticket) || controller.Current(Ticket{}) {
		t.Fatal("ticket transferred between instances or from zero value")
	}
	if controller.Disable(Enrichment) != nil {
		t.Fatal("parent brake failed")
	}
	for _, feature := range []Feature{Enrichment, Memory, SocialPolicy} {
		if _, err := controller.Capture(feature); !errors.Is(err, ErrDisabled) {
			t.Fatal("parent OFF did not suppress dependency")
		}
	}
	if controller.Replace(controller.Revision(), config) != nil || controller.Current(ticket) {
		t.Fatal("OFF then ON resurrected an old ticket")
	}
}

func TestFeatureControllerUpdateCASAtomicityAndConfigCopy(t *testing.T) {
	config := enabledConfig(t, Enrichment)
	controller, _ := NewController(config)
	config.flags[Enrichment] = false
	if _, err := controller.Capture(Enrichment); err != nil {
		t.Fatal("caller mutated controller config through shared map")
	}
	before := controller.Revision()
	if !errors.Is(controller.Replace(before+1, DefaultConfig()), ErrConflict) || controller.Revision() != before {
		t.Fatal("stale CAS modified configuration")
	}
	if !errors.Is(controller.Replace(before, Config{}), ErrInvalidConfig) || controller.Revision() != before {
		t.Fatal("invalid configuration modified current state")
	}
	all := DefaultConfig()
	for _, feature := range features {
		all.flags[feature] = true
	}
	if !errors.Is(controller.Replace(before, all), ErrUnsafeConfig) || controller.Revision() != before {
		t.Fatal("all-on bypassed staged activation")
	}
	start := make(chan struct{})
	results := make(chan error, 2)
	for index := 0; index < 2; index++ {
		go func() { <-start; results <- controller.Replace(before, DefaultConfig()) }()
	}
	close(start)
	winners, conflicts := 0, 0
	for index := 0; index < 2; index++ {
		if err := <-results; err == nil {
			winners++
		} else if errors.Is(err, ErrConflict) {
			conflicts++
		}
	}
	if winners != 1 || conflicts != 1 || controller.Revision() != before+1 {
		t.Fatal("concurrent configuration CAS had multiple winners")
	}
}

func TestFeatureControllerNilUnknownAndConcurrentReadersFailClosed(t *testing.T) {
	var absent *Controller
	if absent.Revision() != 0 || absent.Current(Ticket{}) || absent.Disable(Memory) == nil || absent.Replace(1, DefaultConfig()) == nil {
		t.Fatal("nil controller accepted a capability")
	}
	controller, _ := NewController(enabledConfig(t, Enrichment, Memory))
	before := controller.Revision()
	for _, feature := range []Feature{"", "ALL", "agent_enrichment ", "autonomous_action", "sensitive_inference"} {
		if _, err := controller.Capture(feature); err == nil || controller.Disable(feature) == nil {
			t.Fatal("unknown feature became capability")
		}
	}
	if controller.Revision() != before {
		t.Fatal("unknown feature altered current state")
	}
	var wait sync.WaitGroup
	for index := 0; index < 32; index++ {
		wait.Add(1)
		go func() {
			defer wait.Done()
			for read := 0; read < 50; read++ {
				ticket, err := controller.Capture(Memory)
				if err == nil {
					_ = controller.Current(ticket)
				}
				_ = controller.Revision()
			}
		}()
	}
	if controller.Disable(Memory) != nil {
		t.Fatal("concurrent brake failed")
	}
	wait.Wait()
	if _, err := controller.Capture(Memory); !errors.Is(err, ErrDisabled) {
		t.Fatal("Memory brake did not remain closed")
	}
}
