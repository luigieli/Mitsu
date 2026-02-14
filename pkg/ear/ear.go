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
	"sync/atomic"
	"time"

	"github.com/abadojack/whatlanggo"
	"github.com/maxhawkins/go-webrtcvad"
)

type Ear struct {
	STTURL          string
	CurrentLang     string
	IsMitsuSpeaking *atomic.Bool
	SpeechToBrain   common.SpeechText
	UiMessages      chan string
	InputDevice     string
	TestInput       string 
}

func (e *Ear) Start(ctx context.Context) {
	fmt.Printf("Ear Routine started with Hybrid Go-VAD Pipeline\n")

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
		silenceLimit    = 40 // ~1.2s
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
				"-af", "highpass=f=200",
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

					if silenceCounter >= silenceLimit {
						fmt.Printf("VAD: Sentence finished. Buffer size: %d\n", len(speechBuffer))
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
		"-af", "highpass=f=200,silenceremove=1:0.1:-40dB:1:0.1:-40dB",
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
	if len(cleanedData) == 0 { return inputData }
	return cleanedData
}

func (e *Ear) transcribe(ctx context.Context, audioData []byte, prof *common.Profile) {
	start := time.Now()
	cleaned := e.cleanAudioWithFFmpeg(audioData)
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

	if result.Text != "" {
		text := e.ApplyFuzzyNameCorrection(result.Text)
		info := whatlanggo.Detect(text)
		detectedLang := "en"
		if info.Lang == whatlanggo.Por { detectedLang = "pt" }

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
