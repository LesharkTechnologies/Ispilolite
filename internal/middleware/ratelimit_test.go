package middleware

import (
	"net"
	"net/http"
	"testing"
)

func requestFrom(remote, forwarded string) *http.Request {
	r := &http.Request{
		RemoteAddr: remote,
		Header:     http.Header{},
	}
	if forwarded != "" {
		r.Header.Set("X-Forwarded-For", forwarded)
	}
	return r
}

func TestClientIPIgnoresForwardedHeaderFromUntrustedPeer(t *testing.T) {
	proxies := parseProxyNets("10.0.0.0/8")
	r := requestFrom("203.0.113.9:44322", "198.51.100.7")
	if got := clientIPWithProxies(r, proxies); got != "203.0.113.9" {
		t.Fatalf("spoofed header honoured: got %q", got)
	}
}

func TestClientIPUsesForwardedHeaderFromTrustedProxy(t *testing.T) {
	proxies := parseProxyNets("10.0.0.0/8")
	r := requestFrom("10.0.0.2:44322", "198.51.100.7")
	if got := clientIPWithProxies(r, proxies); got != "198.51.100.7" {
		t.Fatalf("expected client IP, got %q", got)
	}
}

func TestClientIPSkipsSpoofedEntriesLeftOfProxy(t *testing.T) {
	proxies := parseProxyNets("10.0.0.0/8")
	// Client sent "1.2.3.4"; the trusted proxy appended the real peer.
	r := requestFrom("10.0.0.2:44322", "1.2.3.4, 198.51.100.7")
	if got := clientIPWithProxies(r, proxies); got != "198.51.100.7" {
		t.Fatalf("expected right-most untrusted address, got %q", got)
	}
}

func TestClientIPFallsBackToRemoteForUntrustedChain(t *testing.T) {
	proxies := parseProxyNets("10.0.0.0/8")
	r := requestFrom("10.0.0.2:44322", "10.0.0.3, 10.0.0.4")
	if got := clientIPWithProxies(r, proxies); got != "10.0.0.2" {
		t.Fatalf("expected immediate peer, got %q", got)
	}
}

func TestDefaultTrustedProxiesIncludesLoopback(t *testing.T) {
	proxies := parseProxyNets("127.0.0.1/32,::1/128")
	for _, ip := range []string{"127.0.0.1", "::1"} {
		if !proxies.contains(net.ParseIP(ip)) {
			t.Fatalf("loopback proxy %s not trusted", ip)
		}
	}
	if proxies.contains(net.ParseIP("203.0.113.9")) {
		t.Fatal("public address incorrectly trusted as a proxy")
	}
}
