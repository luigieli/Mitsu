package ear

import (
	"testing"
)

// Whisper transcription tests are now handled via STT service integration.
func TestWhisperTranscription(t *testing.T) {
	t.Skip("Legacy whisper-cpp test removed. New STT tests should target the networked service.")
}
