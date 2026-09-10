package validate_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/zhubert/crowlink/internal/validate"
)

func TestExpiresIn(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		input   string // raw JSON for the expires_in field
		want    time.Duration
		wantErr bool
	}{
		{name: "absent", input: "", want: 0},
		{name: "null", input: "null", want: 0},
		{name: "empty string", input: `""`, want: 0},
		{name: "seconds", input: "3600", want: time.Hour},
		{name: "fractional seconds", input: "1.5", want: 1500 * time.Millisecond},
		{name: "duration string", input: `"1h30m"`, want: 90 * time.Minute},
		{name: "duration string seconds", input: `"45s"`, want: 45 * time.Second},
		{name: "maximum", input: `"87600h"`, want: 10 * 365 * 24 * time.Hour},
		{name: "zero seconds", input: "0", wantErr: true},
		{name: "negative seconds", input: "-5", wantErr: true},
		{name: "negative duration", input: `"-1h"`, wantErr: true},
		{name: "beyond maximum", input: `"87601h"`, wantErr: true},
		{name: "unparseable duration", input: `"soon"`, wantErr: true},
		{name: "bare number string", input: `"3600"`, wantErr: true},
		{name: "boolean", input: "true", wantErr: true},
		{name: "object", input: `{"seconds":60}`, wantErr: true},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, err := validate.ExpiresIn(json.RawMessage(tc.input))
			if tc.wantErr {
				if err == nil {
					t.Fatalf("ExpiresIn(%s) = %v, nil; want an error", tc.input, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("ExpiresIn(%s) returned unexpected error: %v", tc.input, err)
			}
			if got != tc.want {
				t.Errorf("ExpiresIn(%s) = %v; want %v", tc.input, got, tc.want)
			}
		})
	}
}
