package ear

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mitsu/pkg/common"
	"os/exec"
	"sync/atomic"
	"time"

	"github.com/maxhawkins/go-webrtcvad"
)

const (
	SampleRate       = 16000
	FrameDurationMs  = 30
	FrameSize        = SampleRate * FrameDurationMs / 1000
	AudioByteSize    = FrameSize * 2
	SilenceLimit     = 40
	VADMode          = 1
	UIMessageTypeMic = "mic"
)

type Ear struct {
	Configuration *EarConfiguration
	Execution     *EarExecution
}

type EarConfiguration struct {
	State *EarState
}

type EarState struct {
	Language *common.LanguageState
	Device   *EarDevice
}

type EarDevice struct {
	InputName common.DeviceName
	TestInput string
}

type EarExecution struct {
	Pipeline *EarPipeline
}

type EarPipeline struct {
	Data   *PipelineData
	Status *PipelineStatus
}

type PipelineData struct {
	SpeechToBrain common.SpeechChannel
	UiMessages    chan string
}

type PipelineStatus struct {
	IsSilenced atomic.Bool
}

type audioSession struct {
	isSpeaking     bool
	silenceCounter int
	buffer         []byte
}

func (ear *Ear) Start(applicationContext context.Context) {
	fmt.Printf("Ear Routine started with Multimodal Direct Audio Pipeline\n")

	go ear.listenForSpeakingChanges(applicationContext)

	ear.captureAndProcessAudio(applicationContext)
}

func (ear *Ear) listenForSpeakingChanges(applicationContext context.Context) {
	speakingChannel := ear.Configuration.State.Language.SubscribeToSpeaking()
	defer ear.Configuration.State.Language.UnsubscribeFromSpeaking(speakingChannel)

	ear.speakingEventLoop(applicationContext, speakingChannel)
}

func (ear *Ear) speakingEventLoop(applicationContext context.Context, speakingChannel chan bool) {
	for !ear.handleSpeakingChange(applicationContext, speakingChannel) {
	}
}

func (ear *Ear) handleSpeakingChange(applicationContext context.Context, speakingChannel chan bool) bool {
	select {
	case isSpeaking := <-speakingChannel:
		ear.Execution.Pipeline.Status.IsSilenced.Store(isSpeaking)
		return false
	case <-applicationContext.Done():
		return true
	}
}

func (ear *Ear) captureAndProcessAudio(applicationContext context.Context) {
	voiceActivityDetector := ear.initializeVAD()
	if voiceActivityDetector == nil {
		return
	}

	ear.audioCaptureLoop(applicationContext, voiceActivityDetector)
}

func (ear *Ear) initializeVAD() *webrtcvad.VAD {
	voiceActivityDetector, initializationError := webrtcvad.New()
	if initializationError != nil {
		fmt.Printf("Ear Error: Failed to initialize VAD: %v\n", initializationError)
		return nil
	}
	voiceActivityDetector.SetMode(VADMode)
	return voiceActivityDetector
}

