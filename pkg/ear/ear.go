package ear

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mitsu/pkg/common"
	"os"
	"os/exec"
	"strings"
	"sync/atomic"
	"time"

	"github.com/maxhawkins/go-webrtcvad"
)

type Ear struct {
	WhisperModel    string
	CurrentLang     string
	IsMitsuSpeaking *atomic.Bool
	SpeechToBrain   common.SpeechText
	UiMessages      chan string
	InputDevice     string
	TestInput       string // Path to a wav file to play once for testing
}

func (e *Ear) ApplyFuzzyNameCorrection(text string) string {
	cleanText := strings.ReplaceAll(text, ",", "")
	cleanText = strings.ReplaceAll(cleanText, "!", "")
	cleanText = strings.ReplaceAll(cleanText, "?", "")

	variations := []string{"mitzo", "mitso", "metso", "metsu", "mitsu", "mitzu"}
	lowerText := strings.ToLower(cleanText)
	for _, v := range variations {
		if idx := strings.Index(lowerText, v); idx != -1 {
			return cleanText[:idx] + "Mitsu" + cleanText[idx+len(v):]
		}
	}
	return cleanText
}

func (e *Ear) Start(ctx context.Context) {
	fmt.Printf("Ear Routine started with VAD on %s\n", e.InputDevice)

	vad, err := webrtcvad.New()
	if err != nil {
		fmt.Printf("Error creating VAD: %v\n", err)
		return
	}
	vad.SetMode(3)

	const (
		sampleRate      = 16000
		frameDurationMs = 30
		frameSize       = sampleRate * frameDurationMs / 1000
		byteSize        = frameSize * 2
		silenceLimit    = 15 // ~450ms
	)

	for {
		select {
		case <-ctx.Done():
			return
		default:
			var stdout io.ReadCloser
			var cmd *exec.Cmd

			if e.TestInput != "" {
				fmt.Printf("Ear: Running in test mode with file %s\n", e.TestInput)
				cmd = exec.CommandContext(ctx, "ffmpeg", "-i", e.TestInput, "-f", "s16le", "-ar", "16000", "-ac", "1", "pipe:1")
				stdout, _ = cmd.StdoutPipe()
				e.TestInput = "" // Run once
			} else {
				cmd = exec.CommandContext(ctx, "parec", "-d", e.InputDevice, "--format=s16le", "--channels=1", "--rate=16000", "--property=application.name=Mitsu_Ear")
				stdout, _ = cmd.StdoutPipe()
			}

			if err := cmd.Start(); err != nil {
				time.Sleep(1 * time.Second)
				continue
			}

			buffer := make([]byte, byteSize)
			var speechBuffer []byte
			isSpeaking := false
			silenceCounter := 0

			for {
				_, err := io.ReadFull(stdout, buffer)
				if err != nil {
					if err == io.EOF || err == io.ErrUnexpectedEOF {
						if isSpeaking {
							fmt.Printf("VAD: EOF reached while speaking. Forcing transcription. Buffer size: %d\n", len(speechBuffer))
							finalBuffer := make([]byte, len(speechBuffer))
							copy(finalBuffer, speechBuffer)
							go e.transcribe(ctx, finalBuffer)
							isSpeaking = false
						}
					}
					break
				}

				active, _ := vad.Process(sampleRate, buffer)

				if e.IsMitsuSpeaking.Load() {
					active = false
				}

				if active {
					if !isSpeaking {
						isSpeaking = true
						fmt.Println("VAD: Speech detected...")
					}
					speechBuffer = append(speechBuffer, buffer...)
					silenceCounter = 0
				} else if isSpeaking {
					speechBuffer = append(speechBuffer, buffer...)
					silenceCounter++

					if silenceCounter >= silenceLimit {
						fmt.Printf("VAD: Sentence finished. Buffer size: %d\n", len(speechBuffer))
						if len(speechBuffer) > sampleRate*2 {
							finalBuffer := make([]byte, len(speechBuffer))
							copy(finalBuffer, speechBuffer)
							go e.transcribe(ctx, finalBuffer)
						} else {
							fmt.Println("VAD: Buffer too small, skipping transcription.")
						}
						speechBuffer = nil
						isSpeaking = false
						silenceCounter = 0
					}
				}
			}
			cmd.Process.Kill()
			if ctx.Err() != nil {
				return
			}
			time.Sleep(100 * time.Millisecond)
		}
	}
}

func (e *Ear) transcribe(ctx context.Context, audioData []byte) {
	tempFile := fmt.Sprintf("/tmp/mitsu_audio_%d.raw", time.Now().UnixNano())
	os.WriteFile(tempFile, audioData, 0644)
	defer os.Remove(tempFile)

	wavFile := tempFile + ".wav"
	exec.Command("ffmpeg", "-y", "-f", "s16le", "-ar", "16000", "-ac", "1", "-i", tempFile, wavFile).Run()
	defer os.Remove(wavFile)

	whisperCmd := exec.CommandContext(ctx, "./whisper-cpp", "-m", e.WhisperModel, "-f", wavFile, "-nt", "-np", "-l", e.CurrentLang, "-bs", "1", "-t", "4")
	out, _ := whisperCmd.CombinedOutput()

	text := strings.TrimSpace(string(out))
	if text != "" && !strings.HasPrefix(text, "[") {
		text = e.ApplyFuzzyNameCorrection(text)

		fmt.Printf("Captured: %s\n", text)
		msg, _ := json.Marshal(map[string]string{"text": text, "type": "mic"})
		select {
		case e.UiMessages <- string(msg):
		default:
		}
		e.SpeechToBrain <- common.SpeechEntry{Text: text, Timestamp: time.Now()}
	}
}
