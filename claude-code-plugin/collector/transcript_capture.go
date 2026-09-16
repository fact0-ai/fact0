package main

import (
	"context"
	"encoding/json"
	"strings"
	"time"
)

func lastMessageText(stats TurnStats) string {
	if len(stats.Messages) == 0 {
		return ""
	}
	raw := stats.Messages[len(stats.Messages)-1].Content
	var plain string
	if json.Unmarshal(raw, &plain) == nil {
		return plain
	}
	var blocks []contentBlock
	if json.Unmarshal(raw, &blocks) != nil {
		return ""
	}
	var texts []string
	for _, b := range blocks {
		if b.Type == "text" {
			texts = append(texts, b.Text)
		}
	}
	return strings.Join(texts, "\n")
}

func finalTranscriptPresent(in HookInput) bool {
	if in.LastAssistantMessage == nil || in.CapturedTurn == nil || len(in.CapturedTurn.Messages) == 0 {
		return false
	}
	last := in.CapturedTurn.Messages[len(in.CapturedTurn.Messages)-1]
	return last.Source == "" && lastMessageText(*in.CapturedTurn) == *in.LastAssistantMessage
}

// applyFinalAssistant preserves the Stop hook's final text even when Claude
// has not flushed it to JSONL yet. Fallback text is explicitly partial because
// the final transcript's other blocks and usage are not yet available.
func applyFinalAssistant(in *HookInput, event string) {
	if in.CapturedTurn == nil {
		in.CapturedTurn = &TurnStats{CaptureStatus: "unavailable", CaptureReason: "transcript unavailable"}
	}
	stats := in.CapturedTurn
	if in.LastAssistantMessage == nil {
		if stats.CaptureStatus == "complete" {
			stats.CaptureStatus = "partial"
			stats.CaptureReason = "hook did not supply final assistant text; transcript completeness cannot be confirmed"
		}
		return
	}
	if finalTranscriptPresent(*in) {
		return
	}
	final := *in.LastAssistantMessage
	if len(stats.Messages) == 0 || stats.Messages[len(stats.Messages)-1].Source == "" {
		raw, _ := json.Marshal([]map[string]string{{"type": "text", "text": final}})
		stats.Messages = append(stats.Messages, AssistantMessage{ID: "hook-final-" + Sha256Hex(final), Source: event + "_hook", Timestamp: in.Timestamp, Content: raw})
		if stats.ResponseText != "" && final != "" {
			stats.ResponseText += "\n"
		}
		stats.ResponseText += final
	}
	stats.CaptureStatus = "partial"
	stats.CaptureReason = "final text captured from hook; final transcript blocks or usage not yet available"
}

// Refinement happens only in the detached worker. A captured turn ID prevents
// a later prompt from replacing the queued turn while delivery is delayed.
func refineCapturedTurn(ctx context.Context, in *HookInput, event string) {
	if in.LastAssistantMessage == nil || finalTranscriptPresent(*in) {
		applyFinalAssistant(in, event)
		return
	}
	path := in.TranscriptPath
	if event == "subagent-stop" {
		path = in.AgentTranscriptPath
	}
	if path == "" {
		applyFinalAssistant(in, event)
		return
	}
	target := ""
	if in.CapturedTurn != nil {
		target = in.CapturedTurn.TurnID
	}
	waitCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	for {
		fresh, _ := ReadTurnFromTranscriptTurn(path, target)
		// When the original file lacked a turn boundary, preserve its snapshot
		// unless its captured first message is still present in the candidate.
		sameTurn := target != "" && fresh.TurnID == target
		if target == "" {
			sameTurn = in.CapturedTurn == nil || len(in.CapturedTurn.Messages) == 0
			if !sameTurn {
				for _, m := range fresh.Messages {
					if m.ID == in.CapturedTurn.Messages[0].ID {
						sameTurn = true
						break
					}
				}
			}
		}
		candidate := *in
		candidate.CapturedTurn = &fresh
		if sameTurn && finalTranscriptPresent(candidate) {
			in.CapturedTurn = &fresh
			return
		}
		select {
		case <-waitCtx.Done():
			applyFinalAssistant(in, event)
			return
		case <-time.After(100 * time.Millisecond):
		}
	}
}
