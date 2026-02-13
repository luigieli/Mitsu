package common

import "time"

type SpeechEntry struct {
	Text      string
	Language  string // Detected language code (en, pt)
	Timestamp time.Time
}

type LLMEntry struct {
	Text          string
	InputLanguage string // The language the user spoke
}

type SpeechText chan SpeechEntry
type LLMResponse chan LLMEntry
