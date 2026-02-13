package common

import (
	"fmt"
	"strings"
	"sync"
	"time"
)

type Span struct {
	Name     string
	Duration time.Duration
}

type Profile struct {
	mu    sync.Mutex
	Spans []Span
}

func NewProfile() *Profile {
	return &Profile{
		Spans: make([]Span, 0),
	}
}

func (p *Profile) AddSpan(name string, duration time.Duration) {
	if p == nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.Spans = append(p.Spans, Span{Name: name, Duration: duration})
}

func (p *Profile) Summary() string {
	if p == nil {
		return "No profile data"
	}
	p.mu.Lock()
	defer p.mu.Unlock()

	var sb strings.Builder
	var total time.Duration
	for _, s := range p.Spans {
		sb.WriteString(fmt.Sprintf("%s=%v ", s.Name, s.Duration.Round(time.Millisecond)))
		total += s.Duration
	}
	return fmt.Sprintf("TOTAL: %v [%s]", total.Round(time.Millisecond), strings.TrimSpace(sb.String()))
}

type SpeechEntry struct {
	Text      string
	Language  string // Detected language code (en, pt)
	Timestamp time.Time
	Profile   *Profile
}

type LLMEntry struct {
	Text          string
	InputLanguage string // The language the user spoke
	Profile       *Profile
}

type SpeechText chan SpeechEntry
type LLMResponse chan LLMEntry
