package agentfeature

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func configBody(enabled ...Feature) []byte {
	flags := map[string]bool{}
	for _, feature := range features {
		flags[string(feature)] = false
	}
	for _, feature := range enabled {
		flags[string(feature)] = true
	}
	body, _ := json.Marshal(map[string]any{"schemaVersion": SchemaVersion, "flags": flags,
		"pilot": map[string]any{"memory": "basic", "inference": "conservative", "autonomousAction": false, "sensitiveInference": false}})
	return body
}

func TestFeatureConfigEveryStagedCombinationAndPilotCeilings(t *testing.T) {
	for bits := 0; bits < 32; bits++ {
		t.Run(string(rune('A'+bits)), func(t *testing.T) {
			enabled := []Feature{}
			for index, feature := range features {
				if bits&(1<<index) != 0 {
					enabled = append(enabled, feature)
				}
			}
			config, err := ParseConfig(configBody(enabled...))
			if bits == 31 {
				if !errors.Is(err, ErrUnsafeConfig) {
					t.Fatal("all-feature activation must be refused")
				}
				return
			}
			if err != nil || config.validate() != nil {
				t.Fatal("valid staged configuration refused")
			}
			for index, feature := range features {
				want := bits&1 != 0 && bits&(1<<index) != 0
				if config.enabled(feature) != want {
					t.Fatal("feature bypassed its own switch or enrichment parent")
				}
			}
			limits := Limits()
			if limits.MemoryMode != "basic" || limits.InferenceMode != "conservative" || limits.AutonomousAction || limits.SensitiveInference {
				t.Fatal("Pilot ceiling changed or enabled an unsafe capability")
			}
		})
	}
}

func TestFeatureConfigStrictJSONAndRedactedErrors(t *testing.T) {
	valid := string(configBody())
	cases := map[string]string{
		"empty": "", "null": "null", "array": "[]", "scalar": "1", "trailing": valid + " {}",
		"unknown root":         strings.Replace(valid, `"flags":`, `"attacker_canary":"private-value","flags":`, 1),
		"unknown flag":         strings.Replace(valid, `"agent_memory":false`, `"agent_memory":false,"all_enabled":true`, 1),
		"unknown version":      strings.Replace(valid, SchemaVersion, "v999", 1),
		"missing flag":         strings.Replace(valid, `"life_map":false`, `"other":false`, 1),
		"null flags":           strings.Replace(valid, `"flags":{`, `"flags":null,"ignored":{`, 1),
		"null flag":            strings.Replace(valid, `"agent_memory":false`, `"agent_memory":null`, 1),
		"string flag":          strings.Replace(valid, `"agent_memory":false`, `"agent_memory":"true"`, 1),
		"numeric flag":         strings.Replace(valid, `"agent_memory":false`, `"agent_memory":1`, 1),
		"duplicate root":       strings.Replace(valid, `"flags":`, `"schemaVersion":"`+SchemaVersion+`","flags":`, 1),
		"duplicate flag":       strings.Replace(valid, `"agent_memory":false`, `"agent_memory":false,"agent_memory":true`, 1),
		"escaped duplicate":    strings.Replace(valid, `"agent_memory":false`, `"agent_memory":false,"agent_\u006demory":true`, 1),
		"duplicate pilot":      strings.Replace(valid, `"memory":"basic"`, `"memory":"basic","memory":"basic"`, 1),
		"null pilot":           strings.Replace(valid, `"memory":"basic"`, `"memory":null`, 1),
		"full memory":          strings.Replace(valid, `"memory":"basic"`, `"memory":"full"`, 1),
		"aggressive inference": strings.Replace(valid, `"inference":"conservative"`, `"inference":"aggressive"`, 1),
		"autonomous":           strings.Replace(valid, `"autonomousAction":false`, `"autonomousAction":true`, 1),
		"sensitive":            strings.Replace(valid, `"sensitiveInference":false`, `"sensitiveInference":true`, 1),
		"numeric autonomous":   strings.Replace(valid, `"autonomousAction":false`, `"autonomousAction":1`, 1),
		"unknown pilot":        strings.Replace(valid, `"memory":"basic"`, `"memory":"basic","model":"private-canary"`, 1),
		"oversized":            valid + strings.Repeat(" ", MaxConfigBytes),
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			config, err := ParseConfig([]byte(body))
			if err == nil || len(config.flags) != 0 || strings.Contains(err.Error(), "canary") || strings.Contains(err.Error(), "private-value") {
				t.Fatal("invalid config was accepted or error exposed its content")
			}
		})
	}
}

func TestFeatureConfigMissingEnvIsClosedButExplicitInvalidEnvFails(t *testing.T) {
	config, err := LoadConfig(func(key string) (string, bool) {
		if key != EnvironmentKey {
			t.Fatal("unexpected configuration source")
		}
		return "", false
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, feature := range features {
		if config.enabled(feature) {
			t.Fatal("missing server config activated a feature")
		}
	}
	for _, body := range []string{"", "true", "client-canary"} {
		if _, err := LoadConfig(func(string) (string, bool) { return body, true }); err == nil {
			t.Fatal("explicit invalid configuration inherited defaults")
		}
	}
	if _, err := LoadConfig(nil); !errors.Is(err, ErrInvalidConfig) {
		t.Fatal("missing trusted source was accepted")
	}
}

func TestFeatureConfigAndTicketCannotCrossJSONBoundary(t *testing.T) {
	config := DefaultConfig()
	if _, err := json.Marshal(config); !errors.Is(err, ErrServerOnly) {
		t.Fatal("configuration became client authority")
	}
	if err := json.Unmarshal(configBody(Enrichment), &config); !errors.Is(err, ErrServerOnly) || len(config.flags) != 0 {
		t.Fatal("client JSON populated server-owned config")
	}
	ticket := Ticket{}
	if _, err := json.Marshal(ticket); !errors.Is(err, ErrServerOnly) {
		t.Fatal("switch ticket was transferable")
	}
	if err := json.Unmarshal([]byte(`{"feature":"agent_enrichment","revision":1}`), &ticket); !errors.Is(err, ErrServerOnly) || ticket != (Ticket{}) {
		t.Fatal("client JSON forged switch ticket")
	}
}
