package mouth

import (
	"strings"
	"testing"
)

func TestBuildFilterChain(t *testing.T) {
	m := &Mouth{}
	config := VoiceConfig{}

	chain := m.BuildFilterChain(config)
	
	expectedParts := []string{
		"asetrate=24000*1.15",
		"atempo=1/1.15",
		"highpass=f=200",
		"equalizer=f=4000",
		"compand",
	}

	for _, part := range expectedParts {
		if !strings.Contains(chain, part) {
			t.Errorf("BuildFilterChain() output missing expected part: %q", part)
		}
	}
}
