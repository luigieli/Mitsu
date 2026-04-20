package ear

import (
	"mitsu/pkg/common"
	"testing"

	"github.com/abadojack/whatlanggo"
)

func TestWhatLangGoDetection(t *testing.T) {
	tests := []struct {
		name     string
		text     string
		expected common.Language
	}{
		{
			name:     "English Text",
			text:     "The weather is very nice today in Seattle.",
			expected: common.LanguageEnglish,
		},
		{
			name:     "Portuguese Text",
			text:     "O tempo está muito bom hoje em São Paulo.",
			expected: common.LanguagePortuguese,
		},
		{
			name:     "Short English",
			text:     "Hello",
			expected: common.LanguageEnglish,
		},
		{
			name:     "Unmistakable Portuguese",
			text:     "A tecnologia de inteligência artificial está avançando muito rapidamente em todo o mundo.",
			expected: common.LanguagePortuguese,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			info := whatlanggo.Detect(tt.text)
			detected := common.LanguageEnglish
			if info.Lang == whatlanggo.Por {
				detected = common.LanguagePortuguese
			}

			if detected != tt.expected {
				t.Errorf("Detection failed for %s. Got: %q, Expected: %q", tt.name, detected, tt.expected)
			}
		})
	}
}
