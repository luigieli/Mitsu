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

	"github.com/abadojack/whatlanggo"
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
	misspelled := map[string]bool{
		"mitzo": true, "mitso": true, "metso": true, "metsu": true, "mitsu": true, "mitzu": true,
	}
	words := strings.Split(text, " ")
	for i, word := range words {
		if word == "" {
			continue
		}
		// Find core word without surrounding punctuation
		start := 0
		for start < len(word) && isPunct(word[start]) {
			start++
		}
		end := len(word)
		for end > start && isPunct(word[end-1]) {
			end--
		}

		if start < end {
			core := strings.ToLower(word[start:end])
			if misspelled[core] {
				words[i] = word[:start] + "Mitsu" + word[end:]
			}
		}
	}
	return strings.Join(words, " ")
}

func isPunct(b byte) bool {
	return b == ',' || b == '.' || b == '!' || b == '?' || b == '"' || b == '\'' || b == '(' || b == ')'
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
				// FFmpeg Front-End: Capture and Clean before Go VAD
				args := []string{
					"-f", "pulse", "-name", "Mitsu_Ear", "-i", "default",
					"-ar", "16000", "-ac", "1",
					"-af", "highpass=f=200,lowpass=f=3000",
					"-f", "s16le", "pipe:1",
					"-v", "error",
				}
				if e.InputDevice != "" {
					args[5] = e.InputDevice
				}
				cmd = exec.CommandContext(ctx, "ffmpeg", args...)
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
							prof := common.NewProfile()
							go e.transcribe(ctx, finalBuffer, prof)
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
							prof := common.NewProfile()
							go e.transcribe(ctx, finalBuffer, prof)
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

func (e *Ear) transcribe(ctx context.Context, audioData []byte, prof *common.Profile) {
	start := time.Now()
	tempFile := fmt.Sprintf("/tmp/mitsu_audio_%d.raw", time.Now().UnixNano())
	os.WriteFile(tempFile, audioData, 0644)
	defer os.Remove(tempFile)

	wavFile := tempFile + ".wav"
	exec.Command("ffmpeg", "-y", "-f", "s16le", "-ar", "16000", "-ac", "1", "-i", tempFile, wavFile).Run()
	defer os.Remove(wavFile)

	whisperCmd := exec.CommandContext(ctx, "./whisper-cpp", "-m", e.WhisperModel, "-f", wavFile, "-nt", "-np", "-l", "auto", "-bs", "1", "-t", "4")
	out, _ := whisperCmd.CombinedOutput()
	text := strings.TrimSpace(string(out))

	prof.AddSpan("STT", time.Since(start))

	if text != "" && !strings.HasPrefix(text, "[") {
		text = e.ApplyFuzzyNameCorrection(text)

		// Detect language using whatlanggo
		info := whatlanggo.Detect(text)
		detectedLang := "en"
		if info.Lang == whatlanggo.Por {
			detectedLang = "pt"
		}

		fmt.Printf("Captured (%s): %s\n", detectedLang, text)
		msg, _ := json.Marshal(map[string]string{"text": text, "type": "mic"})
		select {
		case e.UiMessages <- string(msg):
		default:
		}
		e.SpeechToBrain <- common.SpeechEntry{Text: text, Language: detectedLang, Timestamp: time.Now(), Profile: prof}
	}
}
