package main

import (
	"encoding/json"
	"path/filepath"
	"strings"

	fact0 "github.com/fact0-ai/fact0/sdk/go"
)

// ResourceFromTool derives a fact0.Resource describing the target of a tool call.
//
//	Bash       -> {type:"shell.command", id:hash of command, name:command preview}
//	Edit/Write -> {type:"file", id:absolute path, name:basename}
//	mcp__*     -> {type:"mcp.tool", id:toolName, name:toolName}
//	default    -> {type:"claude_code.tool", id:toolName, name:toolName}
//
// mode gates whether human-readable locator text (the shell command or the
// file path) is emitted in the Name/ID fields. In hash mode, only sha256
// digests ship so nothing sensitive can leak through the resource label; in
// metadata and raw modes the command preview and file path ship raw — that is
// what makes sessions readable in the dashboard.
func ResourceFromTool(toolName string, toolInput json.RawMessage, mode string) fact0.Resource {
	var fields map[string]any
	if len(toolInput) > 0 {
		_ = json.Unmarshal(toolInput, &fields)
	}
	hashOnly := mode == CaptureHash

	switch {
	case toolName == "Bash":
		cmd := stringField(fields, "command")
		name := ""
		if !hashOnly {
			name = truncate(cmd, locatorPreviewLen)
		}
		return fact0.Resource{
			ID:   Sha256Hex(cmd),
			Type: "shell.command",
			Name: name,
		}
	case toolName == "Edit" || toolName == "Write" || toolName == "MultiEdit" || toolName == "NotebookEdit":
		path := stringField(fields, "file_path")
		if path == "" {
			path = stringField(fields, "notebook_path")
		}
		if hashOnly {
			// Hash the path rather than shipping the absolute filesystem path.
			return fact0.Resource{
				ID:   Sha256Hex(path),
				Type: "file",
				Name: "",
			}
		}
		return fact0.Resource{
			ID:   path,
			Type: "file",
			Name: filepath.Base(path),
		}
	case strings.HasPrefix(toolName, "mcp__"):
		return fact0.Resource{
			ID:   toolName,
			Type: "mcp.tool",
			Name: toolName,
		}
	default:
		return fact0.Resource{
			ID:   toolName,
			Type: "claude_code.tool",
			Name: toolName,
		}
	}
}

func stringField(m map[string]any, key string) string {
	if m == nil {
		return ""
	}
	if v, ok := m[key].(string); ok {
		return v
	}
	return ""
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}
