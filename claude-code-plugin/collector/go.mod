module github.com/fact0-ai/fact0/claude-code-plugin/collector

go 1.23

require github.com/fact0-ai/fact0/sdk/go v0.0.0

// Same-repo module: resolves for every clone of fact0-ai/fact0.
replace github.com/fact0-ai/fact0/sdk/go => ../../sdk/go
