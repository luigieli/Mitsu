package mouth

import (
	"strings"
	"testing"
)

func TestBuildFilterChain(t *testing.T) {
	m := &Mouth{}
	config := VoiceConfig{
		Highpass: 100, Lowpass: 15000, BoxyGain: -5, PresenceGain: 5, SparkleGain: 2,
		Pitch: 1.1, Speed: 1.2, FormantPreserved: true,
		DeesserIntensity: 0.1, ExciterAmount: 1.5, StereoWidth: 1.0, LoudnormI: -14,
	}

	chain := m.BuildFilterChain(config)
	
	expectedParts := []string{
		"highpass=f=100",
		"lowpass=f=15000",
		"pitch=1.10",
		"tempo=1.20",
		"formant=preserved",
	}

	for _, part := range expectedParts {
		if !strings.Contains(chain, part) {
			t.Errorf("BuildFilterChain() output missing expected part: %q", part)
		}
	}
}
