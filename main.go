package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"mitsu/pkg/brain"
	"mitsu/pkg/common"
	"mitsu/pkg/ear"
	"mitsu/pkg/gaming"
	"mitsu/pkg/mouth"
	"net/http"
	"os"
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

var isMitsuSpeaking atomic.Bool
var currentLang string
var earLangChan chan string
var brainLangChan chan string
var mouthLangChan chan string
var clearMemoryChan chan struct{}
var activeVoiceConfig mouth.VoiceConfig
var gameController *gaming.GameController

const (
	KokoroVoiceAmy = "af_heart"
)

func main() {
	langFlag := flag.String("lang", "en", "Language to use (en or pt)")
	flag.Parse()
	currentLang = *langFlag
	earLangChan = make(chan string, 1)
	brainLangChan = make(chan string, 1)
	mouthLangChan = make(chan string, 1)
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

	speechToBrain := make(common.SpeechText, 10)
	brainToMouth := make(common.LLMResponse)
	bargeIn := make(chan struct{}, 1)

	ctx, cancel := context.WithCancel(context.Background())

	// Initialize components
	gameController = gaming.NewGameController("localhost:8888")
	go gameController.Start(ctx)

	sttURL := os.Getenv("STT_HOST")
	if sttURL == "" {
		sttURL = "http://localhost:5001"
	}
	mitsuEar := &ear.Ear{
		STTURL:             sttURL,
		CurrentLang:        currentLang,
		LanguageChangeChan: earLangChan,
		IsMitsuSpeaking:    &isMitsuSpeaking,
		SpeechToBrain:      speechToBrain,
		UiMessages:         broker.messages,
		InputDevice:        "", // Use default (let qpwgraph handle it)
		TestInput:          os.Getenv("TEST_INPUT_FILE"),
	}

	ollamaURL := os.Getenv("OLLAMA_HOST")
	if ollamaURL == "" {
		ollamaURL = "http://localhost:11434"
	}
	mitsuBrain := &brain.Brain{
		OllamaURL:          ollamaURL,
		CurrentLang:        currentLang,
		LanguageChangeChan: brainLangChan,
		SpeechToBrain:      speechToBrain,
		BrainToMouth:       brainToMouth,
		UiMessages:         broker.messages,
		ClearMemoryChan:    clearMemoryChan,
	}

	kokoroURL := os.Getenv("KOKORO_HOST")
	if kokoroURL == "" {
		kokoroURL = "http://kokoro:8880"
	}
	mitsuMouth := &mouth.Mouth{
		KokoroURL:          kokoroURL,
		CurrentLang:        currentLang,
		LanguageChangeChan: mouthLangChan,
		ActiveConfig:       activeVoiceConfig,
		IsMitsuSpeaking:    &isMitsuSpeaking,
		BrainToMouth:       brainToMouth,
		BargeIn:            bargeIn,
		KokoroVoiceAmy:     KokoroVoiceAmy,
		OutputDevice:       "", // Use default (let qpwgraph handle it)
		TestOutput:         os.Getenv("TEST_OUTPUT_FILE"),
	}

	go mitsuEar.Start(ctx)
	go mitsuBrain.Start()
	go mitsuMouth.Start(ctx)

	// Background WarmUp check with vocal alert
	go func() {
		time.Sleep(3 * time.Second) // Wait for Kokoro/Ollama services to be up
		
		err := mitsuBrain.WarmUp(func() {
			thinkingMsg := "Hmmm, let me think about that."
			if currentLang == "pt" {
				thinkingMsg = "Hmmm, deixa eu ver."
			}
			tlog("Brain: Models not in GPU, playing alert and loading...")
			mitsuMouth.Alert(thinkingMsg, currentLang)
		})

		if err != nil {
			tlog("Brain WarmUp error: %v", err)
		} else {
			tlog("Brain is ready.")
		}
	}()

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
		activeVoiceConfig = mouth.VoiceConfig{
			VoiceModel: "mitsu_custom", LangCode: "p", Pitch: 1.25, Speed: 1.0, FormantPreserved: true,
			Highpass: 150, Lowpass: 15000, BoxyGain: -15, PresenceGain: 8, SparkleGain: 0,
			ExciterAmount: 3.0, DeesserIntensity: 0.5, StereoWidth: 2.0, LoudnormI: -16,
		}
		return
	}
	json.Unmarshal(file, &activeVoiceConfig)
	tlog("Voice configuration loaded from Lab.")
}

