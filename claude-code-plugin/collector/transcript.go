package main

import (
	"bufio"
	"encoding/json"
	"io"
	"os"
	"strings"
)

type AssistantMessage struct {
	ID        string          `json:"id"`
	Source    string          `json:"source,omitempty"`
	UUID      string          `json:"uuid,omitempty"`
	Timestamp string          `json:"timestamp,omitempty"`
	Model     string          `json:"model,omitempty"`
	Content   json.RawMessage `json:"content"`
}

// TurnStats preserves the ordered supported transcript content, not merely
// the final answer. Availability is always explicit, including missing files.
type TurnStats struct {
	TurnID        string             `json:"turn_id,omitempty"`
	ResponseText  string             `json:"response_text"`
	InputTokens   int64              `json:"input_tokens"`
	OutputTokens  int64              `json:"output_tokens"`
	Model         string             `json:"model,omitempty"`
	Messages      []AssistantMessage `json:"messages,omitempty"`
	CaptureStatus string             `json:"capture_status"`
	CaptureReason string             `json:"capture_reason,omitempty"`
}

type transcriptLine struct {
	Type      string `json:"type"`
	UUID      string `json:"uuid"`
	Timestamp string `json:"timestamp"`
	IsMeta    bool   `json:"isMeta"`
	Message   struct {
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

type contentBlock struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

const maxTranscriptBytes = 256 << 20

func realUserPrompt(raw json.RawMessage) bool {
	var text string
	if json.Unmarshal(raw, &text) == nil {
		return true
	}
	var blocks []contentBlock
	if json.Unmarshal(raw, &blocks) != nil {
		return false
	}
	hasText := false
	for _, b := range blocks {
		if b.Type == "tool_result" {
			return false
		}
		if b.Type != "" {
			hasText = true
		}
	}
	return hasText
}

func ReadTurnFromTranscript(path string) (TurnStats, bool) {
	return ReadTurnFromTranscriptTurn(path, "")
}

// ReadTurnFromTranscriptTurn selects one captured user turn even if later
// prompts have since been appended to the same transcript.
func ReadTurnFromTranscriptTurn(path, targetTurn string) (TurnStats, bool) {
	st := TurnStats{CaptureStatus: "unavailable", CaptureReason: "transcript path not supplied"}
	if path == "" {
		return st, false
	}
	f, err := os.Open(path)
	if err != nil {
		st.CaptureReason = "transcript unreadable: " + err.Error()
		return st, false
	}
	defer f.Close()
	partialReason := ""
	if info, err := f.Stat(); err == nil && info.Size() > maxTranscriptBytes {
		if _, err = f.Seek(info.Size()-maxTranscriptBytes, 0); err != nil {
			st.CaptureReason = "transcript seek failed: " + err.Error()
			return st, false
		}
		partialReason = "transcript exceeds 256 MiB read window; earlier content unavailable"
	}
	reader := bufio.NewReader(io.LimitReader(f, maxTranscriptBytes+1))
	if partialReason != "" {
		_, _ = reader.ReadBytes('\n')
	}
	type usage struct {
		in, out int64
		model   string
	}
	order := []string{}
	usages := map[string]usage{}
	seenUUID := map[string]bool{}
	texts := []string{}
	for {
		raw, readErr := reader.ReadBytes('\n')
		if len(raw) > 0 {
			var line transcriptLine
			if err := json.Unmarshal(raw, &line); err != nil {
				partialReason = "transcript contains malformed or incomplete JSON records"
			} else {
				if line.Type == "user" && !line.IsMeta && realUserPrompt(line.Message.Content) {
					if targetTurn != "" && st.TurnID == targetTurn {
						break
					}
					st.TurnID = line.UUID
					if st.TurnID == "" {
						st.TurnID = "prompt-" + Sha256Hex(string(raw))
					}

					st.Messages = nil
					order = nil
					usages = map[string]usage{}
					seenUUID = map[string]bool{}
					texts = nil
					partialReason = ""
				}
				if line.Type == "assistant" && (targetTurn == "" || st.TurnID == targetTurn) {
					id := line.Message.ID
					if id == "" {
						id = line.UUID
					}
					if id == "" {
						partialReason = "assistant record lacks message identity"
						id = "record-" + Sha256Hex(string(raw))
					}
					if _, ok := usages[id]; !ok {
						order = append(order, id)
					}
					u := line.Message.Usage
					old := usages[id]
					input := u.InputTokens + u.CacheReadInputTokens + u.CacheCreationInputTokens
					if input == 0 {
						input = old.in
					}
					output := u.OutputTokens
					if output < old.out {
						output = old.out
					}
					model := line.Message.Model
					if model == "" {
						model = old.model
					}
					usages[id] = usage{input, output, model}
					// Repeated snapshots may share a UUID but add content or
					// update usage. Deduplicate only identical content after
					// accounting for the latest usage values.
					recordKey := line.UUID + "|" + Sha256Hex(string(line.Message.Content))
					if line.UUID != "" && seenUUID[recordKey] {
						if readErr == io.EOF {
							break
						}
						continue
					}
					seenUUID[recordKey] = line.UUID != ""
					content := append(json.RawMessage(nil), line.Message.Content...)
					if len(content) == 0 {
						partialReason = "assistant record has no supported content"
					} else {
						st.Messages = append(st.Messages, AssistantMessage{ID: id, UUID: line.UUID, Timestamp: line.Timestamp, Model: line.Message.Model, Content: content})
						var blocks []contentBlock
						if json.Unmarshal(content, &blocks) == nil {
							for _, b := range blocks {
								if b.Type == "text" {
									texts = append(texts, b.Text)
								}
							}
						} else {
							var text string
							if json.Unmarshal(content, &text) == nil {
								texts = append(texts, text)
							} else {
								partialReason = "assistant content format unavailable"
							}
						}
					}
				}
			}
		}
		if readErr != nil {
			if readErr != io.EOF {
				partialReason = "transcript read failed: " + readErr.Error()
			}
			break
		}
	}
	if len(st.Messages) == 0 {
		st.CaptureReason = "no assistant messages found for the latest turn"
		if partialReason != "" {
			st.CaptureReason = partialReason
		}
		return st, false
	}
	for _, id := range order {
		u := usages[id]
		st.OutputTokens += u.out
		if u.in > 0 {
			st.InputTokens = u.in
		}
		if u.model != "" {
			st.Model = u.model
		}
	}
	st.ResponseText = strings.Join(texts, "\n")
	st.CaptureStatus = "complete"
	st.CaptureReason = ""
	if partialReason != "" {
		st.CaptureStatus = "partial"
		st.CaptureReason = partialReason
	}
	return st, true
}
