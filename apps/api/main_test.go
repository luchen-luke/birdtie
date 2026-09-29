package main

import "testing"

func TestIsLoopbackHost(t *testing.T) {
	for _, host := range []string{"localhost", "127.0.0.1", "::1"} {
		if !isLoopbackHost(host) {
			t.Fatalf("loopback host rejected: %s", host)
		}
	}
	for _, host := range []string{"", "0.0.0.0", "db", "192.168.1.10", "example.org"} {
		if isLoopbackHost(host) {
			t.Fatalf("non-loopback host accepted: %s", host)
		}
	}
}
