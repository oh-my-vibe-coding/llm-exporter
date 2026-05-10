package prober

import (
	"io"
	"strings"
	"testing"
)

func TestSSEReader_BasicEvents(t *testing.T) {
	input := "event: message_start\ndata: {\"type\":\"start\"}\n\nevent: content\ndata: {\"text\":\"hi\"}\n\n"
	reader := NewSSEReader(strings.NewReader(input))

	event1, err := reader.Next()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if event1.Event != "message_start" {
		t.Errorf("event1.Event = %q, want %q", event1.Event, "message_start")
	}
	if event1.Data != `{"type":"start"}` {
		t.Errorf("event1.Data = %q, want %q", event1.Data, `{"type":"start"}`)
	}

	event2, err := reader.Next()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if event2.Event != "content" {
		t.Errorf("event2.Event = %q, want %q", event2.Event, "content")
	}
	if event2.Data != `{"text":"hi"}` {
		t.Errorf("event2.Data = %q, want %q", event2.Data, `{"text":"hi"}`)
	}

	_, err = reader.Next()
	if err != io.EOF {
		t.Errorf("expected io.EOF, got %v", err)
	}
}

func TestSSEReader_MultiLineData(t *testing.T) {
	input := "data: line1\ndata: line2\ndata: line3\n\n"
	reader := NewSSEReader(strings.NewReader(input))

	event, err := reader.Next()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if event.Data != "line1\nline2\nline3" {
		t.Errorf("Data = %q, want %q", event.Data, "line1\nline2\nline3")
	}
}

func TestSSEReader_DoneEvent(t *testing.T) {
	input := "data: {\"chunk\":1}\n\ndata: [DONE]\n\n"
	reader := NewSSEReader(strings.NewReader(input))

	event1, err := reader.Next()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if event1.Data != `{"chunk":1}` {
		t.Errorf("Data = %q, want %q", event1.Data, `{"chunk":1}`)
	}

	event2, err := reader.Next()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if event2.Data != "[DONE]" {
		t.Errorf("Data = %q, want %q", event2.Data, "[DONE]")
	}
}

func TestSSEReader_CommentsIgnored(t *testing.T) {
	input := ": this is a comment\ndata: hello\n\n"
	reader := NewSSEReader(strings.NewReader(input))

	event, err := reader.Next()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if event.Data != "hello" {
		t.Errorf("Data = %q, want %q", event.Data, "hello")
	}
}

func TestSSEReader_EmptyStream(t *testing.T) {
	reader := NewSSEReader(strings.NewReader(""))
	_, err := reader.Next()
	if err != io.EOF {
		t.Errorf("expected io.EOF, got %v", err)
	}
}

func TestSSEReader_NoTrailingNewline(t *testing.T) {
	input := "data: final"
	reader := NewSSEReader(strings.NewReader(input))

	event, err := reader.Next()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if event.Data != "final" {
		t.Errorf("Data = %q, want %q", event.Data, "final")
	}
}
