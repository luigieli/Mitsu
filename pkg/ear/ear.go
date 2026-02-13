package ear

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"mitsu/pkg/common"
	"os"
	"os/exec"
	"strings"
	"sync/atomic"
	"time"
)

type Ear struct {
	WhisperModel    string
	CurrentLang     string
	IsMitsuSpeaking *atomic.Bool
	SpeechToBrain   common.SpeechText
	UiMessages      chan string
}

func (e *Ear) ApplyFuzzyNameCorrection(text string) string {
	variations := []string{"mitzo", "mitso", "metso", "metsu", "mitsu", "mitzu"}
	lowerText := strings.ToLower(text)
	for _, v := range variations {
		if idx := strings.Index(lowerText, v); idx != -1 {
			return text[:idx] + "Mitsu" + text[idx+len(v):]
		}
	}
	return text
}

func (e *Ear) Start(ctx context.Context) {
	fmt.Println("Ear Routine started.")
	const (
		baseThreshold = 1500
		silenceLimit  = 1200
		chunkSize     = 3200
	)
	isSpeaking := false

	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case <-time.After(500 * time.Millisecond):
				status := "IDLE"
				if e.IsMitsuSpeaking.Load() {
					status = "SPEAKING..."
				} else if isSpeaking {
					status = "LISTENING"
				}
				msg, _ := json.Marshal(map[string]string{"text": status, "type": "status"})
				select {
				case e.UiMessages <- string(msg):
				default:
				}
			}
		}
	}()

	for {
		recordCmd := exec.CommandContext(ctx, "parec", "-d", "denoised_mic", "--format=s16le", "--channels=1", "--rate=16000", "--property=application.name=Mitsu_Ears")
		stdout, _ := recordCmd.StdoutPipe()
		if err := recordCmd.Start(); err != nil {
			time.Sleep(1 * time.Second)
			continue
		}
		buffer := new(bytes.Buffer)
		silenceMs := 0
		data := make([]byte, chunkSize)
		for {
			n, err := stdout.Read(data)
			if err != nil || n == 0 {
				break
			}

			maxAmp := 0
			for i := 0; i < n; i += 2 {
				sample := int16(data[i]) | int16(data[i+1])<<8
				if sample < 0 {
					sample = -sample
				}
				if int(sample) > maxAmp {
					maxAmp = int(sample)
				}
			}

			effThreshold := baseThreshold
			if e.IsMitsuSpeaking.Load() {
				effThreshold = baseThreshold * 2.5
			}

			if maxAmp > effThreshold {
				if !isSpeaking {
					isSpeaking = true
				}
				buffer.Write(data[:n])
				silenceMs = 0
			} else if isSpeaking {
				buffer.Write(data[:n])
				silenceMs += 100
				if silenceMs >= silenceLimit {
					isSpeaking = false
					break
				}
			}
		}
		recordCmd.Process.Kill()
		if buffer.Len() > 32000 {
			tempFile := "/tmp/phrase.raw"
			os.WriteFile(tempFile, buffer.Bytes(), 0644)
			wavFile := "/tmp/phrase.wav"
			exec.Command("ffmpeg", "-y", "-f", "s16le", "-ar", "16000", "-ac", "1", "-i", tempFile, wavFile).Run()

			whisperCmd := exec.CommandContext(ctx, "./whisper-cpp", "-m", e.WhisperModel, "-f", wavFile, "-nt", "-np", "-l", e.CurrentLang, "-bs", "1", "-t", "4", "-ngl", "100")
			out, _ := whisperCmd.CombinedOutput()
			text := strings.TrimSpace(string(out))
			if text != "" && !strings.HasPrefix(text, "[") {
				text = e.ApplyFuzzyNameCorrection(text)

				fmt.Printf("Captured: \"%s\"\n", text)
				msg, _ := json.Marshal(map[string]string{"text": text, "type": "mic"})
				select {
				case e.UiMessages <- string(msg):
				default:
				}
				e.SpeechToBrain <- common.SpeechEntry{Text: text, Timestamp: time.Now()}
			}
		}
	}
}
