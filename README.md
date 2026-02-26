# Mitsu: An AI Assistant

Mitsu is an interactive AI assistant inspired by [Neuro-Sama](https://www.youtube.com/@Neurosama). She features a low-latency voice-to-voice pipeline and bilingual support (English and Portuguese).

## 🤖 Core Architecture

Mitsu is built as a modular Go-based "Central Nervous System" (CNS) that orchestrates several specialized microservices:

*   **Ear (`pkg/ear`)**: Handles real-time audio capture from PulseAudio. It uses `webrtcvad` for speech detection and sends audio chunks to the STT server.
*   **Brain (`pkg/brain`)**: The reasoning engine. It communicates with **Ollama** to generate responses. It features sentence-based streaming to allow the "Mouth" to start speaking before the LLM has finished its entire thought.
*   **Mouth (`pkg/mouth`)**: Converts text to speech using the **Kokoro TTS** engine. It implements a parallel streaming pipeline where audio is filtered through FFmpeg and piped directly to PulseAudio for playback.

## 🛠️ Tech Stack

*   **Language**: Go 1.24 (Backend), Python (STT Server)
*   **LLM**: [Ollama](https://ollama.com/)
*   **STT**: [Faster-Whisper](https://github.com/SYSTRAN/faster-whisper)
*   **TTS**: [Kokoro](https://github.com/hexgrad/kokoro)
*   **Audio**: PulseAudio, FFmpeg, WebRTC VAD
*   **Containerization**: Docker Compose

## 🚀 Getting Started

### Prerequisites

*   **Linux** with PulseAudio/PipeWire installed.
*   **Docker** and **Docker Compose**.
*   (Optional) **Vulkan** drivers for GPU acceleration in Ollama.

### Setup

1.  **Clone the repository**:
    ```bash
    git clone https://github.com/your-repo/mitsu.git
    cd mitsu
    ```

2.  **Download Models**:
    Ensure you have the necessary models for Ollama and Whisper.
    ```bash
    ./download_models.sh
    ```

3.  **Configure PulseAudio**:
    Mitsu needs to access your host's PulseAudio socket. Ensure the `XDG_RUNTIME_DIR` and Pulse cookie paths in `docker-compose.yml` match your system.

4.  **Launch Mitsu**:
    ```bash
    # For English mode (default)
    docker-compose up --build

    # For Portuguese mode
    MITSU_LANG=pt docker-compose up --build
    ```

## 🌍 Bilingual Support

Mitsu supports both English and Portuguese, but the language must be manually selected at startup using the `MITSU_LANG` environment variable. She does not currently support automatic language switching during a session.

## 🚧 Work in Progress: Gaming Integration

Mitsu includes a bridge for **mGBA** integration, intended to allow her to interact with games like Pokémon. This feature is currently under active development and is **not yet functional**.

*   **Gaming Bridge (`pkg/gaming`)**: A TCP bridge for communicating with emulators.
*   **mGBA Script (`scripts/pokemon_bridge.lua`)**: A Lua script intended to read game state and execute commands.
