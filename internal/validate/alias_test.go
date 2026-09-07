package validate_test

import (
	"strings"
	"testing"

	"github.com/zhubert/crowlink/internal/validate"
)

func TestAlias(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		input   string
		wantErr bool
	}{
		{name: "simple word", input: "my-link", wantErr: false},
		{name: "mixed charset", input: "Ab_9-Z", wantErr: false},
		{name: "single character", input: "a", wantErr: false},
		{name: "maximum length", input: strings.Repeat("a", 64), wantErr: false},
		{name: "empty string", input: "", wantErr: true},
		{name: "too long", input: strings.Repeat("a", 65), wantErr: true},
		{name: "contains slash", input: "foo/bar", wantErr: true},
		{name: "contains space", input: "foo bar", wantErr: true},
		{name: "contains dot", input: "foo.bar", wantErr: true},
		{name: "non-ASCII", input: "crów", wantErr: true},
		{name: "reserved healthz", input: "healthz", wantErr: true},
		{name: "reserved shorten", input: "shorten", wantErr: true},
		{name: "reserved metrics", input: "metrics", wantErr: true},
		{name: "reserved word in mixed case", input: "Healthz", wantErr: true},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			err := validate.Alias(tc.input)
			if tc.wantErr && err == nil {
				t.Errorf("Alias(%q) = nil; want an error", tc.input)
			}
			if !tc.wantErr && err != nil {
				t.Errorf("Alias(%q) = %v; want nil", tc.input, err)
			}
		})
	}
}
