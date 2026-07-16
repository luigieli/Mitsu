package brain

import (
	"context"
	"fmt"
	"mitsu/pkg/common"
	"os"
	"testing"
	"time"
)

func TestJSONStreamingAudio(t *testing.T) {
	// Only run if the audio file exists
	if _, err := os.Stat("../../test_prompt.wav"); os.IsNotExist(err) {
		t.Skip("Skipping TestJSONStreamingAudio: test_prompt.wav not found")
	}

	audioBytes, err := os.ReadFile("../../test_prompt.wav")
	if err != nil {
		t.Fatalf("Failed to read audio file: %v", err)
	}

	// Initialize Brain pointing to Ollama service in the compose network
	b := &Brain{
		Configuration: &BrainConfiguration{
			Connectivity: &BrainConnectivity{
				OllamaURL: common.URL("http://ollama:11434"),
			},
			State: &BrainState{
				Language: common.NewLanguageState(common.LanguageEnglish),
				Memory: &BrainMemory{
					History: &ChatHistory{},
				},
			},
		},
		Execution: &BrainExecution{
			Data: &BrainData{
				SpeechToBrain: make(chan common.SpeechEntry),
				BrainToMouth:  make(chan common.LLMEntry, 100),
			},
			UI: &BrainUI{
				UiMessages: make(chan string, 100),
			},
		},
	}

	// Add dummy message to satisfy history indexing
	b.updateHistory(ChatMessage{
		Role:    RoleUser,
		Content: "Hello",
	})

	// Add the voice input placeholder that will be replaced
	b.updateHistory(ChatMessage{
		Role:    RoleUser,
		Content: "🎤 [Voice Input]",
	})

	entry := common.SpeechEntry{
		Details: common.SpeechDetails{
			Text:     "",
			Language: common.LanguageEnglish,
			Audio:    audioBytes,
		},
		Context: common.EntryContext{
			Timestamp: time.Now(),
			Profile:   common.NewProfile(),
		},
	}

	fmt.Println("TEST: Starting direct audio processRequest...")
	
	// Collect output chunks in a goroutine
	doneChan := make(chan bool)
	var outputChunks []string
	go func() {
		for chunk := range b.Execution.Data.BrainToMouth {
			fmt.Printf("TEST: Mouth Received chunk: Done=%v, Text=%q\n", chunk.Chunk.Done, chunk.Chunk.Details.Text)
			if chunk.Chunk.Details.Text != "" {
				outputChunks = append(outputChunks, string(chunk.Chunk.Details.Text))
			}
			if chunk.Chunk.Done {
				close(doneChan)
				return
			}
		}
	}()

	b.processRequest(context.Background(), entry)

	// Wait for Mouth to receive the done chunk with timeout
	select {
	case <-doneChan:
		fmt.Println("TEST: Response completed successfully!")
	case <-time.After(15 * time.Second):
		t.Fatal("TEST TIMEOUT: Mouth did not receive the final done chunk")
	}

	// Verify we got some output
	if len(outputChunks) == 0 {
		t.Error("TEST FAILED: No output response chunks received")
	}

	// Print final history to verify user speech was updated in short term memory
	history := b.Configuration.State.Memory.History.Messages
	fmt.Printf("TEST: Final History Length: %d\n", len(history))
	for i, msg := range history {
		fmt.Printf("  [%d] %s: %s\n", i, msg.Role, msg.Content)
	}

	// Verify the history has the correct message structures
	if len(history) < 3 {
		t.Fatalf("TEST FAILED: History has less than 3 messages: %d", len(history))
	}
	lastUserMsg := history[len(history)-2]
	if lastUserMsg.Content != "🎤 [Voice Input]" {
		t.Errorf("TEST FAILED: Expected placeholder user message to remain '🎤 [Voice Input]', got: %q", lastUserMsg.Content)
	}
}
