package mouth

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mitsu/pkg/common"
	"net/http"
	"os/exec"
	"strings"
	"sync/atomic"
	"time"

	"github.com/abadojack/whatlanggo"
)

type Mouth struct {
	KokoroURL       string
	CurrentLang     string
	ActiveConfig    VoiceConfig
	IsMitsuSpeaking *atomic.Bool
	BrainToMouth    common.LLMResponse
	BargeIn         chan struct{}
	KokoroVoiceAmy  string
	OutputDevice    string
	TestOutput      string // Path to a wav file to save output for testing
}

// Voice Configuration from Lab
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

func (m *Mouth) BuildFilterChain(f VoiceConfig) string {
	pitchFactor := 1.15
	return fmt.Sprintf(
		"asetrate=24000*%.2f,atempo=1/%.2f,highpass=f=200,equalizer=f=4000:t=h:width=2000:g=4,compand=attacks=0:points=-90/-90|-40/-40|0/-10|20/-10:gain=5",
		pitchFactor, pitchFactor,
	)
}

func (m *Mouth) Start(ctx context.Context) {
	fmt.Println("Mouth Routine started with Streaming Sequential Support.")

	pr, pw := io.Pipe()

	var playbackCmd *exec.Cmd
	if m.TestOutput != "" {
		fmt.Printf("Mouth: Running in test mode, saving output to %s\n", m.TestOutput)
		playbackCmd = exec.CommandContext(ctx, "ffmpeg", "-y",
			"-f", "s16le", "-ar", "24000", "-ac", "1", "-i", "pipe:0",
			m.TestOutput)
	} else {
		args := []string{"--playback", "--format=s16le", "--channels=1", "--rate=24000", "--property=application.name=Mitsu_Mouth"}
		if m.OutputDevice != "" {
			args = append(args, "-d", m.OutputDevice)
		}
		playbackCmd = exec.CommandContext(ctx, "pacat", args...)
	}
	
	playbackCmd.Stdin = pr
	if err := playbackCmd.Start(); err != nil {
		fmt.Printf("Failed to start persistent playback: %v\n", err)
		return
	}

	for {
		select {
		case entry := <-m.BrainToMouth:
			sentence := entry.Text
			
			// Always ensure speaking is true when we get data
			m.IsMitsuSpeaking.Store(true)

			if sentence != "" {
				// 1. Detect the ACTUAL language of the chunk
				info := whatlanggo.Detect(sentence)
				detectedLang := "en"
				if info.Lang == whatlanggo.Por {
					detectedLang = "pt"
				} else if info.Lang == whatlanggo.Eng {
					detectedLang = "en"
				} else {
					detectedLang = entry.InputLanguage
				}

				if len(strings.Split(sentence, " ")) < 5 {
					detectedLang = entry.InputLanguage
				}

				// 2. Select Voice
				voice := "mitsu_anime_en"
				langCode := "a"
				if detectedLang == "pt" {
					voice = "mitsu_anime_pt"
					langCode = "p"
				}

				reqBody, _ := json.Marshal(map[string]interface{}{
					"input":     sentence,
					"voice":     voice,
					"lang_code": langCode,
					"speed":     1.1,
					"model":     "kokoro",
				})

				ttsStart := time.Now()
				resp, err := http.Post(m.KokoroURL+"/v1/audio/speech", "application/json", bytes.NewBuffer(reqBody))
				if err != nil {
					fmt.Printf("Kokoro Error: %v\n", err)
					continue
				}
				
				audioData, err := io.ReadAll(resp.Body)
				resp.Body.Close()
				if err != nil {
					continue
				}
				entry.Profile.AddSpan("TTS_Chunk", time.Since(ttsStart))

				filterChain := m.BuildFilterChain(m.ActiveConfig)
				audioStart := time.Now()
				convCmd := exec.CommandContext(ctx, "ffmpeg", "-i", "pipe:0", "-af", filterChain, "-f", "s16le", "-ar", "24000", "-ac", "1", "pipe:1")
				convCmd.Stdin = bytes.NewReader(audioData)
				stdout, _ := convCmd.StdoutPipe()

				if err := convCmd.Start(); err != nil {
					continue
				}

				fmt.Printf("Mitsu speaking: \"%s\"\n", sentence)
				io.Copy(pw, stdout)
				convCmd.Wait()
				entry.Profile.AddSpan("AudioPost_Chunk", time.Since(audioStart))
			}

			// If this is the last chunk, we can finally stop speaking and report
			if entry.Done {
				m.IsMitsuSpeaking.Store(false)
				fmt.Println("Mitsu finished entire response.")
				fmt.Printf("[PROFILER] %s\n", entry.Profile.Summary())
			}

		case <-ctx.Done():
			pw.Close()
			playbackCmd.Wait()
			return
		}
	}
}
