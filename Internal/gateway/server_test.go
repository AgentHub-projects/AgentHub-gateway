package gateway

import "testing"

func TestSandboxHTTPPath(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "download path strips filesystem prefix",
			in:   "/filesystem/download/agenthub-preview%2Fpreview.md",
			want: "/download/agenthub-preview%2Fpreview.md",
		},
		{
			name: "socket path stays under filesystem",
			in:   "/filesystem/socket.io/",
			want: "/filesystem/socket.io/",
		},
		{
			name: "other filesystem path is unchanged",
			in:   "/filesystem/unknown",
			want: "/filesystem/unknown",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := sandboxHTTPPath(tt.in); got != tt.want {
				t.Fatalf("sandboxHTTPPath(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}
