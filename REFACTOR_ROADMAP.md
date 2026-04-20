# Mitsu: Cognitive Refactoring Roadmap

This document outlines the structural changes required to reduce **Human Cognitive Load** and improve **Developer Experience (DevEx)** by addressing areas of high mental friction, fragmented state, and "God" function patterns.

---

## 🧠 Core Cognitive Friction Points

### 1. Fragmented Language State (Hotswap)
- **Problem:** Language state is synchronized via three redundant channels (`earLangChan`, `brainLangChan`, `mouthLangChan`) and manual updates in `main.go`.
- **Cognitive Impact:** High risk of "state drift." A developer must mentally track four different files to understand a single hotswap operation.

### 2. Orchestration "God" Module (`main.go`)
- **Problem:** `main.go` handles initialization, service orchestration, low-level HTTP routing, and contains a 70-line embedded HTML/JS/CSS string.
- **Cognitive Impact:** Visual noise and "wall of text" syndrome. It's difficult to see the high-level system architecture through the low-level implementation details.

### 3. The "Arrow" Audio Pipeline (`ear.go`)
- **Problem:** The `Start` method in `ear.go` is a deeply nested loop (for > select > for > if) handling ffmpeg IO, VAD processing, and WebSocket handshaking.
- **Cognitive Impact:** Breaks visual tracking. Fixing a bug in "audio capture" requires parsing the logic for "websocket results."

### 4. Interleaved LLM Logic (`brain.go`)
- **Problem:** Token-to-sentence splitting is interleaved with Ollama HTTP chunk reading and manual buffer management.
- **Cognitive Impact:** High working memory overhead. The developer must filter out "network boilerplate" to understand "sentence processing."

### 5. Implicit State Assumptions (Generic)
- **Problem:** Many functions assume valid network responses or non-nil pointers, leading to deep "Happy Path" nesting.
- **Cognitive Impact:** Increases mental debt. The developer must simulate error states late in the function's execution rather than handling them at the entry point.

---

## 🛠 Refactoring Blueprints

### 1. Orchestration Layer (`main.go`)
**Goal:** Pure initialization and component wiring. Move UI and business logic to dedicated managers.

```go
func main() {
	config := parseFlags()
	app := NewMitsuApp(config)
	
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	app.Initialize(ctx)
	app.Run(ctx)

	waitForShutdown(cancel)
}

type MitsuApp struct {
	Lang        *common.LanguageState
	Ear         *ear.Ear
	Brain       *brain.Brain
	Mouth       *mouth.Mouth
	Game        *gaming.GameController
	UI          *UIManager
}

func (a *MitsuApp) Initialize(ctx context.Context) {
	speechToBrain := make(common.SpeechText, 10)
	brainToMouth := make(common.LLMResponse)
	uiMessages := make(chan string, 100)

	a.Lang = common.NewLanguageState(os.Getenv("DEFAULT_LANG"))
	
	a.Ear = &ear.Ear{
		Lang:          a.Lang,
		SpeechToBrain: speechToBrain,
		UiMessages:    uiMessages,
	}
    // ... further initialization
}
```

### 2. Audio Pipeline Decomposition (`pkg/ear/ear.go`)
**Goal:** Remove deep nesting. Use named functions for logical stages of audio processing.

```go
func (e *Ear) Start(ctx context.Context) {
	tlog("Ear: Starting Hybrid Audio Pipeline")
	
	go e.listenForLanguageChanges(ctx)
	go e.streamingTranscriptionLoop(ctx)
	
	e.captureAndProcessAudio(ctx)
}

func (e *Ear) captureAndProcessAudio(ctx context.Context) {
	vad := initVAD(VAD_MODE_LEAST_AGGRESSIVE)
	
	for {
		if ctx.Err() != nil { return }
		
		audioStream := e.openAudioDevice(ctx)
		e.processStream(audioStream, vad)
		
		audioStream.Close()
		time.Sleep(RECOVERY_DELAY)
	}
}

func (e *Ear) processStream(stream io.Reader, vad *webrtcvad.VAD) {
	buffer := make([]byte, BYTE_SIZE_30MS)
	session := &audioSession{isSpeaking: false}

	for {
		if _, err := io.ReadFull(stream, buffer); err != nil { break }
		if e.Lang.IsMitsuSpeaking() { continue }

		if vad.IsSpeech(buffer) {
			e.handleSpeechDetected(session, buffer)
		} else if session.isSpeaking {
			e.handleSilenceDetected(session, buffer)
		}
	}
}
```

### 3. Stream-Based Token Processing (`pkg/brain/brain.go`)
**Goal:** Decouple IO from NLP logic. Use channels to pipe tokens into a sentence splitter.

