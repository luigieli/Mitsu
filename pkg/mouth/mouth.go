package mouth

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mitsu/pkg/common"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/abadojack/whatlanggo"
)

// VoiceConfig defines the parameters for voice synthesis and audio filtering.
type VoiceConfig struct {
	VoiceModel       string  `json:"voice_model"`
	LangCode         string  `json:"lang_code"`
	Pitch            float64 `json:"pitch"`
	Speed            float64 `json:"speed"`
	FormantPreserved bool    `json:"formant_preserved"`
	Highpass         int     `json:"highpass"`
	Lowpass          int     `json:"lowpass"`
	BoxyGain         int     `json:"boxy_gain"`
	PresenceGain     int     `json:"presence_gain"`
	SparkleGain      int     `json:"sparkle_gain"`
	ExciterAmount    float64 `json:"exciter_amount"`
	DeesserIntensity float64 `json:"deesser_intensity"`
	StereoWidth      float64 `json:"stereo_width"`
	LoudnormI        int     `json:"loudnorm_i"`
}

const (
	PlaybackSampleRate    = 24000
	DefaultPitch          = 1.25
	DefaultSpeed          = 1.0
	DefaultHighpass       = 150
	DefaultLowpass        = 15000
	DefaultBoxyGain       = -15
	DefaultPresenceGain   = 8
	DefaultSparkleGain    = 0
	DefaultExciterAmount  = 3.0
	DefaultDeesserIntensity = 0.5
	DefaultStereoWidth     = 2.0
	DefaultLoudnormI       = -16
	KokoroVoiceEnglish     = "mitsu_anime_en"
	KokoroVoicePortuguese  = "mitsu_anime_pt"
	KokoroLangCodeEnglish  = "a"
	KokoroLangCodePortuguese = "p"
	KokoroDefaultSpeed     = 1.1
	KokoroModelName        = "kokoro"
)

// Mouth is the main orchestrator for speech synthesis and playback.
type Mouth struct {
	Configuration *MouthConfiguration
	Runtime       *MouthRuntime
}

// MouthConfiguration holds the static and stateful configuration for the mouth.
type MouthConfiguration struct {
	Settings *MouthSettings
	State    *MouthState
}

// MouthSettings manages synthesis parameters.
type MouthSettings struct {
	KokoroURL      common.URL
	KokoroVoiceAmy string
	ActiveConfig   VoiceConfig
}

// MouthState manages the internal state of the mouth.
type MouthState struct {
	Language *common.LanguageState
	Device   *MouthDevice
}

// MouthDevice manages audio output settings.
type MouthDevice struct {
	OutputName common.DeviceName
	TestOutput string
}

// MouthRuntime handles the runtime data flow and playback synchronization.
type MouthRuntime struct {
	Data     *MouthData
	Playback *MouthPlayback
}

// MouthData holds the communication channels.
type MouthData struct {
	BrainToMouth common.LLMChannel
}

// MouthPlayback manages audio playback resources.
type MouthPlayback struct {
	Mutex           sync.Mutex
	CurrentLanguage atomic.Value // Language
}

// LoadVoiceConfig reads the voice configuration from a JSON file.
func LoadVoiceConfig(path string) VoiceConfig {
	defaultConfig := VoiceConfig{
		VoiceModel: "mitsu_custom", LangCode: KokoroLangCodePortuguese, Pitch: DefaultPitch, Speed: DefaultSpeed, FormantPreserved: true,
		Highpass: DefaultHighpass, Lowpass: DefaultLowpass, BoxyGain: DefaultBoxyGain, PresenceGain: DefaultPresenceGain, SparkleGain: DefaultSparkleGain,
		ExciterAmount: DefaultExciterAmount, DeesserIntensity: DefaultDeesserIntensity, StereoWidth: DefaultStereoWidth, LoudnormI: DefaultLoudnormI,
	}

	configFileContent, readError := os.ReadFile(path)
	if readError != nil {
		return defaultConfig
	}
	var config VoiceConfig
	if unmarshalError := json.Unmarshal(configFileContent, &config); unmarshalError != nil {
		return defaultConfig
	}
	return config
}

// BuildFilterChain constructs an FFmpeg filter chain string from the configuration.
func (mouth *Mouth) BuildFilterChain(config VoiceConfig) string {
	pitch := config.Pitch
	if pitch == 0 { pitch = 1.0 }
	speed := config.Speed
	if speed == 0 { speed = 1.0 }
	highpass := config.Highpass
	if highpass == 0 { highpass = 80 }
	lowpass := config.Lowpass
	if lowpass == 0 { lowpass = 15000 }

	return fmt.Sprintf(
		"asetrate=%d*%.2f,atempo=1/%.2f,highpass=f=%d,lowpass=f=%d,equalizer=f=4000:t=h:width=2000:g=4,compand=attacks=0:points=-90/-90|-40/-40|0/-10|20/-10:gain=5",
		PlaybackSampleRate, pitch, pitch*speed, highpass, lowpass,
	)
}

