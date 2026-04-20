#!/bin/bash
set -euo pipefail

# Fail Fast: Check dependencies
for cmd in wget tar; do
    if ! command -v "$cmd" &> /dev/null; then
        echo "Error: $cmd is not installed."
        exit 1
    fi
done

MODEL_DIR="models"
mkdir -p "$MODEL_DIR"

echo "Downloading models to $MODEL_DIR..."

# List of models to download
# Note: In a production script, we'd check if files already exist to skip.
# For now, we optimize for readability and linear flow.

function download_and_extract() {
    local url=$1
    local output=$2
    if [ ! -d "$output" ]; then
        echo "Fetching $output..."
        wget -q "$url" -O "tmp.tar.gz"
        tar -xzf "tmp.tar.gz" -C "$MODEL_DIR"
        rm "tmp.tar.gz"
    else
        echo "Model $output already exists, skipping."
    fi
}

# Example placeholders - real URLs needed for actual download
# download_and_extract "http://example.com/model.tar.gz" "model_name"

echo "Setup complete."
