package brain

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"mitsu/pkg/common"
	"net/http"
	"strings"
	"time"
)

type Brain struct {
	OllamaURL       string
	CurrentLang     string
	SpeechToBrain   common.SpeechText
	BrainToMouth    common.LLMResponse
	UiMessages      chan string
	ClearMemoryChan chan struct{}
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
	Message ChatMessage `json:"message"`
	Done    bool        `json:"done"`
}

func (b *Brain) Start() {
	fmt.Println("Brain Routine started.")
	history := []ChatMessage{}

	for {
		select {
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

			history = append(history, ChatMessage{Role: "user", Content: entry.Text})
			if len(history) > 10 {
				history = history[len(history)-10:]
			}

			modelName := "mitsu-en"
			if b.CurrentLang == "pt" {
				modelName = "mitsu-pt"
			}

			reqBody, _ := json.Marshal(OllamaChatRequest{
				Model:    modelName,
				Messages: history,
				Stream:   false,
			})

			resp, err := http.Post(b.OllamaURL+"/api/chat", "application/json", bytes.NewBuffer(reqBody))
			if err != nil {
				continue
			}
			body, _ := io.ReadAll(resp.Body)
			resp.Body.Close()

			var ollamaResp OllamaChatResponse
			if err := json.Unmarshal(body, &ollamaResp); err != nil {
				continue
			}

			responseText := strings.TrimSpace(ollamaResp.Message.Content)
			fmt.Printf("Mitsu: \"%s\"\n", responseText)
			history = append(history, ChatMessage{Role: "assistant", Content: responseText})

			msg, _ = json.Marshal(map[string]string{"text": responseText, "type": "aura"})
			select {
			case b.UiMessages <- string(msg):
			default:
			}
			b.BrainToMouth <- responseText
		}
	}
}
