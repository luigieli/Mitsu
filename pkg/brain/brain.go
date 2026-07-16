package brain

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"mitsu/pkg/common"
	"mitsu/pkg/mcp"
	"net/http"
	"strings"
	"time"
)

const (
	ModelName              = "mitsu"
	ContextExpiration      = 5 * time.Second
	HistoryLimit           = 10
	RoleUser               = "user"
	RoleAssistant          = "assistant"
	OllamaAPIStatus        = "/api/ps"
	OllamaAPIChat          = "/api/chat"
	UIMessageTypeStatus    = "status"
	UIMessageTypeAura      = "aura"
	ThinkingStatusMessage  = "THINKING..."
)

// History is a first-class collection of chat messages.
type History []ChatMessage

// ChatHistory wraps the History collection.
type ChatHistory struct {
	Messages History
}

// Brain is the main orchestrator for language processing.
type Brain struct {
	Configuration *BrainConfiguration
	Execution     *BrainExecution
}

// BrainConfiguration holds the static and stateful configuration for the brain.
type BrainConfiguration struct {
	Connectivity *BrainConnectivity
	State        *BrainState
}

// BrainConnectivity manages external service connections.
type BrainConnectivity struct {
	OllamaURL common.URL
	MCP       *mcp.Manager
}

// BrainState manages the internal state of the brain.
type BrainState struct {
	Language *common.LanguageState
	Memory   *BrainMemory
}

// BrainMemory manages the chat history and memory control.
type BrainMemory struct {
	History         *ChatHistory
	ClearChannel    chan struct{}
}

// BrainExecution handles the runtime data flow.
type BrainExecution struct {
	Data *BrainData
	UI   *BrainUI
}

// BrainData holds the communication channels.
type BrainData struct {
	SpeechToBrain common.SpeechChannel
	BrainToMouth  common.LLMChannel
}

// BrainUI handles UI notifications.
type BrainUI struct {
	UiMessages chan string
}

type ChatMessage struct {
	Role   string   `json:"role"`
	Content string   `json:"content"`
	Images  []string `json:"images,omitempty"`
}

