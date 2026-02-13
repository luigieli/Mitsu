package common

import "time"

type SpeechEntry struct {
	Text      string
	Timestamp time.Time
}

type SpeechText chan SpeechEntry
type LLMResponse chan string
