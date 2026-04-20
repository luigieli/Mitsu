package brain

import (
	"reflect"
	"testing"
)

func TestSplitSentences(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected []string
	}{
		{
			name:     "Simple sentences",
			input:    "Hello world. How are you?",
			expected: []string{"Hello world.", "How are you?"},
		},
		{
			name:     "Abbreviation Mr.",
			input:    "Mr. Smith is here. He is nice.",
			expected: []string{"Mr. Smith is here.", "He is nice."},
		},
		{
			name:     "Abbreviation U.S.",
			input:    "The U.S. is a country. It is big.",
			expected: []string{"The U.S.", "is a country.", "It is big."},
		},
		{
			name:     "Multiple punctuation",
			input:    "What?! No way. That's cool!",
			expected: []string{"What?!", "No way.", "That's cool!"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := SplitSentences(tt.input)
			if !reflect.DeepEqual(got, tt.expected) {
				t.Errorf("SplitSentences() = %#v, want %#v", got, tt.expected)
			}
		})
	}
}