```go
func (b *Brain) processUserRequest(entry common.SpeechEntry) {
	b.notifyUI("THINKING...")
	
	responseStream := b.ollama.StreamChat(b.getModel(entry.Language), b.history)
	sentenceStream := b.splitIntoSentences(responseStream)

	var fullResponse strings.Builder
	for sentence := range sentenceStream {
		fullResponse.WriteString(sentence)
		b.BrainToMouth <- b.createMouthEntry(sentence, entry)
	}

	b.updateHistory(entry.Text, fullResponse.String())
	b.notifyUI(fullResponse.String())
}

func (b *Brain) splitIntoSentences(tokens <-chan string) <-chan string {
	out := make(chan string)
	go func() {
		defer close(out)
		var buffer strings.Builder
		
		for token := range tokens {
			buffer.WriteString(token)
			if isEndOfSentence(token) {
				out <- strings.TrimSpace(buffer.String())
				buffer.Reset()
			}
		}
		
		if final := strings.TrimSpace(buffer.String()); final != "" {
			out <- final
		}
	}()
	return out
}

### 4. Fail-Fast Implementation (Example: `brain.go`)
**Goal:** Guard the entry point. Ensure invalid states stop execution immediately, keeping the "Happy Path" at the lowest indentation.

```go
func (b *Brain) streamResponse(resp *http.Response, entry common.SpeechEntry) {
	// Fail Fast: Immediate exit if upstream failed
	if resp.StatusCode != http.StatusOK {
		b.handleUpstreamError(resp)
		return
	}

	tokens := b.consumeOllamaStream(resp, entry)
	sentences := b.pipeTokensToSentences(tokens)
    
    // Main logic remains linear and clean
	for sentence := range sentences {
		b.dispatchSentence(sentence, entry, false)
	}
}
```
```

---

## 🛠 Refactoring Architecture

### Phase 1: Centralized State Management
- **Target:** Create `pkg/common/state.go`.
- **Change:** Introduce a `LanguageState` struct that provides a single source of truth.
- **Goal:** Replace triple-channel broadcasting with a single subscription or shared pointer.

### Phase 2: Orchestration & UI Separation
- **Target:** `main.go` and new `pkg/ui/manager.go`.
- **Change:** 
    - Move HTML/JS/CSS to external files.
    - Transform `main.go` into a thin "Bootstrapper".

### Phase 3: Component Decomposition
- **Target:** `pkg/ear/ear.go` and `pkg/brain/brain.go`.
- **Change:** Linear, top-to-bottom readability. One function, one responsibility.

### Phase 4: Cross-Cutting Fail-Fast Guards
- **Target:** All service handlers and IO loops.
- **Change:** Implement guard clauses at the top of functions.
- **Goal:** Clear "Mental Stack" early; reduce indentation.

### Phase 5: Encapsulate State Machines & Buffers
- **Target:** `pkg/brain/brain.go` and `pkg/ear/ear.go`.
- **Change:** Move manual string building and byte-slicing into dedicated structs or pure functions.
- **Goal:** Remove "Low-Level Noise" from main orchestration loops.

### Phase 6: Clean Data Transformation & Asset Separation
- **Target:** `pkg/mcp/manager.go` and `pkg/ui/manager.go`.
- **Change:** Extract type assertions into `stringifyContent` and use `go:embed` for HTML.
- **Goal:** Improve visual tracking and separation of concerns.

### Phase 7: Python & Lua Logic Hardening
- **Target:** `stt_server.py`, `stt_streaming.py`, `scripts/pokemon_bridge.lua`.
- **Change:** 
    - Decompose "God Loops" into specialized handlers.
    - Remove redundant code paths in STT model selection.
    - Extract button mapping from the Lua bridge loop.
- **Goal:** Reduce mental simulation cost for external scripts.

### Phase 8: Fail-Fast Shell & Build scripts
- **Target:** `download_models.sh`, `Makefile`.
- **Change:** Use `set -e` and explicit checks for dependencies.
- **Goal:** Prevent silent failures during environment setup.

---

## 📋 Execution Checklist

- [x] **Common:** Implement `LanguageState` and `Profile` helpers.
- [x] **UI:** Extract web server logic and static assets from `main.go`.
- [x] **Ear:** Refactor the audio loop into a "Session-based" pipeline.
- [x] **Brain:** Implement a clean `SentenceStream` for token processing.
- [x] **Mouth:** Decouple ffmpeg filter building from the playback loop.
- [x] **Main:** Cleanup `main.go` to be a pure orchestration layer.
- [x] **Global:** Implement Fail-Fast guards across all IO entry points.
- [x] **Brain/Ear:** Encapsulate State Machines & Buffers.
- [x] **UI/MCP:** Clean Data Transformation & Asset Separation.
- [x] **Python/Lua:** Logic Hardening and Loop Decomposition.
- [x] **Shell:** Fail-Fast scripts with dependency checks.
- [x] **Code Review Remediation:** Fixed memory leaks, race conditions, resource management, and implemented graceful shutdowns.

---

## 📏 Success Metrics
1. **Visual Depth:** No function exceeds 3 levels of nesting.
2. **Context Independence:** A developer can fix an STT bug in `ear.go` without looking at `main.go` or `brain.go`.
3. **Implicit Documentation:** Code reads like high-level instructions rather than mechanical steps.
