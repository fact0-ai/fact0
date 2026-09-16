package audit

import "github.com/rs/zerolog"

// zerologDiscard returns a zerolog.Logger that discards everything,
// suitable for use in unit tests where we do not want to assert on logs.
func zerologDiscard() zerolog.Logger { return zerolog.Nop() }
