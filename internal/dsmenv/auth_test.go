package dsmenv

import (
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
