package dsmenv

import (
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
)

// TestCookieCandidatesTriesEveryIdentifier covers the failure this
// normalization exists for: a browser holds a stale "id" beside the current
// one, and the order it sends them decides whether authenticate.cgi accepts the
// request.
func TestCookieCandidatesTriesEveryIdentifier(t *testing.T) {
	got := cookieCandidates("did=abc; id=stale; id=fresh")
	want := []string{"id=fresh; did=abc", "id=stale; did=abc"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("cookieCandidates() = %q, want %q", got, want)
	}
}

func TestCookieCandidatesCollapsesToASingleIdentifier(t *testing.T) {
	for _, candidate := range cookieCandidates("id=stale; id=fresh") {
		if got := parseCookie(candidate); len(got) != 1 || got[0].name != sessionCookie {
			t.Fatalf("candidate %q does not carry exactly one id cookie", candidate)
		}
	}
}

func TestCookieCandidatesDropsRepeatedValues(t *testing.T) {
	got := cookieCandidates("id=same; id=same")
	if len(got) != 1 {
		t.Fatalf("cookieCandidates() = %q, want a single candidate", got)
	}
}

func TestCookieCandidatesKeepsHeaderWithoutIdentifier(t *testing.T) {
	got := cookieCandidates("did=abc")
	if len(got) != 1 || got[0] != "did=abc" {
		t.Fatalf("cookieCandidates() = %q, want [did=abc]", got)
	}
}

func TestCookieCandidatesPreservesIdentifierlessCookies(t *testing.T) {
	got := cookieCandidates("did=abc; id=fresh; _SSID=xyz")
	want := []string{"id=fresh; did=abc; _SSID=xyz"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("cookieCandidates() = %q, want %q", got, want)
	}
}

func TestParseCookieSkipsMalformedPairs(t *testing.T) {
	got := parseCookie(" ; =empty; did=abc ; flag")
	want := []cookie{{name: "did", value: "abc"}, {name: "flag", value: ""}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("parseCookie() = %v, want %v", got, want)
	}
}

// SynoCgiIsAuthorized only accepts a session whose recorded login address
// matches REMOTE_ADDR, so the address nginx reports must not be the only one
// tried: the forwarded client address is preferred, then the direct peer.
func TestRemoteAddrCandidatesOrder(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/api/status", nil)
	request.RemoteAddr = "127.0.0.1:41234"
	request.Header.Set("X-Real-IP", "10.0.0.5")
	got := remoteAddrCandidates(request)
	if len(got) < 2 || got[0] != "10.0.0.5" || got[1] != "127.0.0.1" {
		t.Fatalf("remoteAddrCandidates() = %q, want the forwarded address first", got)
	}
}

func TestRemoteAddrCandidatesSkipsDuplicates(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/api/status", nil)
	request.RemoteAddr = "10.0.0.5:41234"
	request.Header.Set("X-Real-IP", "10.0.0.5")
	got := remoteAddrCandidates(request)
	seen := map[string]int{}
	for _, address := range got {
		seen[address]++
	}
	for address, count := range seen {
		if count > 1 {
			t.Fatalf("address %q appears %d times in %q", address, count, got)
		}
	}
}
