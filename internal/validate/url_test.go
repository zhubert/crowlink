package validate_test

import (
	"strings"
	"testing"

	"github.com/zhubert/crowlink/internal/validate"
)

func TestURL(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		input   string
		wantErr bool
	}{
		{
			name:    "valid http URL",
			input:   "http://example.com/path",
			wantErr: false,
		},
		{
			name:    "valid https URL",
			input:   "https://example.com/path?q=1#anchor",
			wantErr: false,
		},
		{
			name:    "empty string",
			input:   "",
			wantErr: true,
		},
		{
			name:    "URL exceeds 2048 characters",
			input:   "https://example.com/" + strings.Repeat("a", 2048),
			wantErr: true,
		},
		{
			name:    "relative URL (no scheme)",
			input:   "/relative/path",
			wantErr: true,
		},
		{
			name:    "javascript scheme",
			input:   "javascript:alert(1)",
			wantErr: true,
		},
		{
			name:    "file scheme",
			input:   "file:///etc/passwd",
			wantErr: true,
		},
		{
			name:    "ftp scheme",
			input:   "ftp://example.com/file.txt",
			wantErr: true,
		},
		{
			name:    "scheme only (no host)",
			input:   "https://",
			wantErr: true,
		},
		{
			name:    "URL with path but no host",
			input:   "https:///just-a-path",
			wantErr: true,
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := validate.URL(tc.input)
			if tc.wantErr && err == nil {
				t.Errorf("URL(%q): expected an error but got nil", tc.input)
			}
			if !tc.wantErr && err != nil {
				t.Errorf("URL(%q): expected no error but got: %v", tc.input, err)
			}
		})
	}
}
