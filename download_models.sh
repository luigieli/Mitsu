#!/bin/bash
mkdir -p models

echo "Downloading Whisper SMALL model (Quantized q5_1)..."
curl -L https://huggingface.co/ggerganov/whisper.cpp/resolve/main/ggml-small-q5_1.bin -o models/ggml-small-q5_1.bin

echo "Downloading Kokoro Voice Vectors for Anime Blending..."
# Standard paths for Kokoro FastAPI container
# Using URLs from Hugging Face if they were available directly, but usually these are in the image.
# However, to be safe and ensure we have them for the script:
VOICE_URL="https://huggingface.co/hexgrad/Kokoro-82M/resolve/main/voices"
curl -L "$VOICE_URL/af_heart.pt" -o models/af_heart.pt
curl -L "$VOICE_URL/pf_dora.pt" -o models/pf_dora.pt
curl -L "$VOICE_URL/jf_alpha.pt" -o models/jf_alpha.pt

echo "Done!"
