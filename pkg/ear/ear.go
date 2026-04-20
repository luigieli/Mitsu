package ear

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mitsu/pkg/common"
	"net/http"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
	"github.com/maxhawkins/go-webrtcvad"
)

const (
	SampleRate            = 16000
	FrameDurationMs       = 30
	FrameSize             = SampleRate * FrameDurationMs / 1000
	AudioByteSize         = FrameSize * 2
	SilenceLimit          = 40
	StreamingRetryDelay   = 5 * time.Second
	CaptureRetryDelay     = 1 * time.Second
	RestartDelay          = 100 * time.Millisecond
	VADMode               = 1
	SpeechToTextFlushMessage = "FLUSH"
	UIMessageTypeMic      = "mic"
	MitsuCorrectedName    = "Mitsu"
)

// SynchronizedWebSocket provides thread-safe access to a websocket connection.
type SynchronizedWebSocket struct {
	mutex      sync.Mutex
	connection *websocket.Conn
}

// Ear is the main orchestrator for audio capture and transcription.
type Ear struct {
	Configuration *EarConfiguration
	Execution     *EarExecution
}

// EarConfiguration holds the static and stateful configuration for the ear.
type EarConfiguration struct {
	Connectivity *EarConnectivity
	State        *EarState
}

// EarConnectivity manages external service connections.
type EarConnectivity struct {
	SpeechToTextURL          common.URL
	SpeechToTextStreamingURL common.URL
}

// EarState manages the internal state of the ear.
type EarState struct {
	Language *common.LanguageState
	Device   *EarDevice
}

// EarDevice manages audio device settings.
type EarDevice struct {
	InputName common.DeviceName
	TestInput string
}

// EarExecution handles the runtime data flow and processing.
type EarExecution struct {
	Pipeline  *EarPipeline
	Streaming *EarStreaming
}

// EarPipeline holds the data channels and VAD state.
type EarPipeline struct {
	SpeechToBrain common.SpeechChannel
	UiMessages    chan string
	IsSilenced    atomic.Bool
}

// EarStreaming manages the streaming transcription state.
type EarStreaming struct {
	WebSocket       *SynchronizedWebSocket
	CurrentLanguage atomic.Value // Language
}

func (ear *Ear) Start(applicationContext context.Context) {
	fmt.Printf("Ear Routine started with Hybrid Go-VAD Pipeline\n")

	go ear.listenForLanguageChanges(applicationContext)
	go ear.listenForSpeakingChanges(applicationContext)

	if ear.Configuration.Connectivity.SpeechToTextStreamingURL != "" {
		go ear.streamingTranscriptionLoop(applicationContext)
	}

	ear.captureAndProcessAudio(applicationContext)
}

func (ear *Ear) listenForLanguageChanges(applicationContext context.Context) {
	languageChannel := ear.Configuration.State.Language.SubscribeToLanguage()
	defer ear.Configuration.State.Language.UnsubscribeFromLanguage(languageChannel)
	
	for {
		if ear.handleLanguageChange(applicationContext, languageChannel) {
			return
		}
	}
}

func (ear *Ear) handleLanguageChange(applicationContext context.Context, languageChannel chan common.Language) bool {
	select {
	case newLanguage := <-languageChannel:
		ear.Execution.Streaming.CurrentLanguage.Store(newLanguage)
		ear.notifySpeechToTextServer(newLanguage)
		return false
	case <-applicationContext.Done():
		return true
	}
}

func (ear *Ear) notifySpeechToTextServer(language common.Language) {
	fmt.Printf("Ear: Notifying SpeechToText server of swap to %s\n", language)
	url := string(ear.Configuration.Connectivity.SpeechToTextURL) + "/swap/" + string(language)
	response, requestError := http.Post(url, "application/json", nil)
	if requestError == nil {
		response.Body.Close()
	}
}

func (ear *Ear) listenForSpeakingChanges(applicationContext context.Context) {
	speakingChannel := ear.Configuration.State.Language.SubscribeToSpeaking()
	defer ear.Configuration.State.Language.UnsubscribeFromSpeaking(speakingChannel)

	for {
		if ear.handleSpeakingChange(applicationContext, speakingChannel) {
			return
		}
	}
}

func (ear *Ear) handleSpeakingChange(applicationContext context.Context, speakingChannel chan bool) bool {
	select {
	case isSpeaking := <-speakingChannel:
		ear.Execution.Pipeline.IsSilenced.Store(isSpeaking)
		return false
	case <-applicationContext.Done():
		return true
	}
}

func (ear *Ear) streamingTranscriptionLoop(applicationContext context.Context) {
	for {
		if ear.runStreamingIteration(applicationContext) {
			return
		}
	}
}

func (ear *Ear) runStreamingIteration(applicationContext context.Context) bool {
	if applicationContext.Err() != nil {
		return true
	}

	connection := ear.connectToStreamingService(applicationContext)
	if connection == nil {
		return false
	}

	ear.manageStreamingSession(applicationContext, connection)
	return false
}

