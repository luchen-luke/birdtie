package devauth

import "testing"

func TestFixedPublicTestChannelRequiresExactOptInAndKeepsLoopback(t *testing.T) {
	local := TestChannel{Enabled: true, APIAddress: "127.0.0.1:18114", DatabaseHost: "localhost", AllowedOrigins: []string{"http://localhost:7357"}}
	if ValidateTestChannel(local) != nil || Code != "123456" {
		t.Fatal("existing fixed-code local channel changed")
	}
	public := local
	public.AllowedOrigins = []string{"http://localhost:7357", AuthorizedPublicTestOrigin}
	if ValidateTestChannel(public) == nil {
		t.Fatal("public fixed-code channel opened without explicit opt-in")
	}
	public.PublicTestOrigin = AuthorizedPublicTestOrigin
	if ValidateTestChannel(public) != nil {
		t.Fatal("exact authorized HTTPS test URL rejected")
	}
	invalid := []TestChannel{public, public, public, public, public, public}
	invalid[0].APIAddress = "0.0.0.0:18114"
	invalid[1].DatabaseHost = "outside.invalid"
	invalid[2].PublicTestOrigin = "https://47.110.154.133/"
	invalid[3].AllowedOrigins = []string{"https://outside.invalid"}
	invalid[4].AllowedOrigins = []string{"http://localhost:7357"}
	invalid[5].Enabled = false
	for i, c := range invalid {
		if ValidateTestChannel(c) == nil {
			t.Fatal("invalid public test boundary accepted", i)
		}
	}
	// Ordinary production configuration is unaffected when dev login is off.
	if ValidateTestChannel(TestChannel{APIAddress: "0.0.0.0:8080", DatabaseHost: "outside.invalid", AllowedOrigins: []string{"https://production.invalid"}}) != nil {
		t.Fatal("disabled test channel imposed dev-only runtime restrictions")
	}
	socket := local
	socket.DatabaseHost = "/var/run/postgresql"
	if ValidateTestChannel(socket) == nil {
		t.Fatal("API implicitly acquired importer-only socket option")
	}
	socket.AllowUnixDatabaseSocket = true
	if ValidateTestChannel(socket) != nil {
		t.Fatal("existing importer local socket support changed")
	}
}
