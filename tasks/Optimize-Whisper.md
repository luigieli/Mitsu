# Task: Optimize Whisper.cpp for Real-time Bilingual Performance

## Objective
Reduce transcription latency for the `small` Whisper model on an RX 580 (Polaris) to under 800ms while maintaining Portuguese and English accuracy.

## Prerequisites
- [ ] Install `vulkan-radeon` and `glslc` (shader compiler) for Vulkan support.
- [ ] Identify location of `whisper.cpp` source if integrated or external.

## Action Plan

### 1. Model Quantization (q5_1)
- [ ] Download the optimized 5-bit quantized model.
  ```bash
  # Assuming script location
  ./models/download-ggml-model.sh small-q5_1
  ```
- [ ] Update `voice_config.json` or initialization logic to use `ggml-small-q5_1.bin`.

### 2. Vulkan Acceleration
- [ ] Rebuild `whisper.cpp` with Vulkan support enabled.
  ```bash
  rm -rf build
  cmake -B build -DGGML_VULKAN=1
  cmake --build build -j --config Release
  ```
- [ ] Ensure `GGML_VULKAN=1` is passed during the build process in the `Makefile` or CI/CD.

### 3. Latency Reduction (Greedy Search)
- [ ] Set `beam_size` to `1` in the transcription parameters.
  - **Flag**: `--beam_size 1` or `-bs 1`.
- [ ] Verify impact on accuracy vs. speed (Expected: 80% workload reduction).

### 4. Language Detection Optimization
- [ ] Implement logic to skip "Auto-Detect Language" latency.
- [ ] **Option A (Go Logic)**: Force language via flags (`--language pt` or `--language en`) based on application state or toggle.
- [ ] **Option B (Hotkeys)**: Implement keybindings for instant language forcing.

### 5. Integration & Command Tuning
- [ ] Configure the final execution command/parameters:
  ```bash
  ./main 
    -m models/ggml-small-q5_1.bin 
    --language auto 
    --beam_size 1 
    --threads 4 
    --gpu-layers 100
  ```

## Success Criteria
- [ ] Transcription latency < 800ms on RX 580.
- [ ] High accuracy for both Portuguese and English commands.
- [ ] Full GPU offload confirmed via Vulkan.

## Notes
- If `small-q5_1` is still too slow, evaluate `ggml-base.bin` (standard, not .en) as a fallback.
