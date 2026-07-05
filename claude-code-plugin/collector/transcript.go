package main

import (
	"bufio"
	"encoding/json"
	"os"
	"strings"
)

// TurnStats summarizes the assistant's side of the just-finished turn, read
// from the Claude Code transcript JSONL. The transcript format is internal
// and unstable, so parsing is strictly best-effort: any surprise yields a
// zero-value stat rather than an error.
type TurnStats struct {
	ResponseText string // final assistant text of the turn ("" if none)
	InputTokens  int64  // effective context of the last call (incl. cache)
	OutputTokens int64  // summed output tokens across the turn's messages
	Model        string
}

// transcriptLine is the subset of a transcript JSONL line we care about.
type transcriptLine struct {
	Type    string `json:"type"`
	Message struct {
		ID      string          `json:"id"`
		Model   string          `json:"model"`
		Content json.RawMessage `json:"content"`
		Usage   struct {
			InputTokens              int64 `json:"input_tokens"`
			OutputTokens             int64 `json:"output_tokens"`
			CacheReadInputTokens     int64 `json:"cache_read_input_tokens"`
			CacheCreationInputTokens int64 `json:"cache_creation_input_tokens"`
		} `json:"usage"`
	} `json:"message"`
}

// contentBlock is one element of an assistant/user content array.
type contentBlock struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// maxTranscriptBytes caps how much transcript is read; long sessions are
// read from the tail, which always contains the latest turn.
const maxTranscriptBytes = 8 << 20 // 8 MiB

// ReadTurnFromTranscript extracts stats for the most recent turn: everything
// after the last real user prompt (a user line whose content is a plain
// string — tool_result lines are also type "user" but carry arrays).
// Returns (stats, true) when at least one assistant message was found.
func ReadTurnFromTranscript(path string) (TurnStats, bool) {
	if path == "" {
		return TurnStats{}, false
	}
	f, err := os.Open(path)
	if err != nil {
		return TurnStats{}, false
	}
	defer f.Close()

	// Seek to the tail of very large transcripts; drop the first (likely
	// partial) line after seeking.
	if info, err := f.Stat(); err == nil && info.Size() > maxTranscriptBytes {
		if _, err := f.Seek(info.Size()-maxTranscriptBytes, 0); err != nil {
			return TurnStats{}, false
		}
	}

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 1<<20), 1<<20)

	// One assistant message spans multiple JSONL lines (one per content
	// block) with the SAME message id and repeated usage — aggregate by id
	// so tokens are not double-counted.
	type msg struct {
		in, out int64
		model   string
		text    strings.Builder
	}
	var (
		order []string
		byID  = map[string]*msg{}
	)
	reset := func() {
		order = nil
		byID = map[string]*msg{}
	}

	for scanner.Scan() {
		raw := scanner.Bytes()
		var line transcriptLine
		if err := json.Unmarshal(raw, &line); err != nil {
			continue
		}
		switch line.Type {
		case "user":
			// A real user prompt has string content; tool results come as
			// arrays. A new prompt starts a new turn.
			var s string
			if err := json.Unmarshal(line.Message.Content, &s); err == nil {
				reset()
			}
		case "assistant":
			id := line.Message.ID
			if id == "" {
				continue
			}
			m, ok := byID[id]
			if !ok {
				m = &msg{}
				byID[id] = m
				order = append(order, id)
			}
			u := line.Message.Usage
			m.in = u.InputTokens + u.CacheReadInputTokens + u.CacheCreationInputTokens
			m.out = u.OutputTokens
			if line.Message.Model != "" {
				m.model = line.Message.Model
			}
			var blocks []contentBlock
			if err := json.Unmarshal(line.Message.Content, &blocks); err == nil {
				for _, b := range blocks {
					if b.Type == "text" && b.Text != "" {
						if m.text.Len() > 0 {
							m.text.WriteString("\n")
						}
						m.text.WriteString(b.Text)
					}
				}
			}
		}
	}

	if len(order) == 0 {
		return TurnStats{}, false
	}

	var st TurnStats
	for _, id := range order {
		m := byID[id]
		st.OutputTokens += m.out
		if m.in > 0 {
			st.InputTokens = m.in // last call's effective context wins
		}
		if m.model != "" {
			st.Model = m.model
		}
		if t := m.text.String(); t != "" {
			st.ResponseText = t // last message with text wins
		}
	}
	return st, true
}
