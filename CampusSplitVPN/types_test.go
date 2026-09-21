package main

import "testing"

func TestParseTargets(t *testing.T) {
	targets, err := parseTargets("172.25.24.135， 10.0.7.9; 172.25.0.9/16\n172.25.24.135")
	if err != nil {
		t.Fatal(err)
	}
	if len(targets) != 3 {
		t.Fatalf("got %d targets, want 3: %#v", len(targets), targets)
	}
	if targets[0].Prefix != "172.25.24.135" || !targets[0].Host {
		t.Fatalf("unexpected host target: %#v", targets[0])
	}
	if targets[2].Prefix != "172.25.0.0/16" || targets[2].Probe != "172.25.0.1" || targets[2].Host {
		t.Fatalf("unexpected network target: %#v", targets[2])
	}
}

func TestParseTargetsRejectsUnsafeText(t *testing.T) {
	bad := []string{"", "example.com", "172.25.24.135; rm -rf x", "2001:db8::1", "10.0.0.0/99"}
	for _, input := range bad {
		if _, err := parseTargets(input); err == nil {
			t.Errorf("parseTargets(%q) unexpectedly succeeded", input)
		}
	}
}
