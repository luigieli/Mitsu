package mouth

import (
	"context"
	"mitsu/pkg/common"
	"strings"
	"testing"
	"time"
)

func TestMouth_Start_Simple(t *testing.T) {
	languageState := common.NewLanguageState(common.LanguageEnglish)
	brainToMouth := make(common.LLMChannel, 10)

	mouthComponent := &Mouth{
		Configuration: &MouthConfiguration{
			Settings: &MouthSettings{},
			State: &MouthState{
				Language: languageState,
				Device: &MouthDevice{
					TestOutput: "/dev/null",
				},
			},
		},
		Runtime: &MouthRuntime{
			Data: &MouthData{
				BrainToMouth: brainToMouth,
			},
			Playback: &MouthPlayback{},
		},
	}

	applicationContext, cancelApplication := context.WithCancel(context.Background())
	defer cancelApplication()

	go mouthComponent.Start(applicationContext)

	// Send a sentence
	brainToMouth <- common.LLMEntry{
		Chunk: common.LLMChunk{
			Details: common.LLMDetails{
				Text:          common.LLMResponseContent("This is a test sentence."),
				InputLanguage: common.LanguageEnglish,
			},
			Done: true,
		},
		Context: common.EntryContext{
			Timestamp: time.Now(),
			Profile:   common.NewProfile(),
		},
	}

	time.Sleep(100 * time.Millisecond)
}

func TestBuildFilterChain(t *testing.T) {
	mouthComponent := &Mouth{}
	config := VoiceConfig{
		Pitch: 1.2,
		Speed: 1.1,
	}

	chain := mouthComponent.BuildFilterChain(config)
	
	if !strings.Contains(chain, "asetrate=24000*1.20") {
		t.Errorf("BuildFilterChain() output missing expected pitch: %q", chain)
	}
}
