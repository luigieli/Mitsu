# Mitsu Project: Technical Debt & Flaws To-Do List

This document tracks identified architectural, security, and performance flaws in the Mitsu repository, along with proposed fixes.

## 1. Performance & Latency
| Problem | Possible Fix / Research | Affected Files |
| :--- | :--- | :--- |
| **High Process Overhead**: Frequent spawning of `ffmpeg` and `pacat` for every audio chunk adds significant latency. | Use CGO bindings for PulseAudio/FFmpeg or maintain persistent pipes with `StdinPipe`/`StdoutPipe` more effectively. Research `portaudio` or `beep` for Go. | `pkg/ear/ear.go`, `pkg/mouth/mouth.go` |

## 2. Security Vulnerabilities
| Problem | Possible Fix / Research | Affected Files |
| :--- | :--- | :--- |
| **Unprotected Web Interface**: No authentication or CSRF protection on the Command Center (port 8080). | Implement basic auth or a simple token-based system. Add CSRF tokens to forms/fetch calls. | `main.go` |
| **Arbitrary Text Injection**: The `/talk` endpoint allows any user to inject text into the LLM context without sanitization. | Add input validation, length limits, and rate-limiting to the web handlers. | `main.go`, `pkg/brain/brain.go` |
| **Exposed Microservices**: Internal services (Ollama, Kokoro, STT) are exposed to the host machine. | Remove `ports` mapping for internal services in `docker-compose.yml`; keep them only on the internal network. | `docker-compose.yml` |

## 3. Architectural Flaws
| Problem | Possible Fix / Research | Affected Files |
| :--- | :--- | :--- |
| **Broken Barge-In**: Mitsu stops listening while speaking, and there's no way to interrupt current playback. | Allow `Ear` to capture while speaking (using echo cancellation or simple volume ducking). Implement `BargeIn` logic to kill the `pacat` process immediately. | `pkg/ear/ear.go`, `pkg/mouth/mouth.go`, `main.go` |
| **Race Conditions in Gaming**: `GameController.conn` is accessed/modified across goroutines without a mutex. | Wrap `net.Conn` and connection state in a `sync.Mutex` or use a state management channel. | `pkg/gaming/controller.go` |
| **Fragile Sentence Splitting**: Simple punctuation-based splitting breaks on abbreviations like "Mr." or "U.S.". | Use a regex-based splitter or a specialized NLP library for sentence boundary detection. | `pkg/brain/brain.go` |

## 4. Code Quality & Robustness
| Problem | Possible Fix / Research | Affected Files |
| :--- | :--- | :--- |
| **Poor Error Handling**: Many `err` values are ignored (`_`), leading to silent failures. | Systematically audit all tool/network calls and implement proper logging/retry logic. | All files in `pkg/`, `main.go` |
| **Hardcoded Voice Filters**: `Mouth.BuildFilterChain` ignores `voice_config.json` parameters. | Refactor `BuildFilterChain` to dynamically construct the filter string based on the `VoiceConfig` struct. | `pkg/mouth/mouth.go` |
| **Redundant LLM Context**: Redundant "IMPORTANT" instructions are added to every message. | Move language enforcement to the System Prompt (Modelfile) or only include it once at the start of the session. | `pkg/brain/brain.go`, `Modelfile` |
| **Brittle Docker Config**: PulseAudio paths and User IDs are hardcoded to `1000`. | Use environment variables for `UID`/`GID` and research more portable PulseAudio socket sharing methods. | `docker-compose.yml`, `Dockerfile` |

## 5. Missing Features
| Problem | Possible Fix / Research | Affected Files |
| :--- | :--- | :--- |
| **No Health Checks**: Services may attempt to connect before dependencies are ready. | Add `healthcheck` blocks to `docker-compose.yml` for Ollama and Kokoro. | `docker-compose.yml` |
| **Manual Voice Blending**: `blend_anime.py` is not part of the automated build/deploy flow. | Add a setup script or a one-off container job in Compose to ensure voices are blended on startup if missing. | `blend_anime.py`, `docker-compose.yml` |
