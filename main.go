package main

import (
	"context"
	"flag"
	"fmt"
	"mitsu/pkg/brain"
	"mitsu/pkg/common"
	"mitsu/pkg/ear"
	"mitsu/pkg/gaming"
	"mitsu/pkg/mcp"
	"mitsu/pkg/mouth"
	"mitsu/pkg/ui"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"
)

const (
	DefaultServerPort       = ":8080"
	DefaultGameBridgeURL    = "localhost:8888"
	BrainWarmUpDelay        = 3 * time.Second
	ShutdownGracePeriod     = 1 * time.Second
	DefaultVoiceConfig      = "voice_config.json"
	KokoroVoiceAmy          = "af_heart"
	DataChannelBuffer       = 10
	ResponseChannelBuffer   = 100
	UIMessageChannelBuffer  = 100
)

func logWithTimestamp(format string, args ...interface{}) {
	timestamp := time.Now().Format("15:04:05.000")
	fmt.Printf("[%s] "+format+"\n", append([]interface{}{timestamp}, args...)...)
}

func main() {
	languageFlag := flag.String("lang", string(common.LanguageEnglish), "Language to use (en or pt)")
	flag.Parse()

	applicationContext, cancelApplication := context.WithCancel(context.Background())
	defer cancelApplication()

	application := NewMitsuApp(common.Language(*languageFlag))
	application.Initialize(applicationContext)
	application.Run(applicationContext)

	signalChannel := make(chan os.Signal, 1)
	signal.Notify(signalChannel, syscall.SIGINT, syscall.SIGTERM)
	<-signalChannel

	logWithTimestamp("Shutting down...")
	cancelApplication()
	application.CloseMCP()
	time.Sleep(ShutdownGracePeriod)
}

// MitsuApp is the main application container.
type MitsuApp struct {
	State      *AppState
	Components *AppComponents
}

// AppState manages the application-level state.
type AppState struct {
	Language *common.LanguageState
	System   *SystemState
}

// SystemState manages system-level resources.
type SystemState struct {
	Bus *AppBus
	MCP *mcp.Manager
}

// AppBus manages communication channels.
type AppBus struct {
	Data    *DataChannels
	Control *ControlChannels
}

// DataChannels holds the main data flow channels.
type DataChannels struct {
	SpeechToBrain common.SpeechChannel
	BrainToMouth  common.LLMChannel
}

// ControlChannels holds system control channels.
type ControlChannels struct {
	UiMessages  chan string
	ClearMemory chan struct{}
}

// AppComponents aggregates the various functional components.
type AppComponents struct {
	Processing *ProcessingComponents
	Interface  *InterfaceComponents
}

// ProcessingComponents aggregates backend processing modules.
type ProcessingComponents struct {
	IO    *IOComponents
	Brain *brain.Brain
}

// IOComponents aggregates input and output modules.
type IOComponents struct {
	Ear   *ear.Ear
	Mouth *mouth.Mouth
}

// InterfaceComponents aggregates user interface modules.
type InterfaceComponents struct {
	Game *gaming.GameController
	UI   *ui.UIManager
}

// NewMitsuApp creates a new MitsuApp instance.
func NewMitsuApp(initialLanguage common.Language) *MitsuApp {
	bus := &AppBus{
		Data: &DataChannels{
			SpeechToBrain: make(common.SpeechChannel, DataChannelBuffer),
			BrainToMouth:  make(common.LLMChannel, ResponseChannelBuffer),
		},
		Control: &ControlChannels{
			UiMessages:  make(chan string, UIMessageChannelBuffer),
			ClearMemory: make(chan struct{}, 1),
		},
	}

	return &MitsuApp{
		State: &AppState{
			Language: common.NewLanguageState(initialLanguage),
			System: &SystemState{
				Bus: bus,
				MCP: mcp.NewManager(),
			},
		},
		Components: &AppComponents{
			Processing: &ProcessingComponents{
				IO: &IOComponents{},
			},
			Interface: &InterfaceComponents{},
		},
	}
}

