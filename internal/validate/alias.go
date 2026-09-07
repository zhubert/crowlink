package validate

import (
	"fmt"
	"strings"
)

const (
	minAliasLen = 1
	maxAliasLen = 64
)

// reservedAliases are path segments the server itself serves; an alias
// matching one would be shadowed by that route, so they are rejected.
// Matching is case-insensitive.
var reservedAliases = map[string]bool{
	"healthz": true,
	"shorten": true,
	"metrics": true,
}

// Alias validates a caller-supplied custom alias: it must be between 1 and 64
// characters drawn from [A-Za-z0-9_-], and must not collide with a reserved
// server path. It returns a descriptive error suitable for surfacing as an
// HTTP 400 if the alias is invalid.
func Alias(alias string) error {
	if alias == "" {
		return fmt.Errorf("alias must not be empty")
	}

	if len(alias) < minAliasLen || len(alias) > maxAliasLen {
		return fmt.Errorf("alias must be between %d and %d characters", minAliasLen, maxAliasLen)
	}

	for _, r := range alias {
		switch {
		case r >= 'a' && r <= 'z',
			r >= 'A' && r <= 'Z',
			r >= '0' && r <= '9',
			r == '_', r == '-':
		default:
			return fmt.Errorf("alias contains invalid character %q; only letters, digits, '_' and '-' are allowed", r)
		}
	}

	if reservedAliases[strings.ToLower(alias)] {
		return fmt.Errorf("alias %q is reserved", alias)
	}

	return nil
}