func (ear *Ear) audioCaptureLoop(applicationContext context.Context, voiceActivityDetector *webrtcvad.VAD) {
	for applicationContext.Err() == nil {
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

	ear.processAudioStream(stdout, voiceActivityDetector)
	
	ear.cleanupCaptureCommand(captureCommand)
	ear.delayRestart(applicationContext)
}

func (ear *Ear) processAudioStream(reader io.Reader, voiceActivityDetector *webrtcvad.VAD) {
	audioBuffer := make([]byte, AudioByteSize)
	session := &audioSession{
		buffer: make([]byte, 0, AudioByteSize*SilenceLimit*2),
	}

	for ear.readAndProcessNextFrame(reader, voiceActivityDetector, audioBuffer, session) {
	}
}

func (ear *Ear) readAndProcessNextFrame(reader io.Reader, voiceActivityDetector *webrtcvad.VAD, audioBuffer []byte, session *audioSession) bool {
	_, readError := io.ReadFull(reader, audioBuffer)
	if readError != nil {
		return false
	}
	if ear.Execution.Pipeline.Status.IsSilenced.Load() {
		return true
	}

	isSpeech, _ := voiceActivityDetector.Process(SampleRate, audioBuffer)
	ear.handleAudioFrame(session, audioBuffer, isSpeech)
	return true
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
	session.buffer = append(session.buffer, chunk...)
	session.silenceCounter = 0
}

func (ear *Ear) onSilenceDetected(session *audioSession) {
	session.silenceCounter++
	if session.silenceCounter >= SilenceLimit {
		fmt.Printf("VAD: Sentence finished.\n")
		
		go ear.dispatchAudio(session.buffer)

		session.isSpeaking = false
		session.silenceCounter = 0
		session.buffer = make([]byte, 0, AudioByteSize*SilenceLimit*2)
	}
}

func (ear *Ear) dispatchAudio(pcmData []byte) {
	if len(pcmData) == 0 {
		return
	}

	wavBytes := addWAVHeader(pcmData)
	ear.notifyUI("🎤 [Voice Input]", UIMessageTypeMic)

	ear.Execution.Pipeline.Data.SpeechToBrain <- common.SpeechEntry{
		Details: common.SpeechDetails{
			Text:     "",
			Language: ear.Configuration.State.Language.CurrentLanguage(),
			Audio:    wavBytes,
		},
		Context: common.EntryContext{
			Timestamp: time.Now(),
			Profile:   common.NewProfile(),
		},
	}
}

func addWAVHeader(pcmData []byte) []byte {
	fileSize := 36 + len(pcmData)
	header := make([]byte, 44)
	copy(header[0:4], []byte("RIFF"))
	header[4] = byte(fileSize & 0xff)
	header[5] = byte((fileSize >> 8) & 0xff)
	header[6] = byte((fileSize >> 16) & 0xff)
	header[7] = byte((fileSize >> 24) & 0xff)
	copy(header[8:12], []byte("WAVE"))
	copy(header[12:16], []byte("fmt "))
	header[16] = 16
	header[17] = 0
	header[18] = 0
	header[19] = 0
	header[20] = 1
	header[21] = 0
	header[22] = 1
	header[23] = 0
	header[24] = 0x80
	header[25] = 0x3e
	header[26] = 0
	header[27] = 0
	header[28] = 0x00
	header[29] = 0x7d
	header[30] = 0
	header[31] = 0
	header[32] = 2
	header[33] = 0
	header[34] = 16
	header[35] = 0
	copy(header[36:40], []byte("data"))
	header[40] = byte(len(pcmData) & 0xff)
	header[41] = byte((len(pcmData) >> 8) & 0xff)
	header[42] = byte((len(pcmData) >> 16) & 0xff)
	header[43] = byte((len(pcmData) >> 24) & 0xff)
	return append(header, pcmData...)
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
	if inputDevice != "" {
		arguments[5] = inputDevice
	}
	testInput := ear.Configuration.State.Device.TestInput
	if testInput != "" {
		arguments = []string{"-re", "-i", testInput, "-f", "s16le", "-ar", "16000", "-ac", "1", "pipe:1", "-v", "error"}
	}
	return exec.CommandContext(applicationContext, "ffmpeg", arguments...)
}

func (ear *Ear) handleCaptureError(applicationContext context.Context, captureError error) {
	fmt.Printf("Ear Error: %v\n", captureError)
	select {
	case <-applicationContext.Done():
	case <-time.After(1 * time.Second):
	}
}

func (ear *Ear) cleanupCaptureCommand(command *exec.Cmd) {
	if command.Process != nil {
		command.Process.Kill()
	}
	command.Wait()
}

func (ear *Ear) delayRestart(applicationContext context.Context) {
	select {
	case <-applicationContext.Done():
	case <-time.After(100 * time.Millisecond):
	}
}

func (ear *Ear) notifyUI(text, messageType string) {
	message, _ := json.Marshal(map[string]string{"text": text, "type": messageType})
	select {
	case ear.Execution.Pipeline.Data.UiMessages <- string(message):
	default:
	}
}