// Initialize sets up the application components.
func (application *MitsuApp) Initialize(applicationContext context.Context) {
	logWithTimestamp("Starting Mitsu")
	logWithTimestamp("Initializing Mitsu in %s mode...", strings.ToUpper(string(application.GetLanguage())))

	mcpManager := application.State.System.MCP
	mcpError := mcpManager.Start(applicationContext, "python3", "pkg/mcp/pokemon_server.py")
	if mcpError != nil {
		logWithTimestamp("Warning: Failed to start MCP Manager: %v", mcpError)
	}

	gameBridgeAddress := common.Address(DefaultGameBridgeURL)
	application.Components.Interface.Game = gaming.NewGameController(gameBridgeAddress)
	
	application.Components.Interface.UI = ui.NewUIManager(
		application.State.Language,
		application.Components.Interface.Game,
		application.State.System.Bus.Data.SpeechToBrain,
		application.State.System.Bus.Control.UiMessages,
	)

	application.initializeProcessing()
}
func (application *MitsuApp) initializeProcessing() {
	application.Components.Processing.IO.Ear = &ear.Ear{
		Configuration: &ear.EarConfiguration{
			State: &ear.EarState{
				Language: application.State.Language,
				Device: &ear.EarDevice{
					InputName: "",
					TestInput: os.Getenv("TEST_INPUT_FILE"),
				},
			},
		},
		Execution: &ear.EarExecution{
			Pipeline: &ear.EarPipeline{
				Data: &ear.PipelineData{
					SpeechToBrain: application.State.System.Bus.Data.SpeechToBrain,
					UiMessages:    application.State.System.Bus.Control.UiMessages,
				},
				Status: &ear.PipelineStatus{},
			},
		},
	}

	ollamaURL := common.URL(getEnv("OLLAMA_HOST", "http://localhost:11434"))
	application.Components.Processing.Brain = &brain.Brain{
		Configuration: &brain.BrainConfiguration{
			Connectivity: &brain.BrainConnectivity{
				OllamaURL: ollamaURL,
				MCP:       application.State.System.MCP,
			},
			State: &brain.BrainState{
				Language: application.State.Language,
				Memory: &brain.BrainMemory{
					History:      &brain.ChatHistory{},
					ClearChannel: application.State.System.Bus.Control.ClearMemory,
				},
			},
		},
		Execution: &brain.BrainExecution{
			Data: &brain.BrainData{
				SpeechToBrain: application.State.System.Bus.Data.SpeechToBrain,
				BrainToMouth:  application.State.System.Bus.Data.BrainToMouth,
			},
			UI: &brain.BrainUI{
				UiMessages: application.State.System.Bus.Control.UiMessages,
			},
		},
	}

	kokoroURL := common.URL(getEnv("KOKORO_HOST", "http://kokoro:8880"))
	application.Components.Processing.IO.Mouth = &mouth.Mouth{
		Configuration: &mouth.MouthConfiguration{
			Settings: &mouth.MouthSettings{
				KokoroURL:      kokoroURL,
				KokoroVoiceAmy: KokoroVoiceAmy,
				ActiveConfig:   mouth.LoadVoiceConfig(DefaultVoiceConfig),
			},
			State: &mouth.MouthState{
				Language: application.State.Language,
				Device: &mouth.MouthDevice{
					OutputName: "",
					TestOutput: os.Getenv("TEST_OUTPUT_FILE"),
				},
			},
		},
		Runtime: &mouth.MouthRuntime{
			Data: &mouth.MouthData{
				BrainToMouth: application.State.System.Bus.Data.BrainToMouth,
			},
			Playback: &mouth.MouthPlayback{},
		},
	}
}

// Run starts all application service routines.
func (application *MitsuApp) Run(applicationContext context.Context) {
	go application.Components.Interface.Game.Start(applicationContext)
	go application.Components.Processing.IO.Ear.Start(applicationContext)
	go application.Components.Processing.Brain.Start(applicationContext)
	go application.Components.Processing.IO.Mouth.Start(applicationContext)
	go application.Components.Interface.UI.StartServer(applicationContext, DefaultServerPort)

	go application.warmUp(applicationContext)
}

func (application *MitsuApp) warmUp(applicationContext context.Context) {
	time.Sleep(BrainWarmUpDelay)
	warmUpError := application.Components.Processing.Brain.WarmUp(applicationContext, application.onBrainLoading)
	application.logBrainReady(warmUpError)
}

func (application *MitsuApp) onBrainLoading() {
	thinkingMessage := application.getThinkingMessage()
	logWithTimestamp("Brain: Models not in GPU, playing alert and loading...")
	application.Alert(thinkingMessage, string(application.GetLanguage()))
}

func (application *MitsuApp) getThinkingMessage() string {
	if application.GetLanguage() == common.LanguagePortuguese {
		return "Hmmm, deixa eu ver."
	}
	return "Hmmm, let me think about that."
}

func (application *MitsuApp) logBrainReady(readyError error) {
	if readyError != nil {
		logWithTimestamp("Brain WarmUp error: %v", readyError)
		return
	}
	logWithTimestamp("Brain is ready.")
}

// GetLanguage returns the current system language.
func (application *MitsuApp) GetLanguage() common.Language {
	return application.State.Language.CurrentLanguage()
}

// CloseMCP shuts down the MCP manager.
func (application *MitsuApp) CloseMCP() {
	application.State.System.MCP.Close()
}

// Alert triggers a voice alert through the mouth component.
func (application *MitsuApp) Alert(text string, language string) {
	application.Components.Processing.IO.Mouth.Alert(text, language)
}

func getEnv(key, fallback string) string {
	if value, exists := os.LookupEnv(key); exists {
		return value
	}
	return fallback
}
