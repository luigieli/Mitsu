package mouth

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
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
	formant_val := "preserved"
	if !f.FormantPreserved {
		formant_val = "shifted"
	}

	return fmt.Sprintf(
		"highpass=f=%d,lowpass=f=%d,equalizer=f=400:t=q:w=1:g=%d,equalizer=f=3000:t=q:w=1:g=%d,equalizer=f=8000:t=q:w=1:g=%d,"+
			"rubberband=pitch=%.2f:tempo=%.2f:formant=%s,deesser=i=%.2f,aexciter=amount=%.2f,pan=stereo|c0=c0|c1=c0,extrastereo=m=%.2f,"+
			"loudnorm=I=%d:LRA=11:TP=-1.5",
		f.Highpass, f.Lowpass, f.BoxyGain, f.PresenceGain, f.SparkleGain,
		f.Pitch, f.Speed, formant_val, f.DeesserIntensity, f.ExciterAmount, f.StereoWidth,
		f.LoudnormI,
	)
}

func (m *Mouth) Start(ctx context.Context) {
	fmt.Println("Mouth Routine started.")

	for {
		select {
		case sentence := <-m.BrainToMouth:
			if sentence == "" {
				continue
			}
			playCtx, playCancel := context.WithCancel(ctx)
			go func() {
				select {
				case <-m.BargeIn:
					fmt.Println("Mouth: Interrupted!")
					playCancel()
				case <-playCtx.Done():
				}
			}()

			m.IsMitsuSpeaking.Store(true)

			// Use the dynamically loaded config
			voice := m.ActiveConfig.VoiceModel
			langCode := m.ActiveConfig.LangCode

			// Override for English mode if set via CLI
			if m.CurrentLang == "en" && voice == "mitsu_custom" {
				voice = m.KokoroVoiceAmy
				langCode = "a"
			}

			reqBody, _ := json.Marshal(map[string]interface{}{
				"input":     sentence,
				"voice":     voice,
				"lang_code": langCode,
				"speed":     1.0, // We use rubberband for tempo now
				"model":     "kokoro",
			})

			resp, err := http.Post(m.KokoroURL+"/v1/audio/speech", "application/json", bytes.NewBuffer(reqBody))
			if err != nil {
				fmt.Printf("Kokoro Error: %v\n", err)
				m.IsMitsuSpeaking.Store(false)
				playCancel()
				continue
			}

			// BUILD FILTER CHAIN FROM CONFIG
			filterChain := m.BuildFilterChain(m.ActiveConfig)

			ffmpegCmd := exec.CommandContext(playCtx, "ffmpeg", "-i", "pipe:0", "-af", filterChain, "-f", "wav", "pipe:1")
			ffmpegCmd.Stdin = resp.Body
			stdout, _ := ffmpegCmd.StdoutPipe()

			paplayCmd := exec.CommandContext(playCtx, "paplay", "--property=application.name=Mitsu")
			paplayCmd.Stdin = stdout

			fmt.Printf("Mouth playing (Studio Active): \"%s\"\n", sentence)
			if err := ffmpegCmd.Start(); err != nil {
				resp.Body.Close()
				m.IsMitsuSpeaking.Store(false)
				playCancel()
				continue
			}
			if err := paplayCmd.Start(); err != nil {
				resp.Body.Close()
				m.IsMitsuSpeaking.Store(false)
				playCancel()
				continue
			}

			paplayCmd.Wait()
			ffmpegCmd.Process.Kill()
			resp.Body.Close()
			m.IsMitsuSpeaking.Store(false)
			playCancel()
			fmt.Println("Mouth finished.")
		case <-ctx.Done():
			return
		}
	}
}
