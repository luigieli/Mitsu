# Mitsu Project TODO

- [ ] **Simultaneous Speech Timeout**: Implement logic to handle cases where both the user and Mitsu speak at the same time (barge-in / interruption handling).
- [x] **Whisper.cpp Optimization**: Research and implement ways to reduce transcription latency (e.g., using different quantization, tiny models, or further Vulkan tuning).
- [ ] **Performance Profiling**: Add detailed timing logs for each pipeline step (Ear -> Whisper -> Brain -> Kokoro -> Mouth) to identify and eliminate bottlenecks.
- [ ] **Context Awareness**: Implement a history buffer to inject context into prompts.
- [ ] **Response Triggering**: Use a low-cost embedding model to trigger Mitsu's responses.
- [ ] **Multitasking Brain**: Enable the brain to handle both playing and answering simultaneously.
- [ ] **Brain Architecture**: Implement two brain parts for autopilot gaming or manual play.
- [ ] **Movement Thread**: Add a dedicated movement thread for the brain to decide actions.
- [ ] **VAD Implementation**: Implement Voice Activity Detection (VAD) for the ear.
- [x] **Whisper Optimization**: Optimized Whisper thread usage and switched to `ggml-small-q5_1.bin` with Vulkan offloading.
- [ ] **Dual-Channel Ears**: Implement two-channel audio input (microphone and Discord).
- [ ] **Bilingual Support**: Implement language verification for a bilingual AI.
- [ ] **Voice Blending**: Experiment further with voice blending techniques.
- [ ] **Kokoro Verification**: Verify if voices are correctly loaded into Kokoro.
- [ ] **MCPS Integration**: Investigate potential usage of Minecraft Protocol Support (MCPS).
- [x] **Testing Suite**: Implemented unit tests for packages and E2E tests using Testcontainers.
