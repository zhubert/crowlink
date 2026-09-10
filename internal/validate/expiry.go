package validate

import (
	"encoding/json"
	"fmt"
	"time"
)

// maxExpiresIn bounds how far in the future a link may be set to expire.
const maxExpiresIn = 10 * 365 * 24 * time.Hour

// ExpiresIn parses the optional "expires_in" field of a shorten request into
// a time-to-live. The field may be a number of seconds (`3600`) or a Go
// duration string (`"1h30m"`). An absent, empty or null field yields a zero
// duration, meaning the link never expires.
//
// It returns a descriptive error suitable for surfacing as an HTTP 400 if the
// value is not a number or duration string, is not positive, or exceeds the
// maximum supported expiry.
func ExpiresIn(raw json.RawMessage) (time.Duration, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return 0, nil
	}

	var ttl time.Duration

	switch raw[0] {
	case '"':
		var s string
		if err := json.Unmarshal(raw, &s); err != nil {
			return 0, fmt.Errorf("expires_in could not be parsed: %w", err)
		}
		if s == "" {
			return 0, nil
		}
		d, err := time.ParseDuration(s)
		if err != nil {
			return 0, fmt.Errorf("expires_in %q is not a valid duration (try \"30m\" or \"24h\")", s)
		}
		ttl = d
	default:
		var secs float64
		if err := json.Unmarshal(raw, &secs); err != nil {
			return 0, fmt.Errorf("expires_in must be a number of seconds or a duration string like \"24h\"")
		}
		ttl = time.Duration(secs * float64(time.Second))
	}

	if ttl <= 0 {
		return 0, fmt.Errorf("expires_in must be positive")
	}
	if ttl > maxExpiresIn {
		return 0, fmt.Errorf("expires_in exceeds the maximum of %s", maxExpiresIn)
	}

	return ttl, nil
}
