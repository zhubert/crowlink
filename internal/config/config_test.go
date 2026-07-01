package config_test

import (
	"strings"
	"testing"

	"github.com/zhubert/crowlink/internal/config"
)

// noEnv simulates an environment with no relevant variables set.
func noEnv(string) string { return "" }

// envMap returns a getenv func backed by the given map.
func envMap(m map[string]string) func(string) string {
	return func(key string) string {
		return m[key]
	}
}

func TestLoad_Defaults(t *testing.T) {
	cfg, err := config.Load(nil, noEnv)
	if err != nil {
		t.Fatalf("Load() unexpected error: %v", err)
	}

	if cfg.Addr != ":8080" {
		t.Errorf("Addr = %q; want %q", cfg.Addr, ":8080")
	}
	if cfg.BaseURL != "http://localhost:8080" {
		t.Errorf("BaseURL = %q; want %q", cfg.BaseURL, "http://localhost:8080")
	}
	if cfg.Store != "mem" {
		t.Errorf("Store = %q; want %q", cfg.Store, "mem")
	}
}

func TestLoad_EnvOverride(t *testing.T) {
	getenv := envMap(map[string]string{
		"ADDR":     ":9090",
		"BASE_URL": "https://short.example.com",
		"STORE":    "mem",
	})

	cfg, err := config.Load(nil, getenv)
	if err != nil {
		t.Fatalf("Load() unexpected error: %v", err)
	}

	if cfg.Addr != ":9090" {
		t.Errorf("Addr = %q; want %q", cfg.Addr, ":9090")
	}
	if cfg.BaseURL != "https://short.example.com" {
		t.Errorf("BaseURL = %q; want %q", cfg.BaseURL, "https://short.example.com")
	}
	if cfg.Store != "mem" {
		t.Errorf("Store = %q; want %q", cfg.Store, "mem")
	}
}

func TestLoad_FlagOverridesEnv(t *testing.T) {
	getenv := envMap(map[string]string{
		"ADDR":     ":9090",
		"BASE_URL": "https://from-env.example.com",
	})

	args := []string{
		"-addr", ":9999",
		"-base-url", "https://from-flag.example.com",
	}

	cfg, err := config.Load(args, getenv)
	if err != nil {
		t.Fatalf("Load() unexpected error: %v", err)
	}

	if cfg.Addr != ":9999" {
		t.Errorf("Addr = %q; want flag value %q", cfg.Addr, ":9999")
	}
	if cfg.BaseURL != "https://from-flag.example.com" {
		t.Errorf("BaseURL = %q; want flag value %q", cfg.BaseURL, "https://from-flag.example.com")
	}
}

func TestLoad_FlagOverridesDefaults(t *testing.T) {
	cfg, err := config.Load([]string{"-store", "mem"}, noEnv)
	if err != nil {
		t.Fatalf("Load() unexpected error: %v", err)
	}
	if cfg.Store != "mem" {
		t.Errorf("Store = %q; want %q", cfg.Store, "mem")
	}
}

func TestLoad_InvalidStore(t *testing.T) {
	getenv := envMap(map[string]string{"STORE": "redis"})

	_, err := config.Load(nil, getenv)
	if err == nil {
		t.Fatal("Load() expected error for invalid STORE value, got nil")
	}
	if !strings.Contains(err.Error(), "redis") {
		t.Errorf("error %q does not mention invalid value %q", err.Error(), "redis")
	}
}

func TestLoad_InvalidStoreFromFlag(t *testing.T) {
	_, err := config.Load([]string{"-store", "bogus"}, noEnv)
	if err == nil {
		t.Fatal("Load() expected error for invalid -store flag value, got nil")
	}
}
