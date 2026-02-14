# Stage 1: Build the Go Companion
FROM golang:1.24-bookworm AS builder
RUN apt-get update && apt-get install -y --no-install-recommends gcc libc6-dev && rm -rf /var/lib/apt/lists/*
WORKDIR /app
# Cache Go modules
COPY go.mod go.sum ./
RUN go mod download
# Copy only source needed for build
COPY main.go ./
COPY pkg/ ./pkg/
RUN CGO_ENABLED=1 go build -o companion main.go

# Stage 2: Runtime
FROM debian:bookworm-slim
WORKDIR /app

# Pre-install system packages (cached layer)
RUN apt-get update && apt-get install -y --no-install-recommends \
    ffmpeg alsa-utils pulseaudio-utils libstdc++6 curl ca-certificates espeak-ng-data \
    libvulkan1 mesa-vulkan-drivers mesa-utils \
    && rm -rf /var/lib/apt/lists/*

RUN echo 'pcm.!default { type pulse }' > /etc/asound.conf && \
    echo 'ctl.!default { type pulse }' >> /etc/asound.conf

# Copy binary from builder
COPY --from=builder /app/companion .

CMD ["./companion"]
