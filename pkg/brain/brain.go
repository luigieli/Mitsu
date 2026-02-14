package brain

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"mitsu/pkg/common"
	"net/http"
	"strings"
	"time"
)

type Brain struct {
	OllamaURL          string
	CurrentLang        string
	LanguageChangeChan chan string
	SpeechToBrain      common.SpeechText
	BrainToMouth       common.LLMResponse
	UiMessages         chan string
	ClearMemoryChan    chan struct{}
}

type ChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type OllamaChatRequest struct {
	Model    string        `json:"model"`
	Messages []ChatMessage `json:"messages"`
	Stream   bool          `json:"stream"`
}

type OllamaChatResponse struct {
	Model     string      `json:"model"`
	CreatedAt time.Time   `json:"created_at"`
	Message   ChatMessage `json:"message"`
	Done      bool        `json:"done"`
}

type OllamaPsResponse struct {
	Models []struct {
		Name string `json:"name"`
	} `json:"models"`
}

func (b *Brain) WarmUp(onLoading func()) error {
	fmt.Printf("Brain: Checking model status...\n")
	
	modelsToLoad := []string{"mitsu-en", "mitsu-pt"}
	actuallyNeedsLoading := false

	// Check /api/ps to see what's currently resident
	resp, err := http.Get(b.OllamaURL + "/api/ps")
	if err == nil {
		var ps OllamaPsResponse
		if err := json.NewDecoder(resp.Body).Decode(&ps); err == nil {
			resp.Body.Close()
			
			// Simple check: are both models there?
			for _, mName := range modelsToLoad {
				found := false
				for _, m := range ps.Models {
					if strings.HasPrefix(m.Name, mName) {
						found = true
						break
					}
				}
				if !found {
					actuallyNeedsLoading = true
					break
				}
			}
		}
	} else {
		actuallyNeedsLoading = true // Can't check, assume we need to try loading
	}

	if !actuallyNeedsLoading {
		fmt.Println("Brain: All models already resident in GPU.")
		return nil
	}

	// Trigger the vocal alert if provided
	if onLoading != nil {
		onLoading()
	}

	for _, m := range modelsToLoad {
		fmt.Printf("Brain: Pre-loading model %s into GPU...\n", m)
		reqBody, _ := json.Marshal(OllamaChatRequest{
			Model:    m,
			Messages: []ChatMessage{{Role: "user", Content: "hi"}},
			Stream:   false,
		})
		resp, err := http.Post(b.OllamaURL+"/api/chat", "application/json", bytes.NewBuffer(reqBody))
		if err != nil {
			fmt.Printf("Warning: Failed to load %s: %v\n", m, err)
			continue
		}
		resp.Body.Close()
	}

	return nil
}

func (b *Brain) Start() {
	fmt.Println("Brain Routine started with Streaming Overlap.")
	history := []ChatMessage{}
	splitters := ".?!:;," // Added comma

	for {
		select {
		case newLang := <-b.LanguageChangeChan:
			fmt.Printf("Brain: Hotswapping language to %s\n", newLang)
			b.CurrentLang = newLang
			// Optional: Clear memory on language swap to prevent mixing personas
			history = []ChatMessage{}
			continue
		case <-b.ClearMemoryChan:
			fmt.Println("Brain: Memory cleared.")
			history = []ChatMessage{}
			continue
		case entry := <-b.SpeechToBrain:
			if time.Since(entry.Timestamp) > 5*time.Second {
				fmt.Printf("Brain: Skipping expired context (%s)\n", entry.Text)
				continue
			}

			fmt.Printf("Brain processing: \"%s\"\n", entry.Text)
			msg, _ := json.Marshal(map[string]string{"text": "THINKING...", "type": "status"})
			select {
			case b.UiMessages <- string(msg):
			default:
			}

			// Language Enforcement
			langInstruction := "IMPORTANT: The user is speaking English. You MUST reply in English."
			if entry.Language == "pt" {
				langInstruction = "IMPORTANT: The user is speaking Portuguese. You MUST reply in Portuguese."
			}

			history = append(history, ChatMessage{Role: "user", Content: langInstruction + "\n\n" + entry.Text})
			if len(history) > 10 {
				history = history[len(history)-10:]
			}

			modelName := "mitsu-en"
			if entry.Language == "pt" {
				modelName = "mitsu-pt"
			}

			reqBody, _ := json.Marshal(OllamaChatRequest{
				Model:    modelName,
				Messages: history,
				Stream:   true,
			})

			brainStart := time.Now()
			resp, err := http.Post(b.OllamaURL+"/api/chat", "application/json", bytes.NewBuffer(reqBody))
			if err != nil {
				fmt.Printf("Brain Error: %v\n", err)
				continue
			}

			reader := bufio.NewReader(resp.Body)
			var fullResponseBuilder strings.Builder
			var sentenceBuilder strings.Builder
			firstSentenceDone := false
			
			// Lock in the language for this entire response to prevent voice-switching mid-stream
			responseLanguage := entry.Language

			for {
				line, err := reader.ReadBytes('\n')
				if err != nil {
					break
				}

				var ollamaResp OllamaChatResponse
				if err := json.Unmarshal(line, &ollamaResp); err != nil {
					continue
				}

				token := ollamaResp.Message.Content
				fullResponseBuilder.WriteString(token)
				sentenceBuilder.WriteString(token)

				// Check for sentence boundary (punctuation found)
				if strings.ContainsAny(token, splitters) {
					sentence := strings.TrimSpace(sentenceBuilder.String())
					words := strings.Fields(sentence)

					// Minimum words to avoid weird fragmentation (e.g., "Ai, ai, ai!")
					// We also want to send it if it's been building up for a while
					if len(words) >= 3 {
						if !firstSentenceDone {
							entry.Profile.AddSpan("Brain_TTFS", time.Since(brainStart))
							firstSentenceDone = true
						}

						// Send chunk to Mouth
						b.BrainToMouth <- common.LLMEntry{
							Text:          sentence,
							InputLanguage: responseLanguage,
							Profile:       entry.Profile,
							Done:          false,
						}
						sentenceBuilder.Reset()
					}
				}

				if ollamaResp.Done {
					break
				}
			}
			resp.Body.Close()

			// Send remaining buffer if any, marking it as Done
			leftover := strings.TrimSpace(sentenceBuilder.String())
			if len(leftover) > 0 {
				if !firstSentenceDone {
					entry.Profile.AddSpan("Brain_TTFS", time.Since(brainStart))
					firstSentenceDone = true
				}
				b.BrainToMouth <- common.LLMEntry{
					Text:          leftover,
					InputLanguage: responseLanguage,
					Profile:       entry.Profile,
					Done:          true,
				}
			} else {
				// If no leftover, send an empty Done signal to close the loop
				b.BrainToMouth <- common.LLMEntry{
					Text:          "",
					InputLanguage: responseLanguage,
					Profile:       entry.Profile,
					Done:          true,
				}
			}

			responseText := strings.TrimSpace(fullResponseBuilder.String())
			fmt.Printf("Mitsu: \"%s\"\n", responseText)
			history = append(history, ChatMessage{Role: "assistant", Content: responseText})

			// UI Update with full text
			msg, _ = json.Marshal(map[string]string{"text": responseText, "type": "aura"})
			select {
			case b.UiMessages <- string(msg):
			default:
			}
		}
	}
}
