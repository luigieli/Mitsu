# Stage 1: Build Whisper.cpp with VULKAN support
FROM ubuntu:22.04 AS whisper-builder
ENV DEBIAN_FRONTEND=noninteractive
RUN apt-get update && apt-get install -y --no-install-recommends \
    gcc g++ make git cmake ca-certificates curl gnupg2 && \
    curl -fsSL https://packages.lunarg.com/lunarg-signing-key-pub.asc | gpg --dearmor -o /usr/share/keyrings/lunarg-vulkan.gpg && \
    # Using the main stable repo URL
    echo "deb [signed-by=/usr/share/keyrings/lunarg-vulkan.gpg] https://packages.lunarg.com/vulkan jammy main" > /etc/apt/sources.list.d/lunarg-vulkan.list && \
    apt-get update && apt-get install -y --no-install-recommends vulkan-sdk && \
    rm -rf /var/lib/apt/lists/*

WORKDIR /build
RUN git clone https://github.com/ggerganov/whisper.cpp.git && \
    cd whisper.cpp && \
    cmake -B build -DGGML_VULKAN=1 -DBUILD_SHARED_LIBS=OFF && \
    cmake --build build --config Release --target whisper-cli && \
    cp build/bin/whisper-cli /whisper-cpp

# Stage 2: Build the Go Companion
FROM golang:1.24-bookworm AS builder
RUN apt-get update && apt-get install -y --no-install-recommends gcc libc6-dev && rm -rf /var/lib/apt/lists/*
WORKDIR /app
COPY go.mod* go.sum* ./
RUN if [ -f go.mod ]; then go mod download; fi
COPY . .
RUN CGO_ENABLED=1 go build -o companion main.go

# Stage 3: Runtime
FROM debian:bookworm-slim
WORKDIR /app
RUN apt-get update && apt-get install -y --no-install-recommends \
    ffmpeg alsa-utils pulseaudio-utils libstdc++6 curl ca-certificates espeak-ng-data \
    libvulkan1 mesa-vulkan-drivers mesa-utils \
    && rm -rf /var/lib/apt/lists/*

RUN echo 'pcm.!default { type pulse }' > /etc/asound.conf && \
    echo 'ctl.!default { type pulse }' >> /etc/asound.conf

# Copy binaries
COPY --from=whisper-builder /whisper-cpp .
COPY --from=builder /app/companion .

# Copy models
COPY models/ ./models/

CMD ["./companion"]
