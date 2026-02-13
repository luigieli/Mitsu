package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"sync/atomic"
	"syscall"
	"time"
)

// Helper for timestamped logs
func tlog(format string, a ...interface{}) {
	ts := time.Now().Format("15:04:05.000")
	fmt.Printf("[%s] "+format+"\n", append([]interface{}{ts}, a...)...)
}

type SpeechEntry struct {
	Text      string
	Timestamp time.Time
}

type SpeechText chan SpeechEntry
type LLMResponse chan string

var isMitsuSpeaking atomic.Bool
var currentLang string
var clearMemoryChan chan struct{}

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

var activeVoiceConfig VoiceConfig

const (
	WhisperModel   = "models/ggml-small.bin"
	OllamaModel    = "mitsu"
	KokoroVoiceAmy = "af_heart"
)

func main() {
	langFlag := flag.String("lang", "en", "Language to use (en or pt)")
	flag.Parse()
	currentLang = *langFlag
	clearMemoryChan = make(chan struct{}, 1)

	// Load voice config from Lab
	loadVoiceConfig()

	tlog("Starting Mitsu in %s mode...", strings.ToUpper(currentLang))

	broker := &Broker{
		clients:        make(map[chan string]bool),
		newClients:     make(chan chan string),
		defunctClients: make(chan chan string),
		messages:       make(chan string),
	}
	go broker.Start()

	speechToBrain := make(SpeechText, 10)
	brainToMouth := make(LLMResponse)
	bargeIn := make(chan struct{}, 1)

	ctx, cancel := context.WithCancel(context.Background())

	go earRoutine(ctx, speechToBrain, broker.messages)
	go brainRoutine(speechToBrain, brainToMouth, broker.messages)
	go mouthRoutine(ctx, bargeIn, brainToMouth)

	go startWebServer(speechToBrain, broker)

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	<-sigChan

	tlog("Shutting down...")
	cancel()
	time.Sleep(1 * time.Second)
}

func loadVoiceConfig() {
	file, err := os.ReadFile("voice_config.json")
	if err != nil {
		tlog("Warning: voice_config.json not found, using defaults.")
		activeVoiceConfig = VoiceConfig{
			VoiceModel: "mitsu_custom", LangCode: "p", Pitch: 1.25, Speed: 1.0, FormantPreserved: true,
			Highpass: 150, Lowpass: 15000, BoxyGain: -15, PresenceGain: 8, SparkleGain: 0,
			ExciterAmount: 3.0, DeesserIntensity: 0.5, StereoWidth: 2.0, LoudnormI: -16,
		}
		return
	}
	json.Unmarshal(file, &activeVoiceConfig)
	tlog("Voice configuration loaded from Lab.")
}

func earRoutine(ctx context.Context, speechToBrain SpeechText, uiMessages chan string) {
	tlog("Ear Routine started.")
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
				if isMitsuSpeaking.Load() {
					status = "SPEAKING..."
				} else if isSpeaking {
					status = "LISTENING"
				}
				msg, _ := json.Marshal(map[string]string{"text": status, "type": "status"})
				select {
				case uiMessages <- string(msg):
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
			if isMitsuSpeaking.Load() {
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

			whisperCmd := exec.CommandContext(ctx, "./whisper-cpp", "-m", WhisperModel, "-f", wavFile, "-nt", "-np", "-nth", "0.1", "-l", currentLang, "-ng")
			out, _ := whisperCmd.CombinedOutput()
			text := strings.TrimSpace(string(out))
			if text != "" && !strings.HasPrefix(text, "[") {
				// FUZZY NAME CORRECTION
				variations := []string{"mitzo", "mitso", "metso", "metsu", "mitsu", "mitzu"}
				lowerText := strings.ToLower(text)
				for _, v := range variations {
					if strings.Contains(lowerText, v) {
						text = strings.ReplaceAll(lowerText, v, "Mitsu")
						break
					}
				}

				tlog("Captured: \"%s\"", text)
				msg, _ := json.Marshal(map[string]string{"text": text, "type": "mic"})
				select {
				case uiMessages <- string(msg):
				default:
				}
				speechToBrain <- SpeechEntry{Text: text, Timestamp: time.Now()}
			}
		}
	}
}

