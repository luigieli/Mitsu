.PHONY: build up down mic-setup clean run run-pt run-en setup-model monitor clear-memory help test

UID := $(shell id -u)
GID := $(shell id -g)
XDG_RUNTIME_DIR := $(shell echo $$XDG_RUNTIME_DIR)
HOME := $(shell echo $$HOME)

# Default language
MITSU_LANG ?= en

export UID
export GID
export XDG_RUNTIME_DIR
export HOME
export MITSU_LANG

build:
	@echo "Building Docker images..."
	docker-compose build

up:
	@echo "Starting services [LANG=$(MITSU_LANG)]..."
	docker-compose up -d
	@echo "Generating custom voice blend..."
	@sleep 5
	@docker-compose cp blend_mitsu.py kokoro:/app/blend_mitsu.py
	@docker-compose exec -T kokoro python3 /app/blend_mitsu.py

down:
	@echo "Stopping services..."
	docker-compose down

run-pt:
	@$(MAKE) run MITSU_LANG=pt

run-en:
	@$(MAKE) run MITSU_LANG=en

clear-memory:
	@echo "Clearing Mitsu's memory..."
	@curl -s http://localhost:8080/clear > /dev/null
	@echo "Memory cleared."

mic-setup:
	@echo "Checking for denoised mic..."
	@pactl list sources short | grep -q "denoised_mic" || echo "WARNING: Denoised mic not found. Ensure PipeWire config is loaded."

clean:
	@echo "Cleaning up..."
	docker-compose down --volumes --rmi all
	@echo "Removing ollama_data volume..."
	rm -rf ./ollama_data
	@echo "Removing temporary build artifacts..."
	docker rmi $$(docker images -aq -f "dangling=true") 2>/dev/null || true

run: mic-setup build up setup-model

setup-model:
	@echo "Creating Mitsu personas in Ollama..."
	docker-compose exec -T ollama ollama pull llama3.2:3b || true
	docker-compose cp Modelfile.en ollama:/Modelfile.en
	docker-compose cp Modelfile.pt ollama:/Modelfile.pt
	docker-compose exec -T ollama ollama create mitsu-en -f /Modelfile.en
	docker-compose exec -T ollama ollama create mitsu-pt -f /Modelfile.pt

monitor:
	@echo "Manual routing required via qpwgraph. Connect Mitsu_Mouth to your playback device."

test:
	@echo "Running Go unit tests..."
	go test -v ./pkg/...
	@echo "Running Go E2E tests..."
	go test -v ./e2e/...
	@echo "Running Python tests..."
	python3 tests/test_voice_lab.py

help:
	@echo "Available targets:"
	@echo "  make run         - Build and start Mitsu"
	@echo "  make test        - Run all tests"
	@echo "  make clean       - Reset everything"

.DEFAULT_GOAL := help
