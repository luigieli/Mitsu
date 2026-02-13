package brain

import (
	"encoding/json"
	"testing"
)

func TestJSONMarshalling(t *testing.T) {
	msg := ChatMessage{Role: "user", Content: "hello"}
	data, err := json.Marshal(msg)
	if err != nil {
		t.Fatalf("Failed to marshal ChatMessage: %v", err)
	}

	var decoded ChatMessage
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Failed to unmarshal ChatMessage: %v", err)
	}

	if decoded.Role != msg.Role || decoded.Content != msg.Content {
		t.Errorf("Decoded message does not match original: got %+v, want %+v", decoded, msg)
	}
}
