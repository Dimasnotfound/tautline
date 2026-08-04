package main

import (
	"bytes"
	"compress/gzip"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestExternalMCPCompressionTransport(t *testing.T) {
	const payload = `{"jsonrpc":"2.0","id":1,"result":{"ok":true}}`
	compressed := gzipBytes(t, []byte(payload))

	tests := []struct {
		name              string
		body              []byte
		contentEncoding   string
		alreadyDecoded    bool
		wantUncompressed  bool
		wantEncodingEmpty bool
	}{
		{
			name:             "plain response",
			body:             []byte(payload),
			wantUncompressed: false,
		},
		{
			name:             "gzip magic without header",
			body:             compressed,
			wantUncompressed: true,
		},
		{
			name:              "gzip with header",
			body:              compressed,
			contentEncoding:   "gzip",
			wantUncompressed:  true,
			wantEncodingEmpty: true,
		},
		{
			name:             "already decoded by standard transport",
			body:             []byte(payload),
			alreadyDecoded:   true,
			wantUncompressed: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			base := roundTripFunc(func(*http.Request) (*http.Response, error) {
				header := make(http.Header)
				if test.contentEncoding != "" {
					header.Set("Content-Encoding", test.contentEncoding)
				}
				return &http.Response{
					StatusCode:   http.StatusOK,
					Header:       header,
					Body:         io.NopCloser(bytes.NewReader(test.body)),
					Uncompressed: test.alreadyDecoded,
				}, nil
			})
			transport := externalMCPCompressionTransport{base: base}
			request, err := http.NewRequest(http.MethodPost, "https://example.test/mcp", strings.NewReader("{}"))
			if err != nil {
				t.Fatal(err)
			}
			response, err := transport.RoundTrip(request)
			if err != nil {
				t.Fatal(err)
			}
			defer response.Body.Close()
			body, err := io.ReadAll(response.Body)
			if err != nil {
				t.Fatal(err)
			}
			if string(body) != payload {
				t.Fatalf("body=%q, want %q", body, payload)
			}
			if response.Uncompressed != test.wantUncompressed {
				t.Fatalf("Uncompressed=%v, want %v", response.Uncompressed, test.wantUncompressed)
			}
			if test.wantEncodingEmpty && response.Header.Get("Content-Encoding") != "" {
				t.Fatalf("Content-Encoding=%q, want empty", response.Header.Get("Content-Encoding"))
			}
		})
	}
}

func gzipBytes(t *testing.T, input []byte) []byte {
	t.Helper()
	var output bytes.Buffer
	writer := gzip.NewWriter(&output)
	if _, err := writer.Write(input); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return output.Bytes()
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

func TestNormalizeExternalMCPURL(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "preserves configured Google Docs endpoint",
			input: "https://docsmcp.googleapis.com/mcp/v1",
			want:  "https://docsmcp.googleapis.com/mcp/v1",
		},
		{
			name:  "migrates legacy Google Docs endpoint",
			input: "https://docsmcp.googleapis.com/mcp",
			want:  "https://docsmcp.googleapis.com/mcp/v1",
		},
		{
			name:  "does not rewrite another server",
			input: "https://example.test/mcp/v1",
			want:  "https://example.test/mcp/v1",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := normalizeExternalMCPURL(test.input); got != test.want {
				t.Fatalf("normalizeExternalMCPURL(%q)=%q, want %q", test.input, got, test.want)
			}
		})
	}
}
