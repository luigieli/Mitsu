# Mitsu Gaming Module (Pokemon MCP)

This module enables Mitsu to interact with Game Boy Advance games via the mGBA emulator.

## Architecture

1.  **Lua Bridge**: A server-side script (`scripts/pokemon_bridge.lua`) running inside the emulator. It reads the game's RAM and executes button inputs sent via TCP.
2.  **Go Controller**: A modular package (`pkg/gaming`) that connects to the bridge and manages the game loop.
3.  **Unified Brain**: The LLM processes both chat history and current game state to decide on moves and commentary simultaneously.

## Modular Control

Mitsu's gaming capability is **opt-in**. It does not require a full system restart to enable or disable.

### How to Toggle Gamer Mode:
- **Web UI**: Use the "Toggle Gamer Mode" button in the Command Center.
- **Voice**: Say "Mitsu, let's play Pokemon" or "Mitsu, stop playing."
- **API**: Send a request to `GET /gaming/toggle`.

## Configuration

- **Default Port**: 8888 (TCP)
- **Supported Games**: Pokémon FireRed (US) (Primary)
- **Manual Routing**: Use `qpwgraph` to route game audio to `Mitsu_Input` if you want her to hear the game music/SFX.

## Logic Layers

- **System 1 (Autopilot)**: Immediate, CPU-bound logic for basic grinding and mashing through simple menus.
- **System 2 (Brain)**: Strategic, GPU-bound logic (Llama 3.2) for tough battles and contextual trash talk.
