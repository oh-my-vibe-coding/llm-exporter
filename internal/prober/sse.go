package prober

import (
	"bufio"
	"io"
	"strings"
)

// SSEEvent represents a single Server-Sent Event.
type SSEEvent struct {
	Event string
	Data  string
}

// SSEReader reads SSE events from an io.Reader.
type SSEReader struct {
	scanner *bufio.Scanner
}

// NewSSEReader creates a new SSE reader.
func NewSSEReader(r io.Reader) *SSEReader {
	return &SSEReader{scanner: bufio.NewScanner(r)}
}

// Next reads the next SSE event. Returns io.EOF when the stream ends.
func (r *SSEReader) Next() (*SSEEvent, error) {
	var event SSEEvent
	var hasData bool

	for r.scanner.Scan() {
		line := r.scanner.Text()

		// Empty line = end of event
		if line == "" {
			if hasData {
				return &event, nil
			}
			continue
		}

		if strings.HasPrefix(line, "event:") {
			event.Event = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
		} else if strings.HasPrefix(line, "data:") {
			data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
			if hasData {
				event.Data += "\n" + data
			} else {
				event.Data = data
				hasData = true
			}
		}
		// Ignore comments (lines starting with :) and other fields
	}

	if err := r.scanner.Err(); err != nil {
		return nil, err
	}

	if hasData {
		return &event, nil
	}

	return nil, io.EOF
}
