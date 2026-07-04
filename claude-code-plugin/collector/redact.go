package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
)

// Sha256Hex returns the lowercase hex-encoded SHA-256 digest of s.
func Sha256Hex(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

// locatorKeys are tool-input fields whose values locate the work rather than
// contain it — file paths, command text, search patterns. In metadata mode
// these ship raw (truncated) so sessions render readably in the dashboard,
// while content-bearing fields (file contents, new_string, outputs) stay
// hashed.
var locatorKeys = map[string]bool{
	"command":       true,
	"file_path":     true,
	"notebook_path": true,
	"path":          true,
	"pattern":       true,
	"url":           true,
	"query":         true,
}

// locatorPreviewLen bounds raw locator text so a huge command line or URL
// cannot smuggle a large payload through metadata mode.
const locatorPreviewLen = 200

// redactValue walks an arbitrary JSON value, applying the capture mode.
// key is the JSON object key the value sits under ("" at the root / in arrays).
//
//	hash     — every string becomes {sha256, len}.
//	metadata — locator-key strings ship raw (truncated); all others hashed.
//	raw      — strings ship as-is.
//
// Numbers, bools, and nulls are safe and always ship as-is.
func redactValue(key string, v any, mode string) any {
	switch t := v.(type) {
	case string:
		switch {
		case mode == CaptureRawMode:
			return t
		case mode == CaptureMetadata && locatorKeys[key]:
			return truncate(t, locatorPreviewLen)
		}
		return map[string]any{
			"sha256": Sha256Hex(t),
			"len":    len(t),
		}
	case map[string]any:
		out := make(map[string]any, len(t))
		for k, vv := range t {
			out[k] = redactValue(k, vv, mode)
		}
		return out
	case []any:
		out := make([]any, len(t))
		for i, vv := range t {
			out[i] = redactValue(key, vv, mode)
		}
		return out
	default:
		return t
	}
}

// RedactInput decodes raw tool input and returns a map that is safe to ship
// under the given capture mode.
func RedactInput(raw json.RawMessage, mode string) map[string]any {
	if len(raw) == 0 {
		return map[string]any{}
	}
	var decoded any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		// Not valid JSON: treat the bytes as an opaque string value.
		return map[string]any{"value": redactValue("", string(raw), mode)}
	}
	switch m := redactValue("", decoded, mode).(type) {
	case map[string]any:
		return m
	default:
		return map[string]any{"value": m}
	}
}

// SummarizeOutput decodes raw tool output and returns a ship-safe map. Tool
// outputs carry content, not locators, so metadata mode summarizes them the
// same as hash mode (string leaves hashed; numbers like exit codes ship
// as-is). Only raw mode ships output text.
func SummarizeOutput(raw json.RawMessage, mode string) map[string]any {
	if mode == CaptureMetadata {
		mode = CaptureHash
	}
	return RedactInput(raw, mode)
}
