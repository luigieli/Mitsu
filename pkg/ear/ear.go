package ear

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"mitsu/pkg/common"
	"net/http"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/maxhawkins/go-webrtcvad"
)

type Ear struct {
	STTURL             string
	mu                 sync.RWMutex
	CurrentLang        string
	LanguageChangeChan chan string
	IsMitsuSpeaking    *atomic.Bool
	SpeechToBrain      common.SpeechText
	UiMessages         chan string
	InputDevice        string
	TestInput          string
}

func (e *Ear) Start(ctx context.Context) {
	fmt.Printf("Ear Routine started with Hybrid Go-VAD Pipeline (Ruthless Profile)\n")

	// Sync initial language with STT server
	go func() {
		e.mu.RLock()
		lang := e.CurrentLang
		e.mu.RUnlock()
		if lang != "" {
			http.Post(e.STTURL+"/swap/"+lang, "application/json", nil)
		}
	}()

	// Background listener for language hotswaps
	go func() {
		for {
			select {
			case newLang := <-e.LanguageChangeChan:
				e.mu.Lock()
				fmt.Printf("Ear: Hotswapping language to %s\n", newLang)
				e.CurrentLang = newLang
				e.mu.Unlock()
				// Notify STT Server of the swap
				go func(lang string) {
					_, err := http.Post(e.STTURL+"/swap/"+lang, "application/json", nil)
					if err != nil {
						fmt.Printf("Ear Error: Failed to notify STT server of swap: %v\n", err)
					}
				}(newLang)
			case <-ctx.Done():
				return
			}
		}
	}()

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
		// 1.02s Silence = End of Phrase
		silenceLimit    = 34
	)

	for {
		select {
		case <-ctx.Done():
			return
		default:
			var stdout io.ReadCloser
			var cmd *exec.Cmd

			// Capture Command
			args := []string{
				"-f", "pulse", "-name", "Mitsu_Ear", "-i", "default",
				"-ar", "16000", "-ac", "1",
				"-af", "highpass=f=80",
				"-f", "s16le", "pipe:1",
				"-v", "error",
			}
			if e.InputDevice != "" {
				args[5] = e.InputDevice
			}
			cmd = exec.CommandContext(ctx, "ffmpeg", args...)
			stdout, _ = cmd.StdoutPipe()

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
					break
				}

				if e.IsMitsuSpeaking.Load() {
					continue
				}

				active, _ := vad.Process(sampleRate, buffer)

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

					// Trigger on silence
					if silenceCounter >= silenceLimit {
						fmt.Printf("VAD: Sentence finished. Buffer: %d bytes\n", len(speechBuffer))

						if len(speechBuffer) > sampleRate {
							finalBuffer := make([]byte, len(speechBuffer))
							copy(finalBuffer, speechBuffer)
							prof := common.NewProfile()
							go e.transcribe(ctx, finalBuffer, prof)
						}

						speechBuffer = nil
						isSpeaking = false
						silenceCounter = 0
					}
				}
			}
			cmd.Process.Kill()
			time.Sleep(100 * time.Millisecond)
		}
	}
}

func (e *Ear) cleanAudioWithFFmpeg(inputData []byte) []byte {
	cmd := exec.Command("ffmpeg",
		"-f", "s16le", "-ar", "16000", "-ac", "1", "-i", "pipe:0",
		// THE MAGIC FILTER (RELAXED):
		// 1. highpass=f=80: Lower cutoff to preserve more voice depth.
		// 2. start_periods=1: Remove start silence.
		// 3. stop_periods=-1: RECURSIVE REMOVAL. Removes ALL silence detected.
		// 4. stop_duration=0.4: Slightly more breathing room between words.
		// 5. threshold=-35dB: Much more sensitive to catch quiet speech.
		"-af", "highpass=f=80,silenceremove=start_periods=1:start_threshold=-35dB:stop_periods=-1:stop_duration=0.4:stop_threshold=-35dB",
		"-fflags", "+bitexact", "-f", "wav", "pipe:1",
		"-v", "error",
	)
	stdin, _ := cmd.StdinPipe()
	stdout, _ := cmd.StdoutPipe()
	if err := cmd.Start(); err != nil {
		return inputData
	}
	go func() {
		defer stdin.Close()
		stdin.Write(inputData)
	}()
	cleanedData, _ := io.ReadAll(stdout)
	cmd.Wait()
	return cleanedData
}

func (e *Ear) transcribe(ctx context.Context, audioData []byte, prof *common.Profile) {
	start := time.Now()
	fmt.Printf("[Ear] Audio before wash: %d bytes\n", len(audioData))
	cleaned := e.cleanAudioWithFFmpeg(audioData)
	fmt.Printf("[Ear] Audio after wash: %d bytes (Removed %d bytes)\n", len(cleaned), len(audioData)-len(cleaned))
	prof.AddSpan("STT_Wash", time.Since(start))

	whisperStart := time.Now()
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	part, _ := writer.CreateFormFile("audio", "audio.wav")
	part.Write(cleaned)
	writer.Close()

	resp, err := http.Post(e.STTURL+"/transcribe", writer.FormDataContentType(), body)
	if err != nil {
		fmt.Printf("STT API Error: %v\n", err)
		return
	}
	defer resp.Body.Close()
	prof.AddSpan("STT_Inference", time.Since(whisperStart))

	var result struct {
		Text string `json:"text"`
	}
	json.NewDecoder(resp.Body).Decode(&result)

	// DEBUG: Show exactly what was captured by the STT
	fmt.Printf("[Ear] RAW STT: \"%s\"\n", result.Text)

	if result.Text != "" {
		text := e.ApplyFuzzyNameCorrection(result.Text)

		// Use the hotswapped language strictly (thread-safe)
		e.mu.RLock()
		detectedLang := e.CurrentLang
		e.mu.RUnlock()

		if detectedLang == "" {
			detectedLang = "en" // Absolute fallback
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

func (e *Ear) ApplyFuzzyNameCorrection(text string) string {
	misspelled := map[string]bool{
		"mitzo": true, "mitso": true, "metso": true, "metsu": true, "mitsu": true, "mitzu": true,
	}
	words := strings.Split(text, " ")
	for i, word := range words {
		if word == "" { continue }
		start, end := 0, len(word)
		for start < len(word) && isPunct(word[start]) { start++ }
		for end > start && isPunct(word[end-1]) { end-- }
		if start < end {
			core := strings.ToLower(word[start:end])
			if misspelled[core] { words[i] = word[:start] + "Mitsu" + word[end:] }
		}
	}
	return strings.Join(words, " ")
}

func isPunct(b byte) bool {
	return b == ',' || b == '.' || b == '!' || b == '?' || b == '"' || b == '\'' || b == '(' || b == ')'
}
