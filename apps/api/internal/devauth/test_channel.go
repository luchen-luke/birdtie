package devauth

import (
	"errors"
	"net"
	"net/url"
	"strings"
)

// Explicitly authorized test URL, never a generic public-development bypass.
const AuthorizedPublicTestOrigin = "https://47.110.154.133"

var ErrTestChannel = errors.New("fixed_test_phone_channel_configuration_invalid")

type TestChannel struct {
	Enabled          bool
	APIAddress       string
	DatabaseHost     string
	AllowedOrigins   []string
	PublicTestOrigin string
	// Only the pre-existing importer accepts local PostgreSQL sockets. The API
	// caller leaves this false, preserving its original loopback-host boundary.
	AllowUnixDatabaseSocket bool
}

func testLoopbackHost(host string) bool {
	ip := net.ParseIP(host)
	return strings.EqualFold(host, "localhost") || ip != nil && ip.IsLoopback()
}

// Pure startup/import configuration validation; no network, Session, Agent or
// identity effects. Fixed 123456 remains dev_phone, never SMS ownership proof.
func ValidateTestChannel(c TestChannel) error {
	if !c.Enabled {
		if c.PublicTestOrigin != "" {
			return ErrTestChannel
		}
		return nil
	}
	if c.PublicTestOrigin != "" && c.PublicTestOrigin != AuthorizedPublicTestOrigin {
		return ErrTestChannel
	}
	host, _, e := net.SplitHostPort(c.APIAddress)
	if e != nil || !testLoopbackHost(host) {
		return ErrTestChannel
	}
	if !testLoopbackHost(c.DatabaseHost) && !(c.AllowUnixDatabaseSocket && strings.HasPrefix(c.DatabaseHost, "/")) {
		return ErrTestChannel
	}
	publicSeen := false
	for _, raw := range c.AllowedOrigins {
		origin := strings.TrimSpace(raw)
		if origin == "" {
			continue
		}
		if origin == AuthorizedPublicTestOrigin && c.PublicTestOrigin == AuthorizedPublicTestOrigin {
			publicSeen = true
			continue
		}
		u, e := url.Parse(origin)
		if e != nil || !u.IsAbs() || u.Host == "" || u.User != nil || u.Path != "" || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || (u.Scheme != "http" && u.Scheme != "https") || !testLoopbackHost(u.Hostname()) {
			return ErrTestChannel
		}
	}
	if c.PublicTestOrigin != "" && !publicSeen {
		return ErrTestChannel
	}
	return nil
}
