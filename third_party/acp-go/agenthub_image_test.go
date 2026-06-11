package acp

import (
	"bytes"
	"encoding/json"
	"testing"
)

func TestAgentHubImageContentRoundTrip(t *testing.T) {
	raw := []byte(`{
		"type": "image",
		"id": "image-1",
		"title": "assets/generated/test.png",
		"url": "https://cdn.example.com/test.png",
		"metadata": {
			"mimeType": "image/png",
			"sizeBytes": 42
		}
	}`)

	var image ContentBlockImage
	if err := json.Unmarshal(raw, &image); err != nil {
		t.Fatalf("unmarshal direct image content: %v", err)
	}
	if image.Meta[imageMetaID] != "image-1" {
		t.Fatalf("direct image content lost id meta: %#v", image.Meta)
	}

	var content ContentBlock
	if err := json.Unmarshal(raw, &content); err != nil {
		t.Fatalf("unmarshal image content: %v", err)
	}
	contentImage, ok := content.variant.(ContentBlockImage)
	if !ok {
		t.Fatalf("content block has unexpected variant: %T", content.variant)
	}
	if contentImage.Meta[imageMetaID] != "image-1" {
		t.Fatalf("content block image lost id meta: %#v", contentImage.Meta)
	}
	encoded, err := json.Marshal(content)
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
