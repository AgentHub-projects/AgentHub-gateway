package session

import (
	"bytes"
	"encoding/json"
	"testing"

	acp "github.com/ironpark/go-acp"
)

func TestAgentHubContentExtensionsRoundTrip(t *testing.T) {
	raw := []byte(`{
		"sessionId": "session-1",
		"update": {
			"sessionUpdate": "agent_message_chunk",
			"content": {
				"type": "diff",
				"id": "part-1",
				"text": "1 file changed",
				"title": "Workspace change",
				"files": [
					{
						"path": "README.md",
						"status": "M",
						"additions": 1,
						"deletions": 1,
						"patch": "diff --git a/README.md b/README.md"
					}
				],
				"metadata": {
					"agentId": "leader"
				}
			}
		}
	}`)

	var notification acp.SessionNotification
	if err := json.Unmarshal(raw, &notification); err != nil {
		t.Fatalf("unmarshal diff content: %v", err)
	}
	encoded, err := json.Marshal(notification)
	if err != nil {
		t.Fatalf("marshal diff content: %v", err)
	}
	if !bytes.Contains(encoded, []byte(`"type":"diff"`)) {
		t.Fatalf("marshaled content lost diff type: %s", encoded)
	}
}

func TestAgentHubImageContentPreservesRichFields(t *testing.T) {
	raw := []byte(`{
		"sessionId": "session-1",
		"update": {
			"sessionUpdate": "agent_message_chunk",
			"content": {
				"type": "image",
				"id": "image-1",
				"title": "assets/generated/test.png",
				"url": "https://cdn.example.com/test.png",
				"metadata": {
					"mimeType": "image/png",
					"sizeBytes": 42
				}
			}
		}
	}`)

	var notification acp.SessionNotification
	if err := json.Unmarshal(raw, &notification); err != nil {
		t.Fatalf("unmarshal image content: %v", err)
	}
	encoded, err := json.Marshal(notification)
	if err != nil {
		t.Fatalf("marshal image content: %v", err)
	}
	for _, expected := range [][]byte{
		[]byte(`"type":"image"`),
		[]byte(`"id":"image-1"`),
		[]byte(`"title":"assets/generated/test.png"`),
		[]byte(`"url":"https://cdn.example.com/test.png"`),
		[]byte(`"metadata":{"mimeType":"image/png","sizeBytes":42}`),
	} {
		if !bytes.Contains(encoded, expected) {
			t.Fatalf("marshaled image content lost %s: %s", expected, encoded)
		}
	}
}