func (ear *Ear) connectToStreamingService(applicationContext context.Context) *websocket.Conn {
	url := string(ear.Configuration.Connectivity.SpeechToTextStreamingURL)
	connection, _, connectionError := websocket.DefaultDialer.Dial(url, nil)
	if connectionError == nil {
		fmt.Println("✅ Ear: Connected to Sherpa-ONNX Streaming")
		return connection
	}

	fmt.Printf("Ear: Failed to connect to streaming SpeechToText: %v. Retrying in %v...\n", connectionError, StreamingRetryDelay)
	select {
	case <-applicationContext.Done():
	case <-time.After(StreamingRetryDelay):
	}
	return nil
}

func (ear *Ear) manageStreamingSession(applicationContext context.Context, connection *websocket.Conn) {
	ear.Execution.Streaming.WebSocket.mutex.Lock()
	ear.Execution.Streaming.WebSocket.connection = connection
	ear.Execution.Streaming.WebSocket.mutex.Unlock()

	doneSignal := make(chan struct{})
	go ear.handleConnectionClosure(applicationContext, doneSignal)

	ear.readFromWebSocket(applicationContext)
	close(doneSignal)
	
	ear.closeWebSocket()
}

func (ear *Ear) handleConnectionClosure(applicationContext context.Context, doneSignal chan struct{}) {
	select {
	case <-applicationContext.Done():
		ear.closeWebSocket()
	case <-doneSignal:
	}
}

func (ear *Ear) closeWebSocket() {
	ear.Execution.Streaming.WebSocket.mutex.Lock()
	defer ear.Execution.Streaming.WebSocket.mutex.Unlock()
	if ear.Execution.Streaming.WebSocket.connection != nil {
		ear.Execution.Streaming.WebSocket.connection.Close()
		ear.Execution.Streaming.WebSocket.connection = nil
	}
}

func (ear *Ear) readFromWebSocket(applicationContext context.Context) {
	for {
		if ear.processNextWebSocketMessage(applicationContext) {
			return
		}
	}
}

func (ear *Ear) processNextWebSocketMessage(applicationContext context.Context) bool {
	ear.Execution.Streaming.WebSocket.mutex.Lock()
	connection := ear.Execution.Streaming.WebSocket.connection
	ear.Execution.Streaming.WebSocket.mutex.Unlock()
	if connection == nil { return true }

	_, messageBytes, readError := connection.ReadMessage()
	if readError != nil { return true }
	
	ear.handleMessageData(messageBytes)
	
	return applicationContext.Err() != nil
}

func (ear *Ear) handleMessageData(data []byte) {
	var transcriptionResult struct {
		Text    string `json:"text"`
		IsFinal bool   `json:"is_final"`
	}
	if unmarshalError := json.Unmarshal(data, &transcriptionResult); unmarshalError == nil && transcriptionResult.Text != "" {
		ear.handleTranscriptionResult(transcriptionResult.Text, transcriptionResult.IsFinal)
	}
}

func (ear *Ear) handleTranscriptionResult(text string, isFinal bool) {
	if isFinal {
		fmt.Printf("SpeechToText (Final): %s\n", text)
		ear.dispatchTranscription(text, common.NewProfile())
		return
	}
	ear.notifyUI(text, UIMessageTypeMic)
}

func (ear *Ear) captureAndProcessAudio(applicationContext context.Context) {
	voiceActivityDetector, initializationError := webrtcvad.New()
	if initializationError != nil {
		fmt.Printf("Ear Error: Failed to initialize VAD: %v\n", initializationError)
		return
	}
	voiceActivityDetector.SetMode(VADMode)

	for {
		if applicationContext.Err() != nil { return }
		ear.runCaptureIteration(applicationContext, voiceActivityDetector)
	}
}

func (ear *Ear) runCaptureIteration(applicationContext context.Context, voiceActivityDetector *webrtcvad.VAD) {
	captureCommand := ear.startCaptureCommand(applicationContext)
	stdout, pipeError := captureCommand.StdoutPipe()
	if pipeError != nil {
		ear.handleCaptureError(applicationContext, pipeError)
		return
	}
	
	if startError := captureCommand.Start(); startError != nil {
		ear.handleCaptureError(applicationContext, startError)
		return
	}

	ear.processAudioStream(stdout, voiceActivityDetector, AudioByteSize, SilenceLimit)
	
	ear.cleanupCaptureCommand(captureCommand)
	
	select {
	case <-applicationContext.Done():
	case <-time.After(RestartDelay):
	}
}

func (ear *Ear) handleCaptureError(applicationContext context.Context, captureError error) {
	fmt.Printf("Ear Error: %v\n", captureError)
	select {
	case <-applicationContext.Done():
	case <-time.After(CaptureRetryDelay):
	}
}

func (ear *Ear) cleanupCaptureCommand(command *exec.Cmd) {
	if command.Process != nil {
		command.Process.Kill()
	}
	command.Wait()
}