// Say queues a text string for synthesis and playback.
func (mouth *Mouth) Say(text string, language common.Language) {
	mouth.Runtime.Data.BrainToMouth <- common.LLMEntry{
		Chunk: common.LLMChunk{
			Details: common.LLMDetails{
				Text:          common.LLMResponseContent(text),
				InputLanguage: language,
			},
			Done: true,
		},
		Context: common.EntryContext{
			Timestamp: time.Now(),
			Profile:   common.NewProfile(),
		},
	}
}

// Alert is a convenience method for Say with a string language.
func (mouth *Mouth) Alert(text string, language string) {
	mouth.Say(text, common.Language(language))
}

// Start begins the mouth processing loop.
func (mouth *Mouth) Start(applicationContext context.Context) {
	fmt.Println("Mouth Routine started with Parallel Streaming Support.")

	go mouth.listenForLanguageChanges(applicationContext)

	pipeReader, pipeWriter := io.Pipe()
	playbackCommand := mouth.startPlaybackCommand(applicationContext, pipeReader)
	
	if startError := playbackCommand.Start(); startError != nil {
		fmt.Printf("Failed to start persistent playback: %v\n", startError)
		return
	}

	for {
		if mouth.handleNextEntry(applicationContext, pipeWriter, playbackCommand) {
			return
		}
	}
}

func (mouth *Mouth) handleNextEntry(applicationContext context.Context, pipeWriter *io.PipeWriter, playbackCommand *exec.Cmd) bool {
	select {
	case entry := <-mouth.Runtime.Data.BrainToMouth:
		go mouth.processEntry(applicationContext, entry, pipeWriter)
		return false
	case <-applicationContext.Done():
		pipeWriter.Close()
		playbackCommand.Wait()
		return true
	}
}

func (mouth *Mouth) listenForLanguageChanges(applicationContext context.Context) {
	languageChannel := mouth.Configuration.State.Language.SubscribeToLanguage()
	defer mouth.Configuration.State.Language.UnsubscribeFromLanguage(languageChannel)

	for {
		select {
		case newLanguage := <-languageChannel:
			mouth.Runtime.Playback.CurrentLanguage.Store(newLanguage)
		case <-applicationContext.Done():
			return
		}
	}
}

func (mouth *Mouth) startPlaybackCommand(applicationContext context.Context, reader io.Reader) *exec.Cmd {
	if mouth.Configuration.State.Device.TestOutput != "" {
		return mouth.startTestPlaybackCommand(applicationContext, reader)
	}
	return mouth.startLivePlaybackCommand(applicationContext, reader)
}

func (mouth *Mouth) startTestPlaybackCommand(applicationContext context.Context, reader io.Reader) *exec.Cmd {
	testOutput := mouth.Configuration.State.Device.TestOutput
	fmt.Printf("Mouth: Running in test mode, saving output to %s\n", testOutput)
	command := exec.CommandContext(applicationContext, "ffmpeg", "-y", "-f", "s16le", "-ar", "24000", "-ac", "1", "-i", "pipe:0", testOutput)
	command.Stdin = reader
	return command
}

func (mouth *Mouth) startLivePlaybackCommand(applicationContext context.Context, reader io.Reader) *exec.Cmd {
	arguments := []string{"--playback", "--format=s16le", "--channels=1", "--rate=24000", "--property=application.name=Mitsu_Mouth"}
	outputDevice := string(mouth.Configuration.State.Device.OutputName)
	if outputDevice != "" {
		arguments = append(arguments, "-d", outputDevice)
	}
	command := exec.CommandContext(applicationContext, "pacat", arguments...)
	command.Stdin = reader
	return command
}

func (mouth *Mouth) processEntry(applicationContext context.Context, entry common.LLMEntry, pipeWriter io.Writer) {
	if entry.Chunk.Details.Text == "" || pipeWriter == nil {
		if entry.Chunk.Done {
			mouth.Configuration.State.Language.CoordinateSpeaking(false)
		}
		return
	}

	mouth.Configuration.State.Language.CoordinateSpeaking(true)
	defer mouth.finalizeEntry(entry)

	ttsStart := time.Now()
	audioData, fetchError := mouth.fetchAudioFromKokoro(applicationContext, entry)
	if fetchError != nil {
		fmt.Printf("Kokoro Error: %v\n", fetchError)
		return
	}
	defer audioData.Close()
	entry.Context.Profile.AddSpan("TTS_Synthesis", time.Since(ttsStart))

	mouth.playAudio(applicationContext, audioData, entry.Context.Profile, pipeWriter)
}

