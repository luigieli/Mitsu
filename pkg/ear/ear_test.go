package ear

import (
	"testing"
)

func TestApplyFuzzyNameCorrection(t *testing.T) {
	e := &Ear{}
	tests := []struct {
		input    string
		expected string
	}{
		{"Hello mitzo", "Hello Mitsu"},
		{"mitso is here", "Mitsu is here"},
		{"metsu is cool", "Mitsu is cool"},
		{"I like mitsu", "I like Mitsu"},
		{"nothing to change", "nothing to change"},
	}

	for _, tt := range tests {
		result := e.ApplyFuzzyNameCorrection(tt.input)
		if result != tt.expected {
			t.Errorf("ApplyFuzzyNameCorrection(%q) = %q; want %q", tt.input, result, tt.expected)
		}
	}
}
