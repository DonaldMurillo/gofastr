package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/control"
	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/ids"
	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/provider"
)

// StreamSummary is what the model sent in one provider round.
type StreamSummary struct {
	Text         string            // concatenated TextDelta payloads
	Thinking     []json.RawMessage // provider-stamped thinking blocks
	ToolUses     []control.ToolUse // any tool_use blocks the model emitted
	Usage        provider.Usage    // final accounting at message_stop
	FinishReason string            // raw provider finish_reason
}

// DefaultMaxStreamBytesPerTurn caps the text, thinking and tool-argument bytes one provider round may buffer.
const DefaultMaxStreamBytesPerTurn = 8 << 20 // 8 MiB

// ErrStreamCapExceeded is returned, with the partial summary, when a round passes its byte cap.
var ErrStreamCapExceeded = errors.New("engine: provider turn exceeded the stream buffer cap")

// CollectStream is CollectStreamWithCap with DefaultMaxStreamBytesPerTurn.
func CollectStream(
	ctx context.Context,
	bus *Bus,
	originator ids.ClientID,
	stream <-chan provider.StreamEvent,
) (StreamSummary, error) {
	return CollectStreamWithCap(ctx, bus, originator, stream, DefaultMaxStreamBytesPerTurn)
}

// CollectStreamWithCap publishes a provider stream to bus and returns its summary; maxBytes <= 0 means DefaultMaxStreamBytesPerTurn.
func CollectStreamWithCap(
	ctx context.Context,
	bus *Bus,
	originator ids.ClientID,
	stream <-chan provider.StreamEvent,
	maxBytes int,
) (StreamSummary, error) {
	if maxBytes <= 0 {
		maxBytes = DefaultMaxStreamBytesPerTurn
	}
	var (
		summary     StreamSummary
		textBuf     strings.Builder
		curTool     *control.ToolUse
		curToolJSON strings.Builder
		buffered    int
		sawTerminal bool
	)
	flushTool := func() {
		if curTool != nil {
			s := strings.TrimSpace(curToolJSON.String())
			if s == "" {
				s = "{}"
			}
			if !json.Valid([]byte(s)) {
				wrapped, err := json.Marshal(map[string]string{truncatedInputKey: s})
				if err != nil {
					wrapped = []byte("{}")
				}
				s = string(wrapped)
			}
			curTool.Input = json.RawMessage(s)
			summary.ToolUses = append(summary.ToolUses, *curTool)
			curTool = nil
			curToolJSON.Reset()
		}
	}
	capExceeded := func() (StreamSummary, error) {
		msg := fmt.Sprintf("provider turn exceeded the per-turn stream buffer cap of %d bytes; failing the turn to bound memory (partial output kept)", maxBytes)
		_, _ = bus.Publish(control.Error{
			Reason:  control.ReasonInvalidCommand,
			Message: msg,
		}, originator)
		flushTool()
		summary.Text = textBuf.String()
		return summary, fmt.Errorf("%w: %d bytes cap", ErrStreamCapExceeded, maxBytes)
	}
	for {
		select {
		case <-ctx.Done():
			flushTool()
			return summary, ctx.Err()
		case ev, ok := <-stream:
			if !ok {
				flushTool()
				summary.Text = textBuf.String()
				if !sawTerminal {
					return summary, ErrStreamClosed
				}
				return summary, nil
			}
			switch ev.Kind {
			case provider.KindTextDelta:
				if ev.Text == "" {
					continue
				}
				if buffered+len(ev.Text) > maxBytes {
					return capExceeded()
				}
				buffered += len(ev.Text)
				textBuf.WriteString(ev.Text)
				_, _ = bus.Publish(control.TextDelta{Text: ev.Text}, originator)
			case provider.KindThinkingDelta:
				if len(ev.Thinking) == 0 {
					continue
				}
				if buffered+len(ev.Thinking) > maxBytes {
					return capExceeded()
				}
				buffered += len(ev.Thinking)
				summary.Thinking = append(summary.Thinking, json.RawMessage(ev.Thinking))
				_, _ = bus.Publish(control.ThinkingDelta{Block: json.RawMessage(ev.Thinking)}, originator)
			case provider.KindToolUseStart:
				flushTool() // unusual but safe
				if ev.ToolUse != nil {
					tu := *ev.ToolUse
					curTool = &tu
				}
			case provider.KindToolUseDelta:
				if curTool == nil {
					continue
				}
				if buffered+len(ev.InputDelta) > maxBytes {
					return capExceeded()
				}
				buffered += len(ev.InputDelta)
				curToolJSON.WriteString(ev.InputDelta)
			case provider.KindToolUseStop:
				flushTool()
			case provider.KindUsage:
				if ev.Usage != nil {
					summary.Usage = *ev.Usage
				}
			case provider.KindStop:
				summary.FinishReason = ev.FinishReason
				sawTerminal = true
			case provider.KindError:
				_, _ = bus.Publish(control.Error{
					Reason:  control.ReasonInvalidCommand,
					Message: errOrEmpty(ev.Err),
				}, originator)
				flushTool()
				summary.Text = textBuf.String()
				return summary, ev.Err
			}
		}
	}
}

func errOrEmpty(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

const truncatedInputKey = "__gofastr_truncated_input"

// ErrStreamClosed is returned when the provider stream closed without a stop event.
var ErrStreamClosed = errors.New("engine: provider stream closed unexpectedly")
