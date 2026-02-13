#!/bin/bash
mkdir -p models

echo "Downloading Whisper SMALL model (Quantized q5_1)..."
curl -L https://huggingface.co/ggerganov/whisper.cpp/resolve/main/ggml-small-q5_1.bin -o models/ggml-small-q5_1.bin

echo "Done!"
