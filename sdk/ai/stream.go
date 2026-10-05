package ai

import (
	"context"
	"errors"
)

type Event interface {
	eventMarker()
}

type EventStart struct {
	Partial StreamAssistantMessage `json:"partial"`
}

func (EventStart) eventMarker() {}

type EventTextStart struct {
	ContentIndex int                    `json:"content_index"`
	Partial      StreamAssistantMessage `json:"partial"`
}

func (EventTextStart) eventMarker() {}

type EventTextDelta struct {
	ContentIndex int                    `json:"content_index"`
	Delta        string                 `json:"delta"`
	Partial      StreamAssistantMessage `json:"partial"`
}

func (EventTextDelta) eventMarker() {}

// EventThinkingDelta carries only reasoning explicitly returned by the provider.
type EventThinkingDelta struct {
	Delta string `json:"delta"`
}

func (EventThinkingDelta) eventMarker() {}

type EventTextEnd struct {
	ContentIndex int                    `json:"content_index"`
	Text         string                 `json:"text"`
	Partial      StreamAssistantMessage `json:"partial"`
}

func (EventTextEnd) eventMarker() {}

type EventToolCallStart struct {
	ContentIndex int                    `json:"content_index"`
	Partial      StreamAssistantMessage `json:"partial"`
}

func (EventToolCallStart) eventMarker() {}

type EventToolCallDelta struct {
	ContentIndex int                    `json:"content_index"`
	Delta        string                 `json:"delta"`
	Partial      StreamAssistantMessage `json:"partial"`
}

func (EventToolCallDelta) eventMarker() {}

type EventToolCallEnd struct {
	ContentIndex int                    `json:"content_index"`
	ToolCall     ToolCall               `json:"tool_call"`
	Partial      StreamAssistantMessage `json:"partial"`
}

func (EventToolCallEnd) eventMarker() {}

type EventDone struct {
	Reason  StopReason             `json:"reason"`
	Message StreamAssistantMessage `json:"message"`
}

func (EventDone) eventMarker() {}

type EventError struct {
	Reason string `json:"reason"`
	Error  string `json:"error"`
}

func (EventError) eventMarker() {}

type EventStream struct {
	ch     chan Event
	result StreamAssistantMessage
	err    error
	done   bool
}

func NewEventStream(buffer int) *EventStream {
	if buffer <= 0 {
		buffer = 1
	}
	return &EventStream{ch: make(chan Event, buffer)}
}

func (s *EventStream) Events() <-chan Event {
	return s.ch
}

func (s *EventStream) Push(ctx context.Context, event Event) error {
	select {
	case s.ch <- event:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (s *EventStream) SetResult(result StreamAssistantMessage, err error) {
	s.result = result
	s.err = err
}

func (s *EventStream) Close() {
	if s.done {
		return
	}
	s.done = true
	close(s.ch)
}

func (s *EventStream) Result() (StreamAssistantMessage, error) {
	if s.err == nil && s.done == false {
		return s.result, errors.New("event stream not closed")
	}
	return s.result, s.err
}
