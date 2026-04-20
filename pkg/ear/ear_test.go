package ear

import (
	"context"
	"mitsu/pkg/common"
	"testing"
	"time"
)

func TestEar_Start_Persistence(t *testing.T) {
	speechToBrain := make(common.SpeechChannel, 10)
	languageState := common.NewLanguageState(common.LanguageEnglish)
	uiMessages := make(chan string, 10)

	earComponent := &Ear{
		Configuration: &EarConfiguration{
			Connectivity: &EarConnectivity{
				SpeechToTextURL: "http://localhost:5001",
			},
			State: &EarState{
				Language: languageState,
				Device: &EarDevice{
					TestInput: "/dev/zero",
				},
			},
		},
		Execution: &EarExecution{
			Pipeline: &EarPipeline{
				SpeechToBrain: speechToBrain,
				UiMessages:    uiMessages,
			},
			Streaming: &EarStreaming{
				WebSocket: &SynchronizedWebSocket{},
			},
		},
	}

	applicationContext, cancelApplication := context.WithCancel(context.Background())
	defer cancelApplication()

	// This is a smoke test to ensure Start doesn't crash
	go earComponent.Start(applicationContext)

	time.Sleep(100 * time.Millisecond)
	// Success if it didn't panic
}
