package main

import "testing"

func TestDefaultAddressIsHighLoopback(t *testing.T) {
	t.Setenv("PORT", "")
	cfg, err := parseConfig(nil)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.addr != "127.0.0.1:19081" {
		t.Fatalf("unexpected default: %s", cfg.addr)
	}
}

func TestPortEnvironmentAndConflict(t *testing.T) {
	t.Setenv("PORT", "19123")
	cfg, err := parseConfig(nil)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.addr != "127.0.0.1:19123" {
		t.Fatalf("unexpected PORT addr: %s", cfg.addr)
	}
	if _, err := parseConfig([]string{"-addr=127.0.0.1:19124"}); err == nil {
		t.Fatal("expected conflict")
	}
}

func TestRejectNonLoopbackAddress(t *testing.T) {
	t.Setenv("PORT", "")
	if _, err := parseConfig([]string{"-addr=0.0.0.0:19081"}); err == nil {
		t.Fatal("expected non-loopback rejection")
	}
}
