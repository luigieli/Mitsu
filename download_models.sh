#!/bin/bash
mkdir -p models

echo "Downloading Whisper SMALL model..."
curl -L https://huggingface.co/ggerganov/whisper.cpp/resolve/main/ggml-small.bin -o models/ggml-small.bin

echo "Done!"
