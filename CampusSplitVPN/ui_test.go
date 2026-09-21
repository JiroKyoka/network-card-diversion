package main

import (
	"net/http/httptest"
	"strings"
	"testing"
	"time"
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
	if !strings.Contains(allowed.Body.String(), "endpoint('lifetime')") {
		t.Fatal("rendered page does not keep the browser lifetime connection")
	}
}

func TestLastPageDisconnectExitsWithoutChangingRoutes(t *testing.T) {
	shutdown := make(chan struct{}, 1)
	app := &webServer{
		closeDelay: 10 * time.Millisecond,
		shutdown: func() error {
			shutdown <- struct{}{}
			return nil
		},
	}
	if !app.pageConnected() {
		t.Fatal("first page connection was rejected")
	}
	app.pageDisconnected()
	select {
	case <-shutdown:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("application did not shut down after its last page disconnected")
	}
}

func TestPageReloadCancelsAutomaticExit(t *testing.T) {
	shutdown := make(chan struct{}, 1)
	app := &webServer{
		closeDelay: 30 * time.Millisecond,
		shutdown: func() error {
			shutdown <- struct{}{}
			return nil
		},
	}
	app.pageConnected()
	app.pageDisconnected()
	time.Sleep(5 * time.Millisecond)
	app.pageConnected()
	select {
	case <-shutdown:
		t.Fatal("page reload unexpectedly shut down the application")
	case <-time.After(80 * time.Millisecond):
	}
}
