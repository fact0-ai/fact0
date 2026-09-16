package auth

import "sync/atomic"

var jwtVerifyFailures atomic.Int64
var sseTicketFailures atomic.Int64

// IncJWTVerifyFailure increments the JWT verification failure counter.
func IncJWTVerifyFailure() { jwtVerifyFailures.Add(1) }

// JWTVerifyFailures returns total JWT verification failures since process start.
func JWTVerifyFailures() int64 { return jwtVerifyFailures.Load() }

// IncSSETicketFailure increments SSE ticket consume failures.
func IncSSETicketFailure() { sseTicketFailures.Add(1) }

// SSETicketFailures returns total SSE ticket failures since process start.
func SSETicketFailures() int64 { return sseTicketFailures.Load() }
