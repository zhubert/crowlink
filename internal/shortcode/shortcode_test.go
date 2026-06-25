package shortcode_test

import (
	"math"
	"testing"

	"github.com/zhubert/crowlink/internal/shortcode"
)

func TestEncode_KnownValues(t *testing.T) {
	tests := []struct {
		n    uint64
		want string
	}{
		{0, "0"},
		{1, "1"},
		{9, "9"},
		{10, "A"},
		{35, "Z"},
		{36, "a"},
		{61, "z"},
		{62, "10"},
		{63, "11"},
	}
	for _, tc := range tests {
		got := shortcode.Encode(tc.n)
		if got != tc.want {
			t.Errorf("Encode(%d) = %q; want %q", tc.n, got, tc.want)
		}
	}
}

func TestRoundTrip(t *testing.T) {
	values := []uint64{
		0,
		1,
		61,
		62,
		100,
		999,
		123456,
		9999999,
		1<<32 - 1,
		1 << 32,
		math.MaxUint64,
	}
	for _, n := range values {
		encoded := shortcode.Encode(n)
		decoded, err := shortcode.Decode(encoded)
		if err != nil {
			t.Errorf("Decode(Encode(%d)) returned error: %v", n, err)
			continue
		}
		if decoded != n {
			t.Errorf("Decode(Encode(%d)) = %d; want %d (encoded=%q)", n, decoded, n, encoded)
		}
	}
}

func TestDecode_Errors(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{"empty string", ""},
		{"space character", "a b"},
		{"exclamation mark", "!"},
		{"emoji", "aB3x🎉"},
		{"underscore", "abc_def"},
	}
	for _, tc := range tests {
		_, err := shortcode.Decode(tc.input)
		if err == nil {
			t.Errorf("Decode(%q) expected error, got nil", tc.input)
		}
	}
}
