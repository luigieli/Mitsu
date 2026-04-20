#!/bin/bash
set -e

mkdir -p models
cd models

MODEL_NAME="sherpa-onnx-streaming-zipformer-en-2023-06-26"
URL="https://github.com/k2-fsa/sherpa-onnx/releases/download/asr-models/${MODEL_NAME}.tar.bz2"

if [ ! -d "$MODEL_NAME" ]; then
    echo "Downloading Sherpa-ONNX model: $MODEL_NAME..."
    wget -q "$URL"
    tar xjf "${MODEL_NAME}.tar.bz2"
    rm "${MODEL_NAME}.tar.bz2"
    echo "Model downloaded successfully."
else
    echo "Model $MODEL_NAME already exists, skipping download."
fi
