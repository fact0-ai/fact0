package main

import (
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"strings"
)

// PolicyRule is a single governance rule. Match is a regular expression tested
// against the tool input (and, for the Bash tool, the command string). Tool, if
// non-empty, restricts the rule to a specific tool name (case-insensitive).
// Action "deny" blocks the tool call; any other action is treated as a no-op.
type PolicyRule struct {
	Match  string `json:"match"`
	Tool   string `json:"tool,omitempty"`
	Action string `json:"action"`
	Reason string `json:"reason,omitempty"`
}

// Policy is a collection of governance rules loaded from a JSON policy file.
type Policy struct {
	Rules []PolicyRule `json:"rules"`
}

// Decision is the outcome of evaluating a tool call against the active policy.
// Allow=false means the call must be denied with the given Reason.
type Decision struct {
	Allow       bool
	Reason      string
	MatchedRule string
}

// safetyDefaults are conservative, always-on deny rules applied ONLY when
// FACT0_CC_ENFORCE is truthy. They guard against a few catastrophic Bash
// commands. They are intentionally narrow to avoid false positives.
func safetyDefaults() []PolicyRule {
	return []PolicyRule{
		{Tool: "Bash", Match: `rm\s+-rf\s+/(?:\s|$)`, Action: "deny", Reason: "blocked by safety default: 'rm -rf /'"},
		{Tool: "Bash", Match: `:\(\)\s*\{`, Action: "deny", Reason: "blocked by safety default: fork bomb"},
		{Tool: "Bash", Match: `\bmkfs\b`, Action: "deny", Reason: "blocked by safety default: 'mkfs'"},
	}
}

// LoadPolicy reads the governance policy from the file named by
// FACT0_CC_POLICY_FILE. The second return value reports whether governance is
// active at all.
//
// Behavior (fail-open by design):
//   - If FACT0_CC_POLICY_FILE is unset, missing, or fails to parse, the file
//     rules are empty.
//   - If FACT0_CC_ENFORCE is truthy, a small set of conservative safety
//     defaults is always appended.
//   - The boolean is true only when at least one rule is in effect; otherwise
//     governance is effectively disabled (allow-all).
func LoadPolicy(cfg Config) (Policy, bool) {
	var pol Policy

	path := strings.TrimSpace(os.Getenv("FACT0_CC_POLICY_FILE"))
	if path != "" {
		if data, err := os.ReadFile(path); err == nil && len(data) > 0 {
			var filePol Policy
			if json.Unmarshal(data, &filePol) == nil {
				pol.Rules = append(pol.Rules, filePol.Rules...)
			} else {
				logf("policy file %q parse error; ignoring (fail-open)", path)
			}
		} else if err != nil {
			logf("policy file %q not readable; ignoring (fail-open)", path)
		}
	}

	if isTruthy(os.Getenv("FACT0_CC_ENFORCE")) {
		pol.Rules = append(pol.Rules, safetyDefaults()...)
	}

	return pol, len(pol.Rules) > 0
}

// bashCommand extracts the command string from a Bash tool input. Returns ""
// if the tool is not Bash or the command is absent/unparseable.
func bashCommand(in HookInput) string {
	if !strings.EqualFold(in.ToolName, "Bash") || len(in.ToolInput) == 0 {
		return ""
	}
	var m struct {
		Command string `json:"command"`
	}
	if json.Unmarshal(in.ToolInput, &m) != nil {
		return ""
	}
	return m.Command
}

// evaluationTarget builds the string a rule's regexp is tested against: the
// Bash command (when applicable) plus the raw stringified tool input. This lets
// rules match either the structured input or the literal command.
func evaluationTarget(in HookInput) string {
	parts := make([]string, 0, 2)
	if cmd := bashCommand(in); cmd != "" {
		parts = append(parts, cmd)
	}
	if len(in.ToolInput) > 0 {
		parts = append(parts, string(in.ToolInput))
	}
	return strings.Join(parts, "\n")
}

// EvaluateToolCall evaluates a pre-tool call against the active policy. It
// returns Allow=false with a Reason on the first matching deny rule, and
// Allow=true otherwise. It never panics: a panic or a bad regexp degrades to
// allow (fail-open).
func EvaluateToolCall(cfg Config, in HookInput) (d Decision) {
	d = Decision{Allow: true}
	defer func() {
		if r := recover(); r != nil {
			logf("governance evaluation panic recovered (fail-open allow): %v", r)
			d = Decision{Allow: true}
		}
	}()

	pol, active := LoadPolicy(cfg)
	if !active {
		return d
	}

	target := evaluationTarget(in)
	if target == "" {
		return d
	}

	for i, rule := range pol.Rules {
		if !strings.EqualFold(rule.Action, "deny") {
			continue
		}
		if rule.Tool != "" && !strings.EqualFold(rule.Tool, in.ToolName) {
			continue
		}
		if strings.TrimSpace(rule.Match) == "" {
			continue
		}
		re, err := regexp.Compile(rule.Match)
		if err != nil {
			// Bad regexp: skip the rule rather than failing closed.
			logf("governance: skipping rule %d with invalid regexp %q: %v", i, rule.Match, err)
			continue
		}
		if re.MatchString(target) {
			reason := rule.Reason
			if reason == "" {
				reason = fmt.Sprintf("blocked by policy rule matching %q", rule.Match)
			}
			return Decision{Allow: false, Reason: reason, MatchedRule: rule.Match}
		}
	}

	return d
}

// EmitDenyDecision prints the exact Claude Code PreToolUse deny payload to
// stdout. This is the ONLY place in the collector that writes to stdout.
func EmitDenyDecision(reason string) {
	out := map[string]any{
		"hookSpecificOutput": map[string]any{
			"hookEventName":            "PreToolUse",
			"permissionDecision":       "deny",
			"permissionDecisionReason": reason,
		},
	}
	b, err := json.Marshal(out)
	if err != nil {
		// Should be impossible for this shape; fail-open by emitting nothing.
		return
	}
	fmt.Fprintln(os.Stdout, string(b))
}
