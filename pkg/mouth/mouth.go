package mouth

import (
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
)

type Mouth struct {
	KokoroURL          string
	CurrentLang        string
	LanguageChangeChan chan string
	ActiveConfig       VoiceConfig
	IsMitsuSpeaking    *atomic.Bool
	BrainToMouth       common.LLMResponse
	BargeIn            chan struct{}
	KokoroVoiceAmy     string
	OutputDevice       string
	TestOutput         string 
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

func (m *Mouth) Say(text string, lang string) {
	m.BrainToMouth <- common.LLMEntry{
		Text:          text,
		InputLanguage: lang,
		Profile:       common.NewProfile(),
		Done:          true,
	}
}

func (m *Mouth) Alert(text string, lang string) {
	// Immediate vocalization bypass
	m.Say(text, lang)
}

func (m *Mouth) Start(ctx context.Context) {
	fmt.Println("Mouth Routine started with Parallel Streaming Support.")

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
		case newLang := <-m.LanguageChangeChan:
			fmt.Printf("Mouth: Hotswapping language to %s\n", newLang)
			m.CurrentLang = newLang
		case entry := <-m.BrainToMouth:
			sentence := entry.Text
			m.IsMitsuSpeaking.Store(true)

			if sentence != "" {
				// 1. Voice Selection (Locked to the language provided by the Brain/Hotswap)
				detectedLang := entry.InputLanguage
				if detectedLang == "" {
					detectedLang = m.CurrentLang
				}

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

				// 2. Start Request & Profile Startup
				audioStart := time.Now()
				resp, err := http.Post(m.KokoroURL+"/v1/audio/speech", "application/json", strings.NewReader(string(reqBody)))
				if err != nil {
					fmt.Printf("Kokoro Error: %v\n", err)
					continue
				}

				// 3. Parallel Filtering: Stream Body -> FFmpeg -> pacat
				filterChain := m.BuildFilterChain(m.ActiveConfig)
				// We add -threads 1 to reduce startup overhead
				convCmd := exec.CommandContext(ctx, "ffmpeg", "-threads", "1", "-i", "pipe:0", "-af", filterChain, "-f", "s16le", "-ar", "24000", "-ac", "1", "pipe:1", "-v", "error")
				convCmd.Stdin = resp.Body
				stdout, _ := convCmd.StdoutPipe()

				if err := convCmd.Start(); err != nil {
					resp.Body.Close()
					continue
				}

				fmt.Printf("Mitsu speaking: \"%s\"\n", sentence)
				
				// Monitor first byte to measure real lag
				monitor := &latencyMonitor{r: stdout, prof: entry.Profile, start: audioStart}
				io.Copy(pw, monitor)
				
				convCmd.Wait()
				resp.Body.Close()
				entry.Profile.AddSpan("Audio_Finish_Chunk", time.Since(audioStart))
			}

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

type latencyMonitor struct {
	r     io.Reader
	prof  *common.Profile
	start time.Time
	done  bool
}

func (l *latencyMonitor) Read(p []byte) (n int, err error) {
	n, err = l.r.Read(p)
	if n > 0 && !l.done {
		// This measures how long it took from Request to first Sound hitting the pipe
		l.prof.AddSpan("Audio_Lag_Chunk", time.Since(l.start))
		l.done = true
	}
	return n, err
}
