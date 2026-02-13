package ear

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"testing"
)

func TestWhisperTranscription(t *testing.T) {
	// Skip if whisper-cpp or model is not available
	modelPath := "../../models/ggml-small-q5_1.bin"
	if _, err := os.Stat(modelPath); os.IsNotExist(err) {
		t.Skip("Whisper model not found, skipping integration test")
	}
	if _, err := exec.LookPath("./whisper-cpp"); err != nil {
		// Try to find it in the current dir too
		if _, err := os.Stat("../../whisper-cpp"); os.IsNotExist(err) {
			t.Skip("whisper-cpp binary not found, skipping integration test")
		}
	}

	tests := []struct {
		name     string
		wavFile  string
		lang     string
		expected string
	}{
		{
			name:     "English Greeting",
			wavFile:  "../../tests/data/hello_en.wav",
			lang:     "en",
			expected: "Hello Mitsu",
		},
		{
			name:     "Portuguese Question",
			wavFile:  "../../tests/data/ready_pt.wav",
			lang:     "pt",
			expected: "Olá Mitsu",
		},
	}

	e := &Ear{
		WhisperModel: modelPath,
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := os.Stat(tt.wavFile); os.IsNotExist(err) {
				t.Fatalf("Test data file not found: %s", tt.wavFile)
			}

			// We use a relative path to whisper-cpp if available, or assume it's in PATH
			cmdPath := "../../whisper-cpp"
			if _, err := os.Stat(cmdPath); os.IsNotExist(err) {
				cmdPath = "whisper-cpp"
			}

			cmd := exec.CommandContext(context.Background(), cmdPath, "-m", modelPath, "-f", tt.wavFile, "-nt", "-np", "-l", tt.lang, "-bs", "1")
			out, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("Whisper command failed: %v, Output: %s", err, string(out))
			}

			transcribed := strings.TrimSpace(string(out))
			corrected := e.ApplyFuzzyNameCorrection(transcribed)

			if !strings.Contains(strings.ToLower(corrected), strings.ToLower(tt.expected)) {
				t.Errorf("Transcription mismatch. Got: %q, Expected to contain: %q, Original output: %q", corrected, tt.expected, transcribed)
			}
		})
	}
}
