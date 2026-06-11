package acp

import "encoding/json"

const (
	imageMetaID       = "agenthub.id"
	imageMetaText     = "agenthub.text"
	imageMetaTitle    = "agenthub.title"
	imageMetaURL      = "agenthub.url"
	imageMetaMetadata = "agenthub.metadata"
)

type contentBlockImageJSON struct {
	Meta        map[string]any `json:"_meta,omitempty"`
	Annotations *Annotations   `json:"annotations,omitempty"`
	Data        string         `json:"data,omitempty"`
	MimeType    string         `json:"mimeType,omitempty"`
	URI         string         `json:"uri,omitempty"`
	ID          string         `json:"id,omitempty"`
	Text        string         `json:"text,omitempty"`
	Title       string         `json:"title,omitempty"`
	URL         string         `json:"url,omitempty"`
	Metadata    map[string]any `json:"metadata,omitempty"`
	Type        string         `json:"type"`
}

func (v *ContentBlockImage) UnmarshalJSON(data []byte) error {
	var raw contentBlockImageJSON
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}

	meta := raw.Meta
	if meta == nil {
		meta = make(map[string]any)
	}
	setImageMeta(meta, imageMetaID, raw.ID)
	setImageMeta(meta, imageMetaText, raw.Text)
	setImageMeta(meta, imageMetaTitle, raw.Title)
	setImageMeta(meta, imageMetaURL, raw.URL)
	if raw.Metadata != nil {
		meta[imageMetaMetadata] = raw.Metadata
	}

	v.ImageContent = ImageContent{
		Meta:        emptyMapNil(meta),
		Annotations: raw.Annotations,
		Data:        raw.Data,
		MimeType:    raw.MimeType,
		URI:         raw.URI,
	}
	v.Type = "image"
	return nil
}

func (v ContentBlockImage) MarshalJSON() ([]byte, error) {
	meta := cloneMap(v.Meta)
	id := stringImageMeta(meta, imageMetaID)
	text := stringImageMeta(meta, imageMetaText)
	title := stringImageMeta(meta, imageMetaTitle)
	url := imageURL(v, meta)
	metadata := mapImageMeta(meta, imageMetaMetadata)
	raw := contentBlockImageJSON{
		Meta:        stripImageMeta(meta),
		Annotations: v.Annotations,
		Data:        v.Data,
		MimeType:    v.MimeType,
		URI:         v.URI,
		ID:          id,
		Text:        text,
		Title:       title,
		URL:         url,
		Metadata:    metadata,
		Type:        "image",
	}
	if raw.Meta != nil && len(raw.Meta) == 0 {
		raw.Meta = nil
	}
	return json.Marshal(raw)
}

func setImageMeta(meta map[string]any, key string, value string) {
	if value != "" {
		meta[key] = value
	}
}

func imageURL(v ContentBlockImage, meta map[string]any) string {
	if url := stringImageMeta(meta, imageMetaURL); url != "" {
		return url
	}
	return v.URI
}

func stringImageMeta(meta map[string]any, key string) string {
	value, _ := meta[key].(string)
	return value
}

func mapImageMeta(meta map[string]any, key string) map[string]any {
	value, _ := meta[key].(map[string]any)
	return value
}

func stripImageMeta(meta map[string]any) map[string]any {
	delete(meta, imageMetaID)
	delete(meta, imageMetaText)
	delete(meta, imageMetaTitle)
	delete(meta, imageMetaURL)
	delete(meta, imageMetaMetadata)
	return emptyMapNil(meta)
}

func cloneMap(input map[string]any) map[string]any {
	if input == nil {
		return nil
	}
	output := make(map[string]any, len(input))
	for key, value := range input {
		output[key] = value
	}
	return output
}

func emptyMapNil(input map[string]any) map[string]any {
	if len(input) == 0 {
		return nil
	}
	return input
}
