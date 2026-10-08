package modelgateway

import (
	"bytes"
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
	if !c.valid() || c.maxOutputTokens != 768 || c.credential() != tencentTestKey || c.model != TencentTokenHubModel {
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

func TestTencentConfigExactModelAllowlistAndOpaqueCredential(t *testing.T) {
	for _, model := range []string{TencentTokenHubModel, TencentTokenHubDeepSeekModel} {
		t.Run(model, func(t *testing.T) {
			raw := strings.Replace(tencentConfigJSON(), `"hy3"`, `"`+model+`"`, 1)
			raw = strings.Replace(raw, tencentTestKey, "opaque+/_=.:!~", 1)
			c, err := ParseTencentTokenHubConfig([]byte(raw))
			if err != nil || !c.valid() || c.model != model || c.credential() != "opaque+/_=.:!~" {
				t.Fatal("exact selected model or opaque credential rejected")
			}
			for _, verb := range []string{"%v", "%+v", "%#v", "%s", "%q", "%w"} {
				if shown := fmt.Sprintf(verb, c); strings.Contains(shown, c.credential()) || (verb != "%w" && !strings.Contains(shown, "redacted")) {
					t.Fatal("opaque configuration formatting: credentialPresent", strings.Contains(shown, c.credential()), "verb", verb)
				}
			}
		})
	}
	for _, model := range []string{"deepseek-v4-pro", "deepseek-v4-pro-0813 ", "DeepSeek-v4-pro-0813", "deepseek/deepseek-flash", "", "hy3@hy3"} {
		raw := strings.Replace(tencentConfigJSON(), `"hy3"`, `"`+model+`"`, 1)
		if c, err := ParseTencentTokenHubConfig([]byte(raw)); !errors.Is(err, ErrTencentConfig) || c.valid() {
			t.Fatal("model alias or unselected route accepted")
		}
	}
}

func TestTencentConfigEnvironmentOverrideRequiresSelectedOpaqueKey(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".env.tencent.local.json")
	raw := []byte(strings.Replace(tencentConfigJSON(), `"hy3"`, `"`+TencentTokenHubDeepSeekModel+`"`, 1))
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal("could not write synthetic configuration")
	}
	for name, env := range map[string]struct {
		key     string
		present bool
		valid   bool
	}{
		"opaque":         {"opaque+/_=.:!~", true, true},
		"missing":        {"opaque-synthetic", false, false},
		"empty":          {"", true, false},
		"space":          {"synthetic key", true, false},
		"leading_space":  {" synthetic", true, false},
		"trailing_space": {"synthetic ", true, false},
		"tab":            {"synthetic\tkey", true, false},
		"newline":        {"synthetic\r\nkey", true, false},
		"unicode":        {"synthetic密钥", true, false},
		"too_long":       {strings.Repeat("x", 4097), true, false},
	} {
		t.Run(name, func(t *testing.T) {
			calls := 0
			c, err := LoadTencentTokenHubConfigFromEnvironment(path, func(name string) (string, bool) {
				calls++
				if name != TencentTokenHubAPIKeyEnvironment {
					t.Fatal("unselected environment name read")
				}
				return env.key, env.present
			})
			if calls != 1 {
				t.Fatal("environment lookup was repeated")
			}
			if env.valid {
				if err != nil || !c.valid() || c.credential() != env.key || c.credential() == tencentTestKey || c.model != TencentTokenHubDeepSeekModel {
					t.Fatal("environment failed to replace the old file credential")
				}
			} else if !errors.Is(err, ErrTencentConfig) || c.valid() || c.credential() != "" || err.Error() != ErrTencentConfig.Error() {
				t.Fatal("missing/invalid environment credential used file fallback or leaked data")
			}
		})
	}
	if c, err := LoadTencentTokenHubConfigFromEnvironment(path, nil); !errors.Is(err, ErrTencentConfig) || c.valid() {
		t.Fatal("nil environment lookup used file fallback")
	}
	if c, err := LoadTencentTokenHubConfig(path); err != nil || c.credential() != tencentTestKey || c.model != TencentTokenHubDeepSeekModel {
		t.Fatal("legacy file-only loader behavior changed")
	}
	after, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(raw, after) {
		t.Fatal("environment loader copied its credential into the file")
	}
}

func TestTencentConfigEnvironmentOverrideRetainsClosedFileShape(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".env.tencent.local.json")
	lookup := func(name string) (string, bool) { return "opaque-synthetic", name == TencentTokenHubAPIKeyEnvironment }
	for name, test := range map[string]struct {
		raw   string
		valid bool
	}{
		"old_key":           {tencentConfigJSON(), true},
		"empty_placeholder": {strings.Replace(tencentConfigJSON(), tencentTestKey, "", 1), true},
		"obsolete_file_key": {strings.Replace(tencentConfigJSON(), tencentTestKey, "old file key", 1), true},
		"null_key":          {strings.Replace(tencentConfigJSON(), `"`+tencentTestKey+`"`, "null", 1), false},
		"number_key":        {strings.Replace(tencentConfigJSON(), `"`+tencentTestKey+`"`, "42", 1), false},
		"missing_key":       {strings.Replace(tencentConfigJSON(), `,"apiKey":"`+tencentTestKey+`"`, "", 1), false},
		"duplicate_key":     {strings.Replace(tencentConfigJSON(), `"apiKey":`, `"apiKey":"old","apiKey":`, 1), false},
		"service_id":        {strings.Replace(tencentConfigJSON(), `"provider":`, `"service_id":"synthetic","provider":`, 1), false},
		"double_v1":         {strings.Replace(tencentConfigJSON(), "/v1\"", "/v1/v1\"", 1), false},
		"unknown_model":     {strings.Replace(tencentConfigJSON(), `"hy3"`, `"unselected"`, 1), false},
		"over_limit":        {strings.Replace(tencentConfigJSON(), "768", "4097", 1), false},
		"oversize":          {strings.Repeat(" ", MaxTencentConfigBytes+1), false},
	} {
		t.Run(name, func(t *testing.T) {
			if err := os.WriteFile(path, []byte(test.raw), 0600); err != nil {
				t.Fatal("could not write synthetic configuration")
			}
			c, err := LoadTencentTokenHubConfigFromEnvironment(path, lookup)
			if test.valid {
				if err != nil || !c.valid() || c.credential() != "opaque-synthetic" {
					t.Fatal("valid environment configuration rejected")
				}
			} else if !errors.Is(err, ErrTencentConfig) || c.valid() || c.credential() != "" {
				t.Fatal("environment override widened the closed file configuration")
			}
		})
	}
	for _, badPath := range []string{".env.tencent.local.json", filepath.Dir(path), filepath.Join(filepath.Dir(path), "other.json")} {
		if c, err := LoadTencentTokenHubConfigFromEnvironment(badPath, lookup); !errors.Is(err, ErrTencentConfig) || c.valid() {
			t.Fatal("environment override widened the local path boundary")
		}
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
