package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"

	"github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/mcp"
)

type Manager struct {
	Client  *client.Client
	mu      sync.Mutex
}

func NewManager() *Manager {
	return &Manager{}
}

func (m *Manager) Start(ctx context.Context, command string, args ...string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	fmt.Printf("MCP Manager: Starting server with command: %s %v\n", command, args)
	
	// NewStdioMCPClient handles the process lifecycle internally
	c, err := client.NewStdioMCPClient(command, os.Environ(), args...)
	if err != nil {
		return fmt.Errorf("failed to create stdio MCP client: %v", err)
	}
	m.Client = c

	// Initialize MCP session
	initReq := mcp.InitializeRequest{
		Params: mcp.InitializeParams{
			ProtocolVersion: mcp.LATEST_PROTOCOL_VERSION,
			Capabilities: mcp.ClientCapabilities{
				Sampling: &struct{}{},
			},
			ClientInfo: mcp.Implementation{
				Name:    "Mitsu-Go-Client",
				Version: "1.0.0",
			},
		},
	}

	_, err = m.Client.Initialize(ctx, initReq)
	if err != nil {
		return fmt.Errorf("failed to initialize MCP client: %v", err)
	}

	fmt.Println("MCP Manager: Server connected and initialized.")
	return nil
}

func (m *Manager) ListAllTools(ctx context.Context) ([]mcp.Tool, error) {
	m.mu.Lock()
	client := m.Client
	m.mu.Unlock()

	if client == nil {
		return nil, fmt.Errorf("MCP client not started")
	}
	resp, err := client.ListTools(ctx, mcp.ListToolsRequest{})
	if err != nil {
		return nil, err
	}
	return resp.Tools, nil
}

func (m *Manager) CallTool(ctx context.Context, name string, args map[string]interface{}) (string, error) {
	m.mu.Lock()
	client := m.Client
	m.mu.Unlock()

	if client == nil {
		return "", fmt.Errorf("MCP client not started")
	}
	resp, err := client.CallTool(ctx, mcp.CallToolRequest{
		Params: mcp.CallToolParams{
			Name:      name,
			Arguments: args,
		},
	})
	if err != nil {
		return "", err
	}

	if resp.IsError {
		// Try to extract error message from content
		errMsg := m.extractTextFromContent(resp.Content)
		return "", fmt.Errorf("MCP Tool Error: %s", errMsg)
	}

	return m.extractTextFromContent(resp.Content), nil
}

func (m *Manager) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.Client != nil {
		return m.Client.Close()
	}
	return nil
}

func (m *Manager) extractTextFromContent(content []mcp.Content) string {
	var result strings.Builder
	for _, c := range content {
		result.WriteString(stringifyContent(c))
	}

	if result.Len() == 0 {
		return ""
	}
	return result.String()
}

func stringifyContent(c mcp.Content) string {
	if tc, ok := mcp.AsTextContent(c); ok {
		return tc.Text
	}
	
	// Fallback for non-text content (Image, Audio, etc.)
	data, err := json.Marshal(c)
	if err != nil {
		return ""
	}
	return string(data)
}
