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
		Format:   "json",
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
		prompt := "Listen and respond directly to this speech as Mitsu. You MUST format your response exactly in JSON structure: {\"user_speech\": \"[exactly transcribe what you heard the user say]\", \"response\": \"[your sarcastic response here]\"}"
		if entry.Details.Language == common.LanguagePortuguese {
			prompt = "Escute e responda diretamente a esta fala como Mitsu. Você DEVE formatar sua resposta exatamente na estrutura JSON: {\"user_speech\": \"[transcreva exatamente o que ouviu o usuário falar]\", \"response\": \"[sua resposta sarcástica aqui]\"}"
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

	rawTokens := brain.consumeOllamaStream(response, entry, brainStart)
	tokens := brain.parseStream(rawTokens, entry)
	aggregator := &sentenceAggregator{
		onSentence: func(sentence string) { brain.dispatchSentence(sentence, entry, false) },
	}

	var fullResponse strings.Builder
	for token := range tokens {
		fullResponse.WriteString(token)
		aggregator.Add(token)
	}

	finalText := aggregator.FlushRemaining()
	brain.dispatchSentence(finalText, entry, true)

	brain.finalizeResponse(fullResponse.String())
}

func (brain *Brain) finalizeResponse(fullResponse string) {
	responseText := strings.TrimSpace(brain.extractResponseText(fullResponse))
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

func (brain *Brain) dispatchSentence(text string, entry common.SpeechEntry, done bool) {
	text = strings.TrimSpace(text)
	text = strings.ReplaceAll(text, "</response>", "")
	if text == "" && !done { return }
	
	brain.Execution.Data.BrainToMouth <- common.LLMEntry{
		Chunk: common.LLMChunk{
			Details: common.LLMDetails{
				Text:          common.LLMResponseContent(text),
				InputLanguage: entry.Details.Language,
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

func (brain *Brain) extractResponseText(fullOutput string) string {
	var responseMap map[string]interface{}
	if err := json.Unmarshal([]byte(fullOutput), &responseMap); err == nil {
		if resp, ok := responseMap["response"].(string); ok {
			return resp
		}
	}

	responseKey := "\"response\""
	responseIndex := strings.Index(fullOutput, responseKey)
	if responseIndex < 0 {
		return fullOutput
	}
	remainder := fullOutput[responseIndex+len(responseKey):]
	firstQuote := strings.Index(remainder, "\"")
	if firstQuote >= 0 {
		valPart := remainder[firstQuote+1:]
		secondQuote := findNonEscapedQuote(valPart)
		if secondQuote >= 0 {
			return valPart[:secondQuote]
		}
		return valPart
	}
	return fullOutput
}

func (brain *Brain) parseStream(tokens <-chan string, entry common.SpeechEntry) <-chan string {
	parsedChannel := make(chan string)

	go func() {
		defer close(parsedChannel)
		var accumulated strings.Builder
		state := 0 // 0: before user_speech, 1: in user_speech, 2: before response, 3: in response, 4: done
		var userSpeech strings.Builder

		for token := range tokens {
			accumulated.WriteString(token)
			currentText := accumulated.String()

			switch state {
			case 0:
				userSpeechIndex := strings.Index(currentText, "\"user_speech\"")
				if userSpeechIndex >= 0 {
					remainder := currentText[userSpeechIndex+len("\"user_speech\""):]
					firstQuote := strings.Index(remainder, "\"")
					if firstQuote >= 0 {
						state = 1
						accumulated.Reset()
						tokenRemainder := remainder[firstQuote+1:]
						closingIndex := findNonEscapedQuote(tokenRemainder)
						if closingIndex >= 0 {
							userSpeechText := tokenRemainder[:closingIndex]
							userSpeech.WriteString(userSpeechText)
							brain.updateLastUserMessage(userSpeech.String())
							state = 2
							accumulated.WriteString(tokenRemainder[closingIndex+1:])
							continue
						}
						userSpeech.WriteString(tokenRemainder)
					}
				}
			case 1:
				closingQuoteIndex := findNonEscapedQuote(token)
				if closingQuoteIndex >= 0 {
					lastPart := token[:closingQuoteIndex]
					userSpeech.WriteString(lastPart)
					brain.updateLastUserMessage(userSpeech.String())
					state = 2
					accumulated.Reset()
					accumulated.WriteString(token[closingQuoteIndex+1:])
					continue
				}
				userSpeech.WriteString(token)
			case 2:
				responseIndex := strings.Index(currentText, "\"response\"")
				if responseIndex >= 0 {
					remainder := currentText[responseIndex+len("\"response\""):]
					firstQuote := strings.Index(remainder, "\"")
					if firstQuote >= 0 {
						state = 3
						accumulated.Reset()
						tokenRemainder := remainder[firstQuote+1:]
						closingIndex := findNonEscapedQuote(tokenRemainder)
						if closingIndex >= 0 {
							lastPart := tokenRemainder[:closingIndex]
							cleanText := brain.cleanTrailingJson(lastPart)
							if cleanText != "" {
								parsedChannel <- cleanText
							}
							state = 4
							continue
						}
						cleanText := brain.cleanTrailingJson(tokenRemainder)
						if cleanText != "" {
							parsedChannel <- cleanText
						}
					}
				}
			case 3:
				closingQuoteIndex := findNonEscapedQuote(token)
				if closingQuoteIndex >= 0 {
					lastPart := token[:closingQuoteIndex]
					cleanText := brain.cleanTrailingJson(lastPart)
					if cleanText != "" {
						parsedChannel <- cleanText
					}
					state = 4
					accumulated.Reset()
					continue
				}
				cleanText := brain.cleanTrailingJson(token)
				if cleanText != "" {
					parsedChannel <- cleanText
				}
			}
		}
	}()
	return parsedChannel
}

func findNonEscapedQuote(s string) int {
	for i := 0; i < len(s); i++ {
		if s[i] == '"' {
			if !isEscaped(s, i) {
				return i
			}
		}
	}
	return -1
}

func isEscaped(s string, index int) bool {
	escaped := false
	for j := index - 1; j >= 0 && s[j] == '\\'; j-- {
		escaped = !escaped
	}
	return escaped
}

func (brain *Brain) cleanTrailingJson(text string) string {
	text = strings.ReplaceAll(text, "\\\"", "\"")
	text = strings.ReplaceAll(text, "\\n", "\n")
	text = strings.ReplaceAll(text, "\\t", "\t")
	text = strings.ReplaceAll(text, "\"", "")
	text = strings.ReplaceAll(text, "}", "")
	return text
}

func (brain *Brain) updateLastUserMessage(actualText string) {
	history := brain.Configuration.State.Memory.History
	if len(history.Messages) == 0 {
		return
	}
	for index := len(history.Messages) - 1; index >= 0; index-- {
		if history.Messages[index].Role == RoleUser {
			history.Messages[index].Content = actualText
			fmt.Printf("Brain Memory Updated: replaced placeholder with actual speech: \"%s\"\n", actualText)
			return
		}
	}
}
