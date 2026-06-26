package validate

import (
	"fmt"
	"net/url"
)

const maxURLLen = 2048

// URL validates that raw is an absolute http or https URL with a non-empty
// host. It returns a descriptive error suitable for surfacing as an HTTP 400
// if the URL is invalid.
func URL(raw string) error {
	if raw == "" {
		return fmt.Errorf("URL must not be empty")
	}

	if len(raw) > maxURLLen {
		return fmt.Errorf("URL exceeds maximum length of %d characters", maxURLLen)
	}

	parsed, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("URL could not be parsed: %w", err)
	}

	if parsed.Scheme == "" {
		return fmt.Errorf("URL must be absolute (missing scheme)")
	}

	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return fmt.Errorf("URL scheme %q is not allowed; only http and https are accepted", parsed.Scheme)
	}

	if parsed.Host == "" {
		return fmt.Errorf("URL must have a non-empty host")
	}

	return nil
}
