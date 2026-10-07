package modelgateway

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const tencentTestKey = "local-synthetic-secret-sentinel"

func tencentConfigJSON() string {
	return `{"provider":"tencent_tokenhub","baseUrl":"https://tokenhub.tencentmaas.com/v1","model":"hy3","maxOutputTokens":768,"apiKey":"` + tencentTestKey + `"}`
}

func tencentTestConfig(t *testing.T) TencentTokenHubConfig {
	t.Helper()
	c, err := ParseTencentTokenHubConfig([]byte(tencentConfigJSON()))
	if err != nil {
		t.Fatal("synthetic server configuration rejected")
	}
	return c
}

func TestTencentConfigExistingShapeAndRedaction(t *testing.T) {
	c := tencentTestConfig(t)
	if !c.valid() || c.maxOutputTokens != 768 || c.apiKey != tencentTestKey {
		t.Fatal("existing ignored JSON shape not preserved")
	}
	for _, verb := range []string{"%v", "%+v", "%#v", "%s", "%q"} {
		shown := fmt.Sprintf(verb, c)
		if strings.Contains(shown, tencentTestKey) || !strings.Contains(shown, "redacted") {
			t.Fatal("configuration formatting exposed secret material")
		}
	}
	if raw, err := json.Marshal(c); err == nil || len(raw) != 0 {
		t.Fatal("configuration became a client JSON object")
	}
	if err := json.Unmarshal([]byte(tencentConfigJSON()), &c); !errors.Is(err, ErrTencentConfig) || c.valid() {
		t.Fatal("JSON could reconstruct transport configuration outside loader")
	}
}

func TestTencentConfigRejectsUnsafeAndAmbiguousJSON(t *testing.T) {
	valid := tencentConfigJSON()
	cases := map[string]string{
		"duplicate_key":  strings.Replace(valid, `"apiKey":`, `"apiKey":"other","apiKey":`, 1),
		"case_alias":     strings.Replace(valid, `"apiKey"`, `"ApiKey"`, 1),
		"provider":       strings.Replace(valid, `"tencent_tokenhub"`, `"other"`, 1),
		"model":          strings.Replace(valid, `"hy3"`, `"deepseek-v4-flash"`, 1),
		"http":           strings.Replace(valid, "https://", "http://", 1),
		"foreign_host":   strings.Replace(valid, "tokenhub.tencentmaas.com", "example.org", 1),
		"userinfo":       strings.Replace(valid, "https://", "https://user:password@", 1),
		"query":          strings.Replace(valid, "/v1\"", "/v1?token=secret\"", 1),
		"fragment":       strings.Replace(valid, "/v1\"", "/v1#secret\"", 1),
		"slash":          strings.Replace(valid, "/v1\"", "/v1/\"", 1),
		"unknown_search": strings.Replace(valid, `"provider":`, `"web_search_options":{"enable":true},"provider":`, 1),
		"unknown_budget": strings.Replace(valid, `"provider":`, `"paidBudget":10,"provider":`, 1),
		"null_limit":     strings.Replace(valid, "768", "null", 1),
		"zero_limit":     strings.Replace(valid, "768", "0", 1),
		"large_limit":    strings.Replace(valid, "768", "4097", 1),
		"float_limit":    strings.Replace(valid, "768", "768.5", 1),
		"string_limit":   strings.Replace(valid, "768", `"768"`, 1),
		"empty_key":      strings.Replace(valid, tencentTestKey, "", 1),
		"newline_key":    strings.Replace(valid, tencentTestKey, `secret\r\nheader`, 1),
		"space_key":      strings.Replace(valid, tencentTestKey, "secret key", 1),
		"null_key":       strings.Replace(valid, `"`+tencentTestKey+`"`, "null", 1),
		"missing_key":    `{"provider":"tencent_tokenhub","baseUrl":"https://tokenhub.tencentmaas.com/v1","model":"hy3","maxOutputTokens":768}`,
		"too_large":      strings.Repeat(" ", MaxTencentConfigBytes+1),
		"trailing":       valid + "{}",
		"array":          "[]",
		"empty":          "",
	}
	for name, raw := range cases {
		t.Run(name, func(t *testing.T) {
			c, err := ParseTencentTokenHubConfig([]byte(raw))
			if !errors.Is(err, ErrTencentConfig) || c.valid() || strings.Contains(err.Error(), tencentTestKey) {
				t.Fatal("unsafe configuration accepted or error exposed a value")
			}
		})
	}
}

func TestTencentConfigBoundedLocalFileLoader(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".env.tencent.local.json")
	if err := os.WriteFile(path, []byte(tencentConfigJSON()), 0600); err != nil {
		t.Fatal("could not write synthetic configuration")
	}
	c, err := LoadTencentTokenHubConfig(path)
	if err != nil || !c.valid() {
		t.Fatal("synthetic local file rejected")
	}
	for _, invalid := range []string{".env.tencent.local.json", filepath.Join(filepath.Dir(path), "missing-secret.json"), filepath.Dir(path)} {
		_, err := LoadTencentTokenHubConfig(invalid)
		if !errors.Is(err, ErrTencentConfig) || strings.Contains(err.Error(), invalid) {
			t.Fatal("unsafe path or path-bearing error accepted")
		}
	}
	if err := os.WriteFile(path, []byte(strings.Repeat("x", MaxTencentConfigBytes+1)), 0600); err != nil {
		t.Fatal("could not write oversized synthetic configuration")
	}
	if _, err := LoadTencentTokenHubConfig(path); !errors.Is(err, ErrTencentConfig) {
		t.Fatal("oversized configuration file accepted")
	}
	if _, err := NewTencentTokenHubAdapter(TencentTokenHubConfig{}, nil); !errors.Is(err, ErrTencentConfig) {
		t.Fatal("zero configuration produced a transport")
	}
}
