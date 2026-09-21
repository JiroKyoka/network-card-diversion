package main

import (
	"net/http/httptest"
	"strings"
	"testing"
)

func TestIndexRequiresToken(t *testing.T) {
	app := &webServer{token: "test-token"}

	denied := httptest.NewRecorder()
	app.index(denied, httptest.NewRequest("GET", "/", nil))
	if denied.Code != 403 {
		t.Fatalf("request without token returned %d, want 403", denied.Code)
	}

	allowed := httptest.NewRecorder()
	app.index(allowed, httptest.NewRequest("GET", "/?token=test-token", nil))
	if allowed.Code != 200 {
		t.Fatalf("request with token returned %d, want 200", allowed.Code)
	}
	if !strings.Contains(allowed.Body.String(), "校园 VPN 分流助手") || strings.Contains(allowed.Body.String(), "__TOKEN__") {
		t.Fatal("rendered page is missing its title or still contains the token placeholder")
	}
}
