package config

import "testing"

func TestLoadControlPlaneGeneratesSessionSecretWhenUnset(t *testing.T) {
	t.Setenv("CONTROL_PLANE_SESSION_SECRET", "")
	cfg := LoadControlPlane()
	if cfg.SessionSecret == "" {
		t.Fatal("expected generated session secret")
	}
	if cfg.SessionSecret == "dev-session-secret" {
		t.Fatal("session secret must not fall back to the public development default")
	}
}

func TestLoadControlPlaneUsesConfiguredSessionSecret(t *testing.T) {
	t.Setenv("CONTROL_PLANE_SESSION_SECRET", "configured-secret")
	cfg := LoadControlPlane()
	if cfg.SessionSecret != "configured-secret" {
		t.Fatalf("expected configured secret, got %q", cfg.SessionSecret)
	}
}
