package httpserver

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// nginx forwards "Host $host" (no port) while a browser sends an Origin that
// includes the port, so the comparison has to ignore the port.
func TestCheckSameOriginAcceptsBrowserOriginWithPort(t *testing.T) {
	request := httptest.NewRequest(http.MethodPost, "/api/disconnect", nil)
	request.Host = "10.147.223.115"
	request.Header.Set(requestMarkerHeader, "1")
	request.Header.Set("Origin", "https://10.147.223.115:5001")
	if aerr := checkSameOrigin(request); aerr != nil {
		t.Fatalf("checkSameOrigin() rejected a same-origin request: %v", aerr)
	}
}

func TestCheckSameOriginRejectsForeignOrigin(t *testing.T) {
	request := httptest.NewRequest(http.MethodPost, "/api/disconnect", nil)
	request.Host = "10.147.223.115"
	request.Header.Set(requestMarkerHeader, "1")
	request.Header.Set("Origin", "https://evil.example:5001")
	if aerr := checkSameOrigin(request); aerr == nil {
		t.Fatal("checkSameOrigin() accepted a foreign origin")
	}
}

func TestCheckSameOriginRequiresMarkerOnWrites(t *testing.T) {
	request := httptest.NewRequest(http.MethodPost, "/api/disconnect", nil)
	request.Host = "nas"
	if aerr := checkSameOrigin(request); aerr == nil {
		t.Fatal("checkSameOrigin() accepted a write without the request marker")
	}
}

func TestCheckSameOriginSkipsReads(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/api/status", nil)
	if aerr := checkSameOrigin(request); aerr != nil {
		t.Fatalf("checkSameOrigin() rejected a read: %v", aerr)
	}
}
