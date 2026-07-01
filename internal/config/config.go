// Package config centralizes crowlink's runtime configuration, loading
// values from defaults, environment variables, and command-line flags with
// the following precedence (lowest to highest):
//
//	defaults < environment variables < command-line flags
package config

import (
	"flag"
	"fmt"
	"os"
)

// Default configuration values, used when neither an environment variable
// nor a flag overrides them.
const (
	defaultAddr    = ":8080"
	defaultBaseURL = "http://localhost:8080"
	defaultStore   = "mem"
)

// Config holds the runtime configuration for the crowlink server.
type Config struct {
	// Addr is the address the HTTP server listens on, e.g. ":8080".
	Addr string
	// BaseURL is the externally-visible base URL used to construct short
	// URLs returned from POST /shorten.
	BaseURL string
	// Store selects the storage backend. Currently only "mem" is supported.
	Store string
}

// validStores enumerates the recognized values for Store.
var validStores = map[string]bool{
	"mem": true,
}

// Load builds a Config from defaults, then environment variables (via
// getenv), then command-line flags parsed from args. Flags take precedence
// over environment variables, which take precedence over defaults.
//
// args is typically os.Args[1:] and getenv is typically os.Getenv; both are
// accepted as parameters so callers (and tests) can inject alternate
// sources without touching real process state.
func Load(args []string, getenv func(string) string) (*Config, error) {
	addr := defaultAddr
	baseURL := defaultBaseURL
	store := defaultStore

	if v := getenv("ADDR"); v != "" {
		addr = v
	}
	if v := getenv("BASE_URL"); v != "" {
		baseURL = v
	}
	if v := getenv("STORE"); v != "" {
		store = v
	}

	fs := flag.NewFlagSet("crowlink", flag.ContinueOnError)
	addrFlag := fs.String("addr", addr, "address for the HTTP server to listen on")
	baseURLFlag := fs.String("base-url", baseURL, "externally-visible base URL used to build short URLs")
	storeFlag := fs.String("store", store, "storage backend selector (mem)")

	if err := fs.Parse(args); err != nil {
		return nil, err
	}

	cfg := &Config{
		Addr:    *addrFlag,
		BaseURL: *baseURLFlag,
		Store:   *storeFlag,
	}

	if !validStores[cfg.Store] {
		return nil, fmt.Errorf("invalid STORE value %q: must be one of: mem", cfg.Store)
	}

	return cfg, nil
}

// Get loads configuration from the real process environment and
// command-line arguments.
func Get() (*Config, error) {
	return Load(os.Args[1:], os.Getenv)
}
