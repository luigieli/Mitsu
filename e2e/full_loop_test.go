package e2e

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/network"
	"github.com/testcontainers/testcontainers-go/wait"
)

func TestMitsuFullLoopE2E(t *testing.T) {
	ctx := context.Background()
	cwd, _ := os.Getwd()

	// 1. Setup Network
	net, err := network.New(ctx)
	if err != nil {
		t.Fatalf("Failed to create network: %v", err)
	}
	defer net.Remove(ctx)

	// 2. Start Ollama
	ollamaC, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: testcontainers.ContainerRequest{
			Image: "ollama/ollama:latest",
			Networks: []string{net.Name},
			NetworkAliases: map[string][]string{net.Name: {"ollama"}},
			ExposedPorts: []string{"11434/tcp"},
			Files: []testcontainers.ContainerFile{
				{
					HostFilePath:      filepath.Join(cwd, "..", "Modelfile"),
					ContainerFilePath: "/Modelfile",
					FileMode:          0644,
				},
			},
			WaitingFor: wait.ForHTTP("/").WithPort("11434/tcp"),
		},
		Started: true,
	})
	if err != nil {
		t.Fatalf("Failed to start Ollama: %v", err)
	}
	defer ollamaC.Terminate(ctx)

	fmt.Println("E2E: Creating Mitsu persona in Ollama...")
	_, _, err = ollamaC.Exec(ctx, []string{"ollama", "pull", "gemma4:e4b"})
	if err != nil {
		t.Fatalf("Failed to pull model: %v", err)
	}
	_, _, err = ollamaC.Exec(ctx, []string{"ollama", "create", "mitsu", "-f", "/Modelfile"})
	if err != nil {
		t.Fatalf("Failed to create persona: %v", err)
	}

	// 3. Start Kokoro
	kokoroC, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: testcontainers.ContainerRequest{
			Image: "ghcr.io/remsky/kokoro-fastapi-cpu:latest",
			Networks: []string{net.Name},
			NetworkAliases: map[string][]string{net.Name: {"kokoro"}},
			ExposedPorts: []string{"8880/tcp"},
			WaitingFor: wait.ForHTTP("/v1/audio/speech").WithPort("8880/tcp").WithMethod("GET").WithStatusCodeMatcher(func(status int) bool {
				return status == 405 // Method not allowed is fine, means it's up
			}),
		},
		Started: true,
	})
	if err != nil {
		t.Fatalf("Failed to start Kokoro: %v", err)
	}
	defer kokoroC.Terminate(ctx)

	// 4. Start Companion
	inputWav := filepath.Join(cwd, "..", "tests", "data", "hello_en.wav")

	companionC, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: testcontainers.ContainerRequest{
			FromDockerfile: testcontainers.FromDockerfile{
				Context: filepath.Join(cwd, ".."),
			},
			Networks: []string{net.Name},
			Env: map[string]string{
				"OLLAMA_HOST":      "http://ollama:11434",
				"KOKORO_HOST":      "http://kokoro:8880",
				"TEST_INPUT_FILE":  "/app/test_input.wav",
				"TEST_OUTPUT_FILE": "/app/test_output.wav",
			},
			Files: []testcontainers.ContainerFile{
				{
					HostFilePath:      inputWav,
					ContainerFilePath: "/app/test_input.wav",
					FileMode:          0644,
				},
			},
			WaitingFor: wait.ForLog("Ear Routine started"),
		},
		Started: true,
	})
	if err != nil {
		t.Fatalf("Failed to start Companion: %v", err)
	}
	defer companionC.Terminate(ctx)

	// 5. Wait for the loop to complete
	fmt.Println("E2E: Waiting for Mitsu to process the full loop...")
	waitCtx, cancel := context.WithTimeout(ctx, 120*time.Second) // Increased timeout for model pull/init
	defer cancel()

	err = wait.ForLog("Mouth finished sentence").WaitUntilReady(waitCtx, companionC)
	if err != nil {
		reader, _ := companionC.Logs(ctx)
		logs, _ := io.ReadAll(reader)
		t.Fatalf("Full loop failed or timed out. Logs:\n%s", string(logs))
	}

	// 6. Verify output
	exitCode, _, err := companionC.Exec(ctx, []string{"ls", "/app/test_output.wav"})
	if err != nil || exitCode != 0 {
		reader, _ := companionC.Logs(ctx)
		logs, _ := io.ReadAll(reader)
		t.Fatalf("Mitsu did not produce the output wav file. Logs:\n%s", string(logs))
	}
}
