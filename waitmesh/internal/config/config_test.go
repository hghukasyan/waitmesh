package config_test

import (
	"testing"

	"github.com/waitmesh/waitmesh/internal/config"
)

func TestIsNonLoopbackListen(t *testing.T) {
	cases := map[string]bool{
		"127.0.0.1:7777": false,
		"localhost:7777": false,
		"[::1]:7777":     false,
		":7777":          true,
		"0.0.0.0:7777":   true,
		"[::]:7777":      true,
		"192.168.1.1:1":  true,
	}
	for addr, want := range cases {
		if got := config.IsNonLoopbackListen(addr); got != want {
			t.Fatalf("%s: got %v want %v", addr, got, want)
		}
	}
}

func TestValidateRequiresTokenForRemote(t *testing.T) {
	c := config.Default()
	c.ListenAddr = "0.0.0.0:7777"
	if err := c.Validate(); err == nil {
		t.Fatal("expected token required")
	}
	c.AuthToken = "secret"
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
}
