package main

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// Capture modes control how much raw text leaves the machine.
//
//	hash     — every string (paths, commands, prompts, contents) ships as
//	           sha256+len only. For locked-down environments.
//	metadata — Locator text ships raw (file paths, command text,
//	           prompt text, tool names) so sessions are readable in the
//	           dashboard; file contents and tool outputs stay hashed.
//	raw      — DEFAULT. Everything ships as-is, including file contents and outputs.
const (
	CaptureHash     = "hash"
	CaptureMetadata = "metadata"
	CaptureRawMode  = "raw"
)

// Config holds the collector's runtime configuration, sourced from the
// environment. All fields are populated by LoadConfig.
type Config struct {
	APIKey          string
	BaseURL         string
	ActorID         string
	ActorEmail      string
	CaptureMode     string
	Disabled        bool
	StateDir        string
	RemotePolicy    bool // retained for compatibility; enforcement is disabled in this release
	SpoolMaxBytes   int64
	RequestMaxBytes int64
	transaction     *hookTransaction
}

// RawCapture reports whether full raw capture (contents and outputs) is on.
func (c Config) RawCapture() bool { return c.CaptureMode == CaptureRawMode }

// isTruthy reports whether an environment value should be treated as "on".
func isTruthy(s string) bool {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

// defaultStateDir returns ~/.fact0/cc, falling back to a temp dir if the
// user's home directory cannot be resolved.
func defaultStateDir() string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return filepath.Join(os.TempDir(), "fact0", "cc")
	}
	return filepath.Join(home, ".fact0", "cc")
}

// LoadConfig reads the collector configuration from the environment.
//
// Environment variables:
//
//	FACT0_API_KEY          API key for the Fact0 backend.
//	FACT0_BASE_URL         Override the API base URL.
//	FACT0_CC_ACTOR_ID      Human actor id (defaults to OS user in HumanActor).
//	FACT0_CC_ACTOR_EMAIL   Human actor email.
//	FACT0_CC_CAPTURE_MODE  hash | metadata | raw (default) — see mode docs above.
//	FACT0_CC_CAPTURE_RAW   Legacy: truthy (1/true/yes/on) implies mode raw.
//	FACT0_CC_DISABLED      Truthy to disable the collector entirely.
//	FACT0_CC_STATE_DIR     Override the per-session state directory.
//	FACT0_CC_SPOOL_MAX_BYTES Durable hook queue budget (default 256 MiB).
//	FACT0_CC_REQUEST_MAX_BYTES Request cap (default/hard maximum 4 MiB).
//
// Remote governance and enforcement are disabled in this release.
func LoadConfig() Config {
	stateDir := strings.TrimSpace(os.Getenv("FACT0_CC_STATE_DIR"))
	if stateDir == "" {
		stateDir = defaultStateDir()
	}
	apiKey := strings.TrimSpace(os.Getenv("FACT0_API_KEY"))
	baseURL := strings.TrimSpace(os.Getenv("FACT0_BASE_URL"))
	if baseURL == "" {
		baseURL = "http://localhost:8000"
	}
	return Config{
		APIKey:          apiKey,
		BaseURL:         baseURL,
		ActorID:         strings.TrimSpace(os.Getenv("FACT0_CC_ACTOR_ID")),
		ActorEmail:      strings.TrimSpace(os.Getenv("FACT0_CC_ACTOR_EMAIL")),
		CaptureMode:     captureModeFromEnv(),
		Disabled:        isTruthy(os.Getenv("FACT0_CC_DISABLED")),
		StateDir:        stateDir,
		RemotePolicy:    false,
		SpoolMaxBytes:   positiveBytes("FACT0_CC_SPOOL_MAX_BYTES", 256<<20),
		RequestMaxBytes: positiveBytes("FACT0_CC_REQUEST_MAX_BYTES", 4<<20),
	}
}

// captureModeFromEnv resolves the capture mode. An explicit
// FACT0_CC_CAPTURE_MODE wins; otherwise the legacy FACT0_CC_CAPTURE_RAW
// truthy flag selects raw; the default is raw.
func captureModeFromEnv() string {
	switch mode := strings.ToLower(strings.TrimSpace(os.Getenv("FACT0_CC_CAPTURE_MODE"))); mode {
	case CaptureHash, CaptureMetadata, CaptureRawMode:
		return mode
	case "":
		// fall through to legacy flag
	default:
		logf("unknown FACT0_CC_CAPTURE_MODE %q, using %q", mode, CaptureRawMode)
		return CaptureRawMode
	}
	if isTruthy(os.Getenv("FACT0_CC_CAPTURE_RAW")) {
		return CaptureRawMode
	}
	return CaptureRawMode
}

func positiveBytes(name string, fallback int64) int64 {
	v, err := strconv.ParseInt(strings.TrimSpace(os.Getenv(name)), 10, 64)
	if err != nil || v <= 0 {
		return fallback
	}
	return v
}