type OllamaChatRequest struct {
	Model    string        `json:"model"`
	Messages []ChatMessage `json:"messages"`
	Format   string        `json:"format,omitempty"`
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

func SplitSentences(text string) []string {
	if text == "" {
		return nil
	}
	return findSentences(text)
}

func findSentences(text string) []string {
	var sentences []string
	var startPosition int
	for index := 0; index < len(text); index++ {
		startPosition = processChar(text, index, startPosition, &sentences)
	}
	return finalizeSentences(text, startPosition, sentences)
}

func processChar(text string, index int, startPosition int, sentences *[]string) int {
	if !isEndChar(text[index]) {
		return startPosition
	}
	return trySplitAt(text, index, startPosition, sentences)
}

func isEndChar(character byte) bool {
	return character == '.' || character == '?' || character == '!'
}

func trySplitAt(text string, index int, startPosition int, sentences *[]string) int {
	if isAbbr(text, startPosition, index) {
		return startPosition
	}
	if !isAtBoundary(text, index) {
		return startPosition
	}
	*sentences = append(*sentences, strings.TrimSpace(text[startPosition:index+1]))
	return index + 1
}

func isAbbr(text string, startPosition, index int) bool {
	return index >= 2 && isCommonAbbreviation(text[startPosition:index])
}

func isAtBoundary(text string, index int) bool {
	if index+1 == len(text) {
		return true
	}
	return text[index+1] == ' '
}

func finalizeSentences(text string, startPosition int, sentences []string) []string {
	if startPosition < len(text) {
		sentences = appendRemainder(text, startPosition, sentences)
	}
	if len(sentences) == 0 {
		return []string{text}
	}
	return sentences
}

func appendRemainder(text string, startPosition int, sentences []string) []string {
	remainder := strings.TrimSpace(text[startPosition:])
	if remainder != "" {
		return append(sentences, remainder)
	}
	return sentences
}

func isCommonAbbreviation(segment string) bool {
	segment = strings.ToLower(strings.TrimSpace(segment))
	abbreviations := []string{"mr", "ms", "mrs", "dr", "prof", "st"}
	for _, abbreviation := range abbreviations {
		if strings.HasSuffix(segment, abbreviation) {
			return true
		}
	}
	return false
}

func (brain *Brain) WarmUp(applicationContext context.Context, onLoading func()) error {
	fmt.Printf("Brain: Checking model status...\n")

	brain.checkMCPStatus(applicationContext)

	modelsToLoad := []string{ModelName}
	if !brain.isAnyModelMissing(applicationContext, modelsToLoad) {
		fmt.Println("Brain: Model already resident in GPU.")
		return nil
	}

	brain.runOnLoading(onLoading)
	brain.preLoadModels(applicationContext, modelsToLoad)

	return nil
}

func (brain *Brain) checkMCPStatus(applicationContext context.Context) {
	mcpManager := brain.Configuration.Connectivity.MCP
	if mcpManager == nil {
		return
	}
	tools, mcpError := mcpManager.ListAllTools(applicationContext)
	if mcpError != nil {
		fmt.Printf("Brain: MCP Manager initialization check failed: %v\n", mcpError)
		return
	}
	fmt.Printf("Brain: MCP Manager active with %d tools available.\n", len(tools))
}

func (brain *Brain) isAnyModelMissing(applicationContext context.Context, modelsToLoad []string) bool {
	ollamaURL := brain.Configuration.Connectivity.OllamaURL
	request, _ := http.NewRequestWithContext(applicationContext, "GET", string(ollamaURL)+OllamaAPIStatus, nil)
	response, ollamaError := http.DefaultClient.Do(request)
	if ollamaError != nil {
		return true
	}
	defer response.Body.Close()

	var processStatus OllamaPsResponse
	if decodeError := json.NewDecoder(response.Body).Decode(&processStatus); decodeError != nil {
		return true
	}

	for _, modelName := range modelsToLoad {
		if !brain.isModelResident(processStatus.Models, modelName) {
			return true
		}
	}
	return false
}

func (brain *Brain) isModelResident(residentModels []struct {
	Name string `json:"name"`
}, modelName string) bool {
	for _, model := range residentModels {
		if strings.HasPrefix(model.Name, modelName) {
			return true
		}
	}
	return false
}

func (brain *Brain) runOnLoading(onLoading func()) {
	if onLoading != nil {
		onLoading()
	}
}

func (brain *Brain) preLoadModels(applicationContext context.Context, modelsToLoad []string) {
	for _, model := range modelsToLoad {
		brain.preLoadModel(applicationContext, model)
	}
}

func (brain *Brain) preLoadModel(applicationContext context.Context, model string) {
	fmt.Printf("Brain: Pre-loading model %s into GPU...\n", model)
	requestBody, _ := json.Marshal(OllamaChatRequest{
		Model:    model,
		Messages: []ChatMessage{{Role: RoleUser, Content: "hi"}},
		Stream:   false,
	})

	ollamaURL := brain.Configuration.Connectivity.OllamaURL
	request, _ := http.NewRequestWithContext(applicationContext, "POST", string(ollamaURL)+OllamaAPIChat, bytes.NewBuffer(requestBody))
	request.Header.Set("Content-Type", "application/json")
	response, ollamaError := http.DefaultClient.Do(request)
	if ollamaError != nil {
		fmt.Printf("Warning: Failed to load %s: %v\n", model, ollamaError)
		return
	}
	defer response.Body.Close()
	fmt.Printf("Brain: Model %s pre-loaded.\n", model)
}

func (brain *Brain) Start(applicationContext context.Context) {
	fmt.Println("Brain Routine started with Streaming Overlap.")
	brain.Configuration.State.Memory.History.Messages = History{}

	for {
		if brain.handleNextEvent(applicationContext) {
			return
		}
	}
}

func (brain *Brain) handleNextEvent(applicationContext context.Context) bool {
	select {
	case <-applicationContext.Done():
		fmt.Println("Brain: Shutting down.")
		return true
	case <-brain.Configuration.State.Memory.ClearChannel:
		brain.clearHistory()
		return false
	case entry := <-brain.Execution.Data.SpeechToBrain:
		brain.processSpeechEntry(applicationContext, entry)
		return false
	}
}

func (brain *Brain) clearHistory() {
	fmt.Println("Brain: Memory cleared.")
	brain.Configuration.State.Memory.History.Messages = History{}
}

func (brain *Brain) processSpeechEntry(applicationContext context.Context, entry common.SpeechEntry) {
	if time.Since(entry.Context.Timestamp) > ContextExpiration {
		fmt.Println("Brain: Skipping expired context")
		return
	}
	brain.processRequest(applicationContext, entry)
}

func (brain *Brain) logProcessing(entry common.SpeechEntry) {
	if len(entry.Details.Audio) > 0 {
		fmt.Println("Brain processing direct audio input")
		return
	}
	fmt.Printf("Brain processing: \"%s\"\n", entry.Details.Text)
}

func (brain *Brain) processRequest(applicationContext context.Context, entry common.SpeechEntry) {
	brain.logProcessing(entry)
	brain.notifyUI(ThinkingStatusMessage, UIMessageTypeStatus)

	// Silence the Ear immediately during thinking phase
	brain.Configuration.State.Language.CoordinateSpeaking(true)

	userMsg := ChatMessage{
		Role:    RoleUser,
		Content: string(entry.Details.Text),
	}
	if len(entry.Details.Audio) > 0 {
		userMsg = ChatMessage{
			Role:    RoleUser,
			Content: "🎤 [Voice Input]",
		}
	}
	
	brain.updateHistory(userMsg)

	messages := brain.buildActiveMessages(entry)

	modelName := brain.selectModel(entry)
	requestBody, _ := json.Marshal(OllamaChatRequest{
		Model:    modelName,
		Messages: messages,
		Stream:   true,
	})

	ollamaURL := brain.Configuration.Connectivity.OllamaURL
	request, _ := http.NewRequestWithContext(applicationContext, "POST", string(ollamaURL)+OllamaAPIChat, bytes.NewBuffer(requestBody))
	request.Header.Set("Content-Type", "application/json")

	brainStart := time.Now()
	response, ollamaError := http.DefaultClient.Do(request)
	if ollamaError != nil {
		fmt.Printf("Brain Error: %v\n", ollamaError)
		brain.Configuration.State.Language.CoordinateSpeaking(false)
		return
	}
	defer response.Body.Close()

	brain.streamResponse(response, entry, brainStart)
}

func (brain *Brain) buildActiveMessages(entry common.SpeechEntry) []ChatMessage {
	messages := make([]ChatMessage, len(brain.Configuration.State.Memory.History.Messages))
	copy(messages, brain.Configuration.State.Memory.History.Messages)

	if len(entry.Details.Audio) > 0 {
		base64Audio := base64.StdEncoding.EncodeToString(entry.Details.Audio)
		prompt := "Listen and respond directly to this speech as Mitsu."
		if entry.Details.Language == common.LanguagePortuguese {
			prompt = "Escute e responda diretamente a esta fala como Mitsu."
		}
		messages[len(messages)-1] = ChatMessage{
			Role:    RoleUser,
			Content: prompt,
			Images:  []string{base64Audio},
		}
	}
	return messages
}

func (brain *Brain) selectModel(entry common.SpeechEntry) string {
	return ModelName
}

func (brain *Brain) updateHistory(message ChatMessage) {
	history := brain.Configuration.State.Memory.History
	history.Messages = append(history.Messages, message)
	if len(history.Messages) > HistoryLimit {
		history.Messages = history.Messages[len(history.Messages)-HistoryLimit:]
	}
}

func (brain *Brain) streamResponse(response *http.Response, entry common.SpeechEntry, brainStart time.Time) {
	if response.StatusCode != http.StatusOK {
		fmt.Printf("Brain Error: Ollama returned status %d\n", response.StatusCode)
		brain.Configuration.State.Language.CoordinateSpeaking(false)
		return
	}

	sequenceID := 0
	rawTokens := brain.consumeOllamaStream(response, entry, brainStart)
	aggregator := &sentenceAggregator{
		onSentence: func(sentence string) {
			brain.dispatchSentence(sentence, entry, false, sequenceID)
			sequenceID++
		},
	}

	var fullResponse strings.Builder
	for token := range rawTokens {
		fullResponse.WriteString(token)
		aggregator.Add(token)
	}

	finalText := aggregator.FlushRemaining()
	brain.dispatchSentence(finalText, entry, true, sequenceID)

	brain.finalizeResponse(fullResponse.String())
}

func (brain *Brain) finalizeResponse(fullResponse string) {
	responseText := strings.TrimSpace(fullResponse)
	fmt.Printf("Mitsu: \"%s\"\n", responseText)
	brain.updateHistory(ChatMessage{Role: RoleAssistant, Content: responseText})
	brain.notifyUI(responseText, UIMessageTypeAura)
}

type sentenceAggregator struct {
	buffer     strings.Builder
	onSentence func(string)
}

func (aggregator *sentenceAggregator) Add(token string) {
	aggregator.buffer.WriteString(token)
	
	if !strings.ContainsAny(token, ".?!") {
		return
	}

	currentText := aggregator.buffer.String()
	if len(currentText) < 10 {
		return
	}

	sentences := SplitSentences(currentText)
	if len(sentences) <= 1 {
		return
	}

	for _, sentence := range sentences[:len(sentences)-1] {
		aggregator.onSentence(sentence)
	}
	
	aggregator.buffer.Reset()
	aggregator.buffer.WriteString(sentences[len(sentences)-1])
}

func (aggregator *sentenceAggregator) FlushRemaining() string {
	return strings.TrimSpace(aggregator.buffer.String())
}

func (brain *Brain) consumeOllamaStream(response *http.Response, entry common.SpeechEntry, brainStart time.Time) <-chan string {
	tokenChannel := make(chan string)
	if response == nil || response.Body == nil {
		close(tokenChannel)
		return tokenChannel
	}

	go func() {
		defer close(tokenChannel)
		defer response.Body.Close()
		reader := bufio.NewReader(response.Body)
		firstTokenReceived := false

		for {
			line, readError := reader.ReadBytes('\n')
			if readError != nil { break }

			var ollamaResponse OllamaChatResponse
			if unmarshalError := json.Unmarshal(line, &ollamaResponse); unmarshalError != nil { continue }

			if !firstTokenReceived {
				entry.Context.Profile.AddSpan("Brain_TTFW", time.Since(brainStart))
				firstTokenReceived = true
			}

			tokenChannel <- ollamaResponse.Message.Content
			if ollamaResponse.Done { break }
		}
	}()
	return tokenChannel
}

func (brain *Brain) dispatchSentence(text string, entry common.SpeechEntry, done bool, sequenceID int) {
	text = strings.TrimSpace(text)
	text = strings.ReplaceAll(text, "</response>", "")
	if text == "" && !done { return }
	
	brain.Execution.Data.BrainToMouth <- common.LLMEntry{
		Chunk: common.LLMChunk{
			Details: common.LLMDetails{
				Text:          common.LLMResponseContent(text),
				InputLanguage: entry.Details.Language,
				SequenceID:    sequenceID,
			},
			Done: done,
		},
		Context: entry.Context,
	}
}

func (brain *Brain) notifyUI(text, messageType string) {
	message, _ := json.Marshal(map[string]string{"text": text, "type": messageType})
	select {
	case brain.Execution.UI.UiMessages <- string(message):
	default:
	}
}


