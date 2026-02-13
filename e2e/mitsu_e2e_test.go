package e2e

import (
	"context"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

func TestMitsuCompanionE2E(t *testing.T) {
	ctx := context.Background()

	// Define the container request
	req := testcontainers.ContainerRequest{
		Image:        "mitsu-companion:latest",
		ExposedPorts: []string{"8080/tcp"},
		WaitingFor:   wait.ForHTTP("/").WithPort("8080/tcp").WithStartupTimeout(30 * time.Second),
		// Note: We don't necessarily need GPU for a basic model load check in E2E
		// but we could add it if the environment supports it.
	}

	// Start the container
	mitsuC, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
	})
	if err != nil {
		t.Fatalf("Failed to start container: %v", err)
	}
	defer mitsuC.Terminate(ctx)

	// Get the mapped port
	endpoint, err := mitsuC.Endpoint(ctx, "")
	if err != nil {
		t.Fatalf("Failed to get endpoint: %v", err)
	}

	// 1. Check if the web interface is accessible
	resp, err := http.Get("http://" + endpoint + "/")
	if err != nil {
		t.Fatalf("Failed to GET /: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected status OK, got %v", resp.Status)
	}

	body, _ := io.ReadAll(resp.Body)
	if !contains(string(body), "MITSU COMMAND CENTER") {
		t.Errorf("Response body does not contain expected title")
	}

	// 2. Check logs for initialization
	reader, err := mitsuC.Logs(ctx)
	if err != nil {
		t.Fatalf("Failed to get logs: %v", err)
	}
	defer reader.Close()

	logs, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("Failed to read logs: %v", err)
	}

	logStr := string(logs)
	expectedLogs := []string{
		"Starting Mitsu",
		"Ear Routine started",
		"Brain Routine started",
		"Mouth Routine started",
	}

	for _, expected := range expectedLogs {
		if !contains(logStr, expected) {
			t.Errorf("Logs missing expected entry: %q", expected)
		}
	}

	// 3. Verify whisper-cpp can load the model
	exitCode, execReader, err := mitsuC.Exec(ctx, []string{"./whisper-cpp", "-m", "models/ggml-small-q5_1.bin", "-h"})
	if err != nil {
		t.Fatalf("Failed to exec whisper-cpp: %v", err)
	}
	
	out, _ := io.ReadAll(execReader)
	if exitCode != 0 {
		t.Errorf("whisper-cpp exited with code %d. Output: %s", exitCode, string(out))
	}
	if !contains(string(out), "usage: ./whisper-cpp") {
		t.Errorf("whisper-cpp output does not contain usage info")
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || (len(substr) > 0 && (s[0:len(substr)] == substr || contains(s[1:], substr))))
}