func brainRoutine(speechToBrain SpeechText, brainToMouth LLMResponse, uiMessages chan string) {
	tlog("Brain Routine started.")
	ollamaURL := os.Getenv("OLLAMA_HOST")
	if ollamaURL == "" {
		ollamaURL = "http://localhost:11434"
	}
	history := []ChatMessage{}

	for {
		select {
		case <-clearMemoryChan:
			tlog("Brain: Memory cleared.")
			history = []ChatMessage{}
			continue
		case entry := <-speechToBrain:
			if time.Since(entry.Timestamp) > 5*time.Second {
				tlog("Brain: Skipping expired context (%s)", entry.Text)
				continue
			}

			tlog("Brain processing: \"%s\"", entry.Text)
			msg, _ := json.Marshal(map[string]string{"text": "THINKING...", "type": "status"})
			select {
			case uiMessages <- string(msg):
			default:
			}

			history = append(history, ChatMessage{Role: "user", Content: entry.Text})
			if len(history) > 10 {
				history = history[len(history)-10:]
			}

			modelName := "mitsu-en"
			if currentLang == "pt" {
				modelName = "mitsu-pt"
			}

			reqBody, _ := json.Marshal(OllamaChatRequest{
				Model:    modelName,
				Messages: history,
				Stream:   false,
			})

			resp, err := http.Post(ollamaURL+"/api/chat", "application/json", bytes.NewBuffer(reqBody))
			if err != nil {
				continue
			}
			body, _ := io.ReadAll(resp.Body)
			resp.Body.Close()

			var ollamaResp OllamaChatResponse
			if err := json.Unmarshal(body, &ollamaResp); err != nil {
				continue
			}

			responseText := strings.TrimSpace(ollamaResp.Message.Content)
			tlog("Mitsu: \"%s\"", responseText)
			history = append(history, ChatMessage{Role: "assistant", Content: responseText})

			msg, _ = json.Marshal(map[string]string{"text": responseText, "type": "aura"})
			select {
			case uiMessages <- string(msg):
			default:
			}
			brainToMouth <- responseText
		}
	}
}

func mouthRoutine(ctx context.Context, bargeIn chan struct{}, brainToMouth LLMResponse) {
	tlog("Mouth Routine started.")
	kokoroURL := os.Getenv("KOKORO_HOST")
	if kokoroURL == "" {
		kokoroURL = "http://kokoro:8880"
	}

	for {
		select {
		case sentence := <-brainToMouth:
			if sentence == "" {
				continue
			}
			playCtx, playCancel := context.WithCancel(ctx)
			go func() {
				select {
				case <-bargeIn:
					tlog("Mouth: Interrupted!")
					playCancel()
				case <-playCtx.Done():
				}
			}()

			isMitsuSpeaking.Store(true)

			// Use the dynamically loaded config
			voice := activeVoiceConfig.VoiceModel
			langCode := activeVoiceConfig.LangCode
			
			// Override for English mode if set via CLI
			if currentLang == "en" && voice == "mitsu_custom" {
				voice = KokoroVoiceAmy
				langCode = "a"
			}

			reqBody, _ := json.Marshal(map[string]interface{}{
				"input":     sentence,
				"voice":     voice,
				"lang_code": langCode,
				"speed":     1.0, // We use rubberband for tempo now
				"model":     "kokoro",
			})

			resp, err := http.Post(kokoroURL+"/v1/audio/speech", "application/json", bytes.NewBuffer(reqBody))
			if err != nil {
				tlog("Kokoro Error: %v", err)
				isMitsuSpeaking.Store(false)
				playCancel()
				continue
			}

			// BUILD FILTER CHAIN FROM CONFIG
			f := activeVoiceConfig
			formant_val := "preserved"
			if !f.FormantPreserved { formant_val = "shifted" }
			
			filterChain := fmt.Sprintf(
				"highpass=f=%d,lowpass=f=%d,equalizer=f=400:t=q:w=1:g=%d,equalizer=f=3000:t=q:w=1:g=%d,equalizer=f=8000:t=q:w=1:g=%d,"+
				"rubberband=pitch=%.2f:tempo=%.2f:formant=%s,deesser=i=%.2f,aexciter=amount=%.2f,pan=stereo|c0=c0|c1=c0,extrastereo=m=%.2f,"+
				"loudnorm=I=%d:LRA=11:TP=-1.5",
				f.Highpass, f.Lowpass, f.BoxyGain, f.PresenceGain, f.SparkleGain,
				f.Pitch, f.Speed, formant_val, f.DeesserIntensity, f.ExciterAmount, f.StereoWidth,
				f.LoudnormI,
			)

			ffmpegCmd := exec.CommandContext(playCtx, "ffmpeg", "-i", "pipe:0", "-af", filterChain, "-f", "wav", "pipe:1")
			ffmpegCmd.Stdin = resp.Body
			stdout, _ := ffmpegCmd.StdoutPipe()

			paplayCmd := exec.CommandContext(playCtx, "paplay", "--property=application.name=Mitsu")
			paplayCmd.Stdin = stdout

			tlog("Mouth playing (Studio Active): \"%s\"", sentence)
			if err := ffmpegCmd.Start(); err != nil {
				resp.Body.Close()
				isMitsuSpeaking.Store(false)
				playCancel()
				continue
			}
			if err := paplayCmd.Start(); err != nil {
				resp.Body.Close()
				isMitsuSpeaking.Store(false)
				playCancel()
				continue
			}

			paplayCmd.Wait()
			ffmpegCmd.Process.Kill()
			resp.Body.Close()
			isMitsuSpeaking.Store(false)
			playCancel()
			tlog("Mouth finished.")
		case <-ctx.Done():
			return
		}
	}
}

type ChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type OllamaChatRequest struct {
	Model    string        `json:"model"`
	Messages []ChatMessage `json:"messages"`
	Stream   bool          `json:"stream"`
}

type OllamaChatResponse struct {
	Message ChatMessage `json:"message"`
	Done    bool        `json:"done"`
}

func startWebServer(speechToBrain SpeechText, b *Broker) {
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, `
<!DOCTYPE html>
<html>
<head>
    <title>Mitsu Tactical Interface</title>
    <style>
        body { background: #1a1a1a; color: #00ff00; font-family: monospace; padding: 20px; }
        #header { display: flex; justify-content: space-between; align-items: center; }
        #status { padding: 5px 10px; border-radius: 5px; font-weight: bold; }
        .on { background: #004400; color: #00ff00; }
        .off { background: #440000; color: #ff0000; }
        #terminal { background: #000; border: 1px solid #00ff00; height: 500px; padding: 10px; overflow-y: scroll; margin-bottom: 20px; font-size: 14px; }
        .user { color: #00ff00; }
        .mic { color: #0088ff; font-style: italic; }
        .aura { color: #ff00ff; font-weight: bold; }
        input { background: #000; color: #00ff00; border: 1px solid #00ff00; width: 80%; padding: 12px; }
        button { background: #00ff00; color: #000; border: none; padding: 12px 24px; cursor: pointer; font-weight: bold; }
    </style>
</head>
<body>
    <div id="header">
        <h1>MITSU COMMAND CENTER</h1>
        <div id="status" class="off">IDLE</div>
    </div>
    <div id="terminal"></div>
    <input type="text" id="userInput" placeholder="Type message..." onkeydown="if(event.key==='Enter') send()">
    <button onclick="send()">TRANSMIT</button>
    <script>
        const t = document.getElementById("terminal");
        const s = document.getElementById("status");
        function log(msg, className) {
            const div = document.createElement("div");
            div.className = className;
            let prefix = className === "aura" ? "[MITSU] " : "> ";
            div.textContent = prefix + msg;
            t.appendChild(div);
            t.scrollTop = t.scrollHeight;
        }
        const eventSource = new EventSource("/events");
        eventSource.onmessage = function(e) {
            const data = JSON.parse(e.data);
            if (data.type === "status") {
                s.textContent = data.text;
                s.className = (data.text === "SPEAKING..." || data.text === "LISTENING") ? "on" : "off";
            } else {
                log(data.text, data.type);
            }
        };
        function send() {
            const i = document.getElementById("userInput");
            if (!i.value) return;
            log(i.value, "user");
            fetch("/talk?text=" + encodeURIComponent(i.value));
            i.value = "";
        }
    </script>
</body>
</html>`)
	})

	http.HandleFunc("/talk", func(w http.ResponseWriter, r *http.Request) {
		text := r.URL.Query().Get("text")
		if text != "" {
			speechToBrain <- SpeechEntry{Text: text, Timestamp: time.Now()}
			fmt.Fprint(w, "OK")
		}
	})

	http.HandleFunc("/clear", func(w http.ResponseWriter, r *http.Request) {
		select {
		case clearMemoryChan <- struct{}{}:
			fmt.Fprint(w, "Memory cleared")
		default:
			fmt.Fprint(w, "Already clearing")
		}
	})

	http.HandleFunc("/events", func(w http.ResponseWriter, r *http.Request) {
		f, ok := w.(http.Flusher)
		if !ok {
			return
		}
		messageChan := make(chan string)
		b.newClients <- messageChan
		defer func() { b.defunctClients <- messageChan }()
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		for {
			select {
			case msg := <-messageChan:
				fmt.Fprintf(w, "data: %s\n\n", msg)
				f.Flush()
			case <-r.Context().Done():
				return
			}
		}
	})
	http.ListenAndServe(":8080", nil)
}

type Broker struct {
	clients        map[chan string]bool
	newClients     chan chan string
	defunctClients chan chan string
	messages       chan string
}

func (b *Broker) Start() {
	for {
		select {
		case s := <-b.newClients:
			b.clients[s] = true
		case s := <-b.defunctClients:
			delete(b.clients, s)
			close(s)
		case msg := <-b.messages:
			for s := range b.clients {
				select {
				case s <- msg:
				default:
				}
			}
		}
	}
}
