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
	"sync/atomic"
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
	// Anime/Cute Voice Post-Processing Chain
	// 1.15 = 15% higher pitch
	pitchFactor := 1.15

	return fmt.Sprintf(
		"asetrate=24000*%.2f,atempo=1/%.2f,highpass=f=200,equalizer=f=4000:t=h:width=2000:g=4,compand=attacks=0:points=-90/-90|-40/-40|0/-10|20/-10:gain=5",
		pitchFactor, pitchFactor,
	)
}

func (m *Mouth) Start(ctx context.Context) {
	fmt.Println("Mouth Routine started with Persistent Stream.")

	pr, pw := io.Pipe()

	// Persistent playback process using pacat
	// This ensures a fixed node in qpwgraph
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
		case sentence := <-m.BrainToMouth:
			if sentence == "" {
				continue
			}

			m.IsMitsuSpeaking.Store(true)

			voice := "mitsu_anime_en"
			langCode := "a"
			if m.CurrentLang == "pt" {
				voice = "mitsu_anime_pt"
				langCode = "p"
			}

			reqBody, _ := json.Marshal(map[string]interface{}{
				"input":     sentence,
				"voice":     voice,
				"lang_code": langCode,
				"speed":     1.1, // Faster = Cuter
				"model":     "kokoro",
			})

			resp, err := http.Post(m.KokoroURL+"/v1/audio/speech", "application/json", bytes.NewBuffer(reqBody))
			if err != nil {
				fmt.Printf("Kokoro Error: %v\n", err)
				m.IsMitsuSpeaking.Store(false)
				continue
			}

			filterChain := m.BuildFilterChain(m.ActiveConfig)

			convCmd := exec.CommandContext(ctx, "ffmpeg", "-i", "pipe:0", "-af", filterChain, "-f", "s16le", "-ar", "24000", "-ac", "1", "pipe:1")
			convCmd.Stdin = resp.Body
			stdout, _ := convCmd.StdoutPipe()

			if err := convCmd.Start(); err != nil {
				resp.Body.Close()
				m.IsMitsuSpeaking.Store(false)
				continue
			}

			fmt.Printf("Mouth speaking: \"%s\"\n", sentence)
			io.Copy(pw, stdout)
			
			convCmd.Wait()
			resp.Body.Close()
			m.IsMitsuSpeaking.Store(false)
			fmt.Println("Mouth finished sentence.")

		case <-ctx.Done():
			pw.Close()
			playbackCmd.Wait()
			return
		}
	}
}
