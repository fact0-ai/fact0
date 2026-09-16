package rest

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

// MaxRequestBodyBytes is the encoded JSON request limit for the local API.
const MaxRequestBodyBytes int64 = 4 << 20

// RequestBodyLimit rejects an oversized request before a handler can commit
// its first JSON value. It also catches oversized trailing whitespace/data.
func RequestBodyLimit(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Body != nil && r.Body != http.NoBody {
			body, err := io.ReadAll(io.LimitReader(r.Body, MaxRequestBodyBytes+1))
			_ = r.Body.Close()
			if int64(len(body)) > MaxRequestBodyBytes {
				writeJSON(w, http.StatusRequestEntityTooLarge, map[string]any{"error": "request exceeds 4 MiB limit", "code": "PAYLOAD_TOO_LARGE", "max_bytes": MaxRequestBodyBytes})
				return
			}
			if err != nil {
				writeError(w, http.StatusBadRequest, "reading request body")
				return
			}
			r.Body = io.NopCloser(bytes.NewReader(body))
		}
		next.ServeHTTP(w, r)
	})
}

// requestJSONDecoder retains integer precision in arbitrary capture metadata.
func requestJSONDecoder(r *http.Request) requestDecoder {
	d := json.NewDecoder(r.Body)
	d.UseNumber()
	return requestDecoder{d}
}

type requestDecoder struct{ decoder *json.Decoder }

func (d requestDecoder) Decode(dst any) error {
	if err := d.decoder.Decode(dst); err != nil {
		return err
	}
	var trailing any
	if err := d.decoder.Decode(&trailing); err != io.EOF {
		return fmt.Errorf("request must contain exactly one JSON value")
	}
	return nil
}
