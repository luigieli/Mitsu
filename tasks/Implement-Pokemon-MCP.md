# Task: Implement Pokémon Gaming MCP (Mitsu Control Protocol)

## Objective
Enable Mitsu to play Pokémon FireRed via mGBA using memory reading ("Telepathy") while maintaining conversational awareness.

## Action Plan

### 1. Emulator Environment Setup
- [ ] Install dependencies on host: `sudo pacman -S mgba-qt lua lua-socket`.
- [ ] Create `scripts/pokemon_bridge.lua` to:
    - Bind to a TCP socket (port 8888).
    - Read RAM addresses for HP, Level, Enemy Name, and Player Coordinates.
    - Execute button presses (A, B, UP, DOWN, etc.) received from Go.
- [ ] Document memory addresses for Pokémon FireRed (US).

### 2. Gaming Package (`pkg/gaming`) - Modular Implementation
- [ ] Create `pkg/gaming/controller.go`:
    - TCP client to connect to mGBA.
    - State machine to track "Overworld" vs "Battle" modes.
    - "System 1" (Autopilot): Hardcoded logic for grinding and simple navigation.
- [ ] Implement **Modular Control**:
    - Add `Enabled` atomic boolean to start/stop the gaming loop without application restart.
    - Implement a `Status()` method to report gaming health to the UI.
- [ ] Implement "Type Chart" in Go for instant super-effective move selection without GPU.

### 3. Unified Brain Multitasking (`pkg/brain`)
- [ ] Update `Brain` struct to merge `common.SpeechEntry` and `gaming.GameState`.
- [ ] Implement Structured JSON Output mode:
    ```json
    {
      "thought": "Internal reasoning",
      "strategy": "MANUAL | AUTO_GRIND",
      "move": "A | B | UP | DOWN | WAIT",
      "say": "Optional voice reply"
    }
    ```
- [ ] **Context Injection**: Brain should only process game state if `GamerMode` is active.

### 4. Dynamic Control Interface (UI & Voice)
- [ ] **Web Interface**: Add a "Toggle Gamer Mode" button to the Mitsu Command Center.
- [ ] **Voice Command**: Implement intent detection for "Mitsu, let's play Pokémon" or "Stop playing."
- [ ] **REST API**: Add `/gaming/toggle` endpoint to the Go web server.

### 5. Navigation & Movement Thread
- [ ] Implement the "Navigator (AI) vs Driver (Go)" architecture.
- [ ] Go Logic: "Walk until blocked" (detect coordinate stalls).
- [ ] AI Logic: High-level travel orders ("Go North to Viridian City").

### 6. Integration & Performance
- [ ] Set `OLLAMA_KEEP_ALIVE=-1` in `docker-compose.yml` to prevent VRAM unloading.
- [ ] Orchestrate the `gaming` loop in `main.go`.

## Success Criteria
- [ ] Mitsu can win a battle against a wild Pokémon autonomously.
- [ ] Gamer Mode can be enabled/disabled instantly via the web UI.
- [ ] Mitsu's conversation logic shifts from general chat to "Competitive Player" persona when gaming is active.
- [ ] Latency for game moves remains under 500ms when not chatting.