func (mouth *Mouth) finalizeEntry(entry common.LLMEntry) {
	if entry.Chunk.Done {
		mouth.Configuration.State.Language.CoordinateSpeaking(false)
		fmt.Printf("[PROFILER] %s\n", entry.Context.Profile.Summary())
	}
}

func (mouth *Mouth) fetchAudioFromKokoro(applicationContext context.Context, entry common.LLMEntry) (io.ReadCloser, error) {
	fallbackLanguage := entry.Chunk.Details.InputLanguage
	if fallbackLanguage == "" {
		fallbackLanguage = mouth.getCurrentLanguage()
	}
	language := mouth.detectLanguage(string(entry.Chunk.Details.Text), fallbackLanguage)

	voice := KokoroVoiceEnglish
	langCode := KokoroLangCodeEnglish
	if language == common.LanguagePortuguese {
		voice = KokoroVoicePortuguese
		langCode = KokoroLangCodePortuguese
	}

	requestBodyBytes, _ := json.Marshal(map[string]interface{}{
		"input":     string(entry.Chunk.Details.Text),
		"voice":     voice,
		"lang_code": langCode,
		"speed":     KokoroDefaultSpeed,
		"model":     KokoroModelName,
	})

	kokoroURL := string(mouth.Configuration.Settings.KokoroURL)
	request, requestError := http.NewRequestWithContext(applicationContext, "POST", kokoroURL+"/v1/audio/speech", strings.NewReader(string(requestBodyBytes)))
	if requestError != nil { return nil, requestError }
	request.Header.Set("Content-Type", "application/json")

	response, responseError := http.DefaultClient.Do(request)
	if responseError != nil { return nil, responseError }

	if response.StatusCode != http.StatusOK {
		response.Body.Close()
		return nil, fmt.Errorf("kokoro returned status %d", response.StatusCode)
	}

	return response.Body, nil
}

func (mouth *Mouth) detectLanguage(text string, fallback common.Language) common.Language {
	if len(strings.TrimSpace(text)) < 8 {
		return fallback
	}
	info := whatlanggo.Detect(text)
	if info.Lang == whatlanggo.Por {
		return common.LanguagePortuguese
	}
	if info.Lang == whatlanggo.Eng {
		return common.LanguageEnglish
	}
	return fallback
}


func (mouth *Mouth) getCurrentLanguage() common.Language {
	currentLanguage := mouth.Runtime.Playback.CurrentLanguage.Load()
	if currentLanguage != nil {
		return currentLanguage.(common.Language)
	}
	return common.LanguageEnglish
}

func (mouth *Mouth) playAudio(applicationContext context.Context, reader io.Reader, profile *common.Profile, pipeWriter io.Writer) {
	if reader == nil || pipeWriter == nil { return }
	audioStartTime := time.Now()
	filterChain := mouth.BuildFilterChain(mouth.Configuration.Settings.ActiveConfig)
	conversionCommand := exec.CommandContext(applicationContext, "ffmpeg", "-threads", "1", "-i", "pipe:0", "-af", filterChain, "-f", "s16le", "-ar", "24000", "-ac", "1", "pipe:1", "-v", "error")
	conversionCommand.Stdin = reader
	stdout, pipeError := conversionCommand.StdoutPipe()
	if pipeError != nil {
		fmt.Printf("Mouth Error: Failed to create stdout pipe: %v\n", pipeError)
		return
	}

	if startError := conversionCommand.Start(); startError != nil {
		fmt.Printf("Mouth Error: Failed to start ffmpeg: %v\n", startError)
		return
	}

	monitor := &latencyMonitor{reader: stdout, profile: profile, startTime: audioStartTime}
	
	mouth.Runtime.Playback.Mutex.Lock()
	io.Copy(pipeWriter, monitor)
	mouth.Runtime.Playback.Mutex.Unlock()
	
	conversionCommand.Wait()
	profile.AddSpan("Audio_Finish_Chunk", time.Since(audioStartTime))
}

type latencyMonitor struct {
	reader    io.Reader
	profile   *common.Profile
	startTime time.Time
	isDone    bool
}

func (monitor *latencyMonitor) Read(buffer []byte) (bytesRead int, error error) {
	bytesRead, error = monitor.reader.Read(buffer)
	if bytesRead > 0 && !monitor.isDone {
		monitor.profile.AddSpan("Audio_Lag_Chunk", time.Since(monitor.startTime))
		monitor.isDone = true
	}
	return bytesRead, error
}
