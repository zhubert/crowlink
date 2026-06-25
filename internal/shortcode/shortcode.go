// Package shortcode provides base62 encoding and decoding for uint64 values.
// The alphabet is ordered as digits (0–9), uppercase letters (A–Z), then
// lowercase letters (a–z), giving 62 unique characters.
package shortcode

import (
	"errors"
)

const alphabet = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"
const base = uint64(len(alphabet)) // 62

// decodeTable maps byte values to their index in alphabet, or 255 for invalid.
var decodeTable [256]byte

func init() {
	for i := range decodeTable {
		decodeTable[i] = 255
	}
	for i := 0; i < len(alphabet); i++ {
		decodeTable[alphabet[i]] = byte(i)
	}
}

// Encode converts a uint64 to its base62 string representation.
// Encode(0) returns "0".
func Encode(n uint64) string {
	if n == 0 {
		return "0"
	}
	// Determine the digits in reverse order, then reverse.
	var buf [13]byte // ceil(64 * log(2) / log(62)) = 11; 13 is safe
	pos := len(buf)
	for n > 0 {
		pos--
		buf[pos] = alphabet[n%base]
		n /= base
	}
	return string(buf[pos:])
}

// Decode converts a base62 string back to a uint64.
// It returns an error if s is empty or contains any character outside the
// alphabet "0-9A-Za-z".
func Decode(s string) (uint64, error) {
	if len(s) == 0 {
		return 0, errors.New("shortcode: empty input")
	}
	var result uint64
	for i := 0; i < len(s); i++ {
		v := decodeTable[s[i]]
		if v == 255 {
			return 0, errors.New("shortcode: invalid character in input")
		}
		result = result*base + uint64(v)
	}
	return result, nil
}