func startWebServer(speechToBrain common.SpeechText, b *Broker) {
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
        .game-btn { background: #ffaa00; margin-left: 10px; }
    </style>
</head>
<body>
    <div id="header">
        <h1>MITSU COMMAND CENTER</h1>
        <div id="status" class="off">IDLE</div>
        <div>
            <button onclick="hotswap('en')" style="background: #0088ff;">EN</button>
            <button onclick="hotswap('pt')" style="background: #00cc00;">PT</button>
        </div>
        <button onclick="toggleGaming()" class="game-btn" id="gameBtn">GAMER MODE: OFF</button>
    </div>
    <div id="terminal"></div>
    <input type="text" id="userInput" placeholder="Type message..." onkeydown="if(event.key==='Enter') send()">
    <button onclick="send()">TRANSMIT</button>
    <script>
        const t = document.getElementById("terminal");
        const s = document.getElementById("status");
        const gb = document.getElementById("gameBtn");
        function log(msg, className) {
            const div = document.createElement("div");
            div.className = className;
            let prefix = className === "aura" ? "[MITSU] " : "> ";
            div.textContent = prefix + msg;
            t.appendChild(div);
            t.scrollTop = t.scrollHeight;
        }
        function hotswap(lang) {
            fetch("/hotswap?lang=" + lang);
        }
        const eventSource = new EventSource("/events");
        eventSource.onmessage = function(e) {
            const data = JSON.parse(e.data);
            if (data.type === "status") {
                s.textContent = data.text;
                s.className = (data.text === "SPEAKING..." || data.text === "LISTENING") ? "on" : "off";
            } else if (data.type === "gaming") {
                gb.textContent = "GAMER MODE: " + data.status;
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
        function toggleGaming() {
            fetch("/gaming/toggle");
        }
    </script>
</body>
</html>`)
	})

	http.HandleFunc("/talk", func(w http.ResponseWriter, r *http.Request) {
		text := r.URL.Query().Get("text")
		if text != "" {
			speechToBrain <- common.SpeechEntry{
				Text:      text,
				Language:  currentLang, // Use the current hotswapped language
				Timestamp: time.Now(),
				Profile:   common.NewProfile(),
			}
			fmt.Fprint(w, "OK")
		}
	})

	http.HandleFunc("/hotswap", func(w http.ResponseWriter, r *http.Request) {
		lang := r.URL.Query().Get("lang")
		if lang == "en" || lang == "pt" {
			currentLang = lang
			// Broadcast to all specialized channels
			select { case earLangChan <- lang: default: }
			select { case brainLangChan <- lang: default: }
			select { case mouthLangChan <- lang: default: }

			msg, _ := json.Marshal(map[string]string{"text": "HOTSWAP: Switched to " + lang, "type": "status"})
			b.messages <- string(msg)
			fmt.Fprintf(w, "Switched to %s", lang)
			tlog("Language hotswapped to %s", lang)
		} else {
			http.Error(w, "Invalid language", 400)
		}
	})

	http.HandleFunc("/gaming/toggle", func(w http.ResponseWriter, r *http.Request) {
		enabled := gameController.Enabled.Load()
		gameController.Enabled.Store(!enabled)
		status := "OFF"
		if !enabled {
			status = "ON"
		}
		// Notify via broker (hacky but works for now)
		msg, _ := json.Marshal(map[string]string{"status": status, "type": "gaming"})
		b.messages <- string(msg)
		fmt.Fprint(w, "OK")
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
