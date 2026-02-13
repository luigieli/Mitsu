package ear

import (
	"github.com/abadojack/whatlanggo"
	"testing"
)

func TestWhatLangGoDetection(t *testing.T) {
	tests := []struct {
		name     string
		text     string
		expected string
	}{
		{
			name:     "English Text",
			text:     "The weather is very nice today in Seattle.",
			expected: "en",
		},
		{
			name:     "Portuguese Text",
			text:     "O tempo está muito bom hoje em São Paulo.",
			expected: "pt",
		},
		{
			name:     "Short English",
			text:     "Hello",
			expected: "en",
		},
		{
			name:     "Unmistakable Portuguese",
			text:     "A tecnologia de inteligência artificial está avançando muito rapidamente em todo o mundo.",
			expected: "pt",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			info := whatlanggo.Detect(tt.text)
			detected := "en"
			if info.Lang == whatlanggo.Por {
				detected = "pt"
			}

			if detected != tt.expected {
				t.Errorf("Detection failed for %s. Got: %q, Expected: %q", tt.name, detected, tt.expected)
			}
		})
	}
}
