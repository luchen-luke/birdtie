package modelgateway

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

const TencentTokenHubBaseURL = "https://tokenhub.tencentmaas.com/v1"
const TencentTokenHubModel = "hy3"
const MaxTencentConfigBytes = 8 * 1024

var ErrTencentConfig = errors.New("invalid Tencent TokenHub server configuration")

// TencentTokenHubConfig is server-local transport configuration. It is not a
// source/egress grant, monetary reservation, free resource receipt or LIVE gate.
// Load only an approved ignored server file; never send this object to a client.
type TencentTokenHubConfig struct {
	apiKey          string
	maxOutputTokens int
}

func (TencentTokenHubConfig) String() string   { return "TencentTokenHubConfig{redacted}" }
func (TencentTokenHubConfig) GoString() string { return "TencentTokenHubConfig{redacted}" }
func (TencentTokenHubConfig) Format(s fmt.State, _ rune) {
	_, _ = io.WriteString(s, "TencentTokenHubConfig{redacted}")
}
func (TencentTokenHubConfig) MarshalJSON() ([]byte, error) { return nil, ErrTencentConfig }
func (c *TencentTokenHubConfig) UnmarshalJSON([]byte) error {
	*c = TencentTokenHubConfig{}
	return ErrTencentConfig
}

func (c TencentTokenHubConfig) valid() bool {
	if len(c.apiKey) < 1 || len(c.apiKey) > 4096 || c.maxOutputTokens < 1 || c.maxOutputTokens > 4096 {
		return false
	}
	for _, b := range []byte(c.apiKey) {
		if b < 33 || b > 126 {
			return false
		}
	}
	return true
}

// ParseTencentTokenHubConfig accepts the existing ignored JSON shape only.
// Provider, URL, model and token limit are explicit; unknown fields, duplicate
// keys, aliases, embedded credentials and alternate endpoints fail closed.
func ParseTencentTokenHubConfig(raw []byte) (TencentTokenHubConfig, error) {
	var empty TencentTokenHubConfig
	if len(raw) == 0 || len(raw) > MaxTencentConfigBytes {
		return empty, ErrTencentConfig
	}
	obj, err := strictObject(raw, []string{"provider", "baseUrl", "model", "maxOutputTokens", "apiKey"}, nil)
	if err != nil {
		return empty, ErrTencentConfig
	}
	provider, e1 := stringValue(obj["provider"], 80)
	base, e2 := stringValue(obj["baseUrl"], 256)
	model, e3 := stringValue(obj["model"], 80)
	key, e4 := stringValue(obj["apiKey"], 4096)
	var limit int
	if e1 != nil || e2 != nil || e3 != nil || e4 != nil || provider != "tencent_tokenhub" || base != TencentTokenHubBaseURL || model != TencentTokenHubModel || decodeTencentInt(obj["maxOutputTokens"], &limit) != nil {
		return empty, ErrTencentConfig
	}
	c := TencentTokenHubConfig{apiKey: key, maxOutputTokens: limit}
	if !c.valid() {
		return empty, ErrTencentConfig
	}
	return c, nil
}

// LoadTencentTokenHubConfig does not log or retain the path/read error. The
// narrowly named regular file must be ignored by the repository/deployment.
// Git ignore is an operator prerequisite, not a runtime authorization check.
func LoadTencentTokenHubConfig(path string) (TencentTokenHubConfig, error) {
	var empty TencentTokenHubConfig
	if !filepath.IsAbs(path) || filepath.Base(path) != ".env.tencent.local.json" {
		return empty, ErrTencentConfig
	}
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Size() > MaxTencentConfigBytes {
		return empty, ErrTencentConfig
	}
	f, err := os.Open(path)
	if err != nil {
		return empty, ErrTencentConfig
	}
	defer f.Close()
	opened, err := f.Stat()
	if err != nil || !opened.Mode().IsRegular() || !os.SameFile(info, opened) {
		return empty, ErrTencentConfig
	}
	raw, err := io.ReadAll(io.LimitReader(f, MaxTencentConfigBytes+1))
	defer clear(raw)
	if err != nil {
		return empty, ErrTencentConfig
	}
	return ParseTencentTokenHubConfig(raw)
}
