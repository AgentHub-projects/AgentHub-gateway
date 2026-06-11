package acp

import "encoding/json"

type ContentBlockDiff struct {
	Meta     map[string]any         `json:"_meta,omitempty"`
	ID       string                 `json:"id,omitempty"`
	Text     string                 `json:"text,omitempty"`
	Title    string                 `json:"title,omitempty"`
	Files    []ContentBlockDiffFile `json:"files,omitempty"`
	Metadata map[string]any         `json:"metadata,omitempty"`
	Type     string                 `json:"type"`
}

type ContentBlockDiffFile struct {
	Path      string `json:"path"`
	OldPath   string `json:"oldPath,omitempty"`
	Status    string `json:"status"`
	Additions int    `json:"additions"`
	Deletions int    `json:"deletions"`
	Patch     string `json:"patch"`
}

func (ContentBlockDiff) isContentBlockVariant() string {
	return "diff"
}

func init() {
	RegisterContentBlockVariant("diff", func(data []byte) (contentBlockVariant, error) {
		var v ContentBlockDiff
		if err := json.Unmarshal(data, &v); err != nil {
			return nil, err
		}
		v.Type = "diff"
		return v, nil
	})
}
