package main

import (
	"bufio"
	"compress/gzip"
	"io"
	"net/http"
	"strings"
	"time"
)

// externalMCPHTTPClient keeps the standard Go HTTP behavior while also
// handling servers that return gzip bytes without a usable Content-Encoding
// header. Some preview MCP endpoints have emitted that response shape for
// larger tool results.
func externalMCPHTTPClient(timeout time.Duration) *http.Client {
	return &http.Client{
		Transport: externalMCPCompressionTransport{base: http.DefaultTransport},
		Timeout:   timeout,
	}
}

type externalMCPCompressionTransport struct {
	base http.RoundTripper
}

func (t externalMCPCompressionTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	base := t.base
	if base == nil {
		base = http.DefaultTransport
	}
	response, err := base.RoundTrip(request)
	if err != nil || response == nil || response.Body == nil {
		return response, err
	}
	if response.Uncompressed {
		return response, nil
	}

	buffered := bufio.NewReader(response.Body)
	prefix, peekErr := buffered.Peek(2)
	isGzip := peekErr == nil && len(prefix) == 2 && prefix[0] == 0x1f && prefix[1] == 0x8b
	declaredGzip := strings.Contains(strings.ToLower(response.Header.Get("Content-Encoding")), "gzip")
	if !isGzip {
		response.Body = &externalMCPBufferedBody{Reader: buffered, closer: response.Body}
		return response, nil
	}

	reader, gzipErr := gzip.NewReader(buffered)
	if gzipErr != nil {
		response.Body = &externalMCPBufferedBody{Reader: buffered, closer: response.Body}
		return response, nil
	}
	response.Body = &externalMCPGzipBody{Reader: reader, gzipReader: reader, closer: response.Body}
	response.Uncompressed = true
	response.ContentLength = -1
	if declaredGzip {
		response.Header.Del("Content-Encoding")
		response.Header.Del("Content-Length")
	}
	return response, nil
}

type externalMCPBufferedBody struct {
	io.Reader
	closer io.Closer
}

func (b *externalMCPBufferedBody) Close() error {
	return b.closer.Close()
}

type externalMCPGzipBody struct {
	io.Reader
	gzipReader *gzip.Reader
	closer     io.Closer
}

func (b *externalMCPGzipBody) Close() error {
	gzipErr := b.gzipReader.Close()
	bodyErr := b.closer.Close()
	if gzipErr != nil {
		return gzipErr
	}
	return bodyErr
}