func (ear *Ear) startCaptureCommand(applicationContext context.Context) *exec.Cmd {
	arguments := []string{
		"-f", "pulse", "-name", "Mitsu_Ear", "-i", "default",
		"-ar", "16000", "-ac", "1",
		"-af", "highpass=f=80",
		"-f", "s16le", "pipe:1",
		"-v", "error",
	}
	inputDevice := string(ear.Configuration.State.Device.InputName)
	if inputDevice != "" { arguments[5] = inputDevice }
	testInput := ear.Configuration.State.Device.TestInput
	if testInput != "" {
		arguments = []string{"-re", "-i", testInput, "-f", "s16le", "-ar", "16000", "-ac", "1", "pipe:1", "-v", "error"}
	}
	return exec.CommandContext(applicationContext, "ffmpeg", arguments...)
}

func (ear *Ear) processAudioStream(reader io.Reader, voiceActivityDetector *webrtcvad.VAD, byteSize, silenceLimit int) {
	if reader == nil || voiceActivityDetector == nil {
		fmt.Println("Ear Error: Invalid capture state (nil stream or VAD).")
		return
	}

	audioBuffer := make([]byte, byteSize)
	currentSession := &audioSession{silenceLimit: silenceLimit}

	for {
		if _, readError := io.ReadFull(reader, audioBuffer); readError != nil { break }
		if ear.Execution.Pipeline.IsSilenced.Load() { continue }

		isSpeech, _ := voiceActivityDetector.Process(SampleRate, audioBuffer)
		ear.handleAudioFrame(currentSession, audioBuffer, isSpeech)
	}
}

type audioSession struct {
	isSpeaking     bool
	silenceCounter int
	silenceLimit   int
}

func (ear *Ear) handleAudioFrame(session *audioSession, chunk []byte, isSpeech bool) {
	if isSpeech {
		ear.onSpeechDetected(session, chunk)
		return
	}

	if session.isSpeaking {
		ear.onSilenceDetected(session)
	}
}

func (ear *Ear) onSpeechDetected(session *audioSession, chunk []byte) {
	if !session.isSpeaking {
		session.isSpeaking = true
		fmt.Println("VAD: Speech started.")
	}
	ear.sendToWebSocket(chunk, false)
	session.silenceCounter = 0
}

func (ear *Ear) onSilenceDetected(session *audioSession) {
	session.silenceCounter++
	if session.silenceCounter >= session.silenceLimit {
		fmt.Printf("VAD: Sentence finished.\n")
		ear.sendToWebSocket(nil, true)
		session.isSpeaking = false
		session.silenceCounter = 0
	}
}

func (ear *Ear) sendToWebSocket(data []byte, flush bool) {
	ear.Execution.Streaming.WebSocket.mutex.Lock()
	defer ear.Execution.Streaming.WebSocket.mutex.Unlock()
	connection := ear.Execution.Streaming.WebSocket.connection
	if connection == nil {
		return
	}

	if flush {
		connection.WriteMessage(websocket.TextMessage, []byte(SpeechToTextFlushMessage))
		return
	}
	connection.WriteMessage(websocket.BinaryMessage, data)
}

func (ear *Ear) dispatchTranscription(text string, profile *common.Profile) {
	text = ear.ApplyFuzzyNameCorrection(text)
	language := ear.Execution.Streaming.CurrentLanguage.Load().(common.Language)

	fmt.Printf("Captured (%s): %s\n", language, text)
	ear.notifyUI(text, UIMessageTypeMic)
	ear.Execution.Pipeline.SpeechToBrain <- common.SpeechEntry{
		Details: common.SpeechDetails{
			Text:     common.Transcription(text),
			Language: language,
		},
		Context: common.EntryContext{
			Timestamp: time.Now(),
			Profile:   profile,
		},
	}
}

func (ear *Ear) notifyUI(text, messageType string) {
	message, _ := json.Marshal(map[string]string{"text": text, "type": messageType})
	select {
	case ear.Execution.Pipeline.UiMessages <- string(message):
	default:
	}
}

func (ear *Ear) ApplyFuzzyNameCorrection(text string) string {
	mitsuAliases := map[string]bool{
		"mitzo": true, "mitso": true, "metso": true, "metsu": true, "mitsu": true, "mitzu": true,
	}
	
	words := strings.Fields(text)
	for index, word := range words {
		cleanWord := strings.ToLower(stripPunctuation(word))
		if mitsuAliases[cleanWord] {
			words[index] = "Mitsu" + word[len(stripTrailingPunctuation(word)):]
		}
	}
	return strings.Join(words, " ")
}

func stripPunctuation(segment string) string {
	return strings.Map(func(character rune) rune {
		if strings.ContainsRune(",.?!\"'()", character) {
			return -1
		}
		return character
	}, segment)
}

func stripTrailingPunctuation(segment string) string {
    return strings.TrimRight(segment, ",.?!\"'()")
}
