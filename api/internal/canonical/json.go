// Package canonical encodes deterministic JSON for persisted content hashes.
package canonical

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

// JSON sorts object keys, preserves integer precision, and gives equal decimal
// numbers the same encoding (JSONB normalizes 1e3 and 1000, for example).
func JSON(v any) ([]byte, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	d := json.NewDecoder(bytes.NewReader(b))
	d.UseNumber()
	var value any
	if err := d.Decode(&value); err != nil {
		return nil, err
	}
	value, err = numbers(value)
	if err != nil {
		return nil, err
	}
	return json.Marshal(value)
}

type number string

func (n number) MarshalJSON() ([]byte, error) { return []byte(n), nil }

func numbers(v any) (any, error) {
	switch x := v.(type) {
	case json.Number:
		s := string(x)
		sign := ""
		if strings.HasPrefix(s, "-") {
			sign = "-"
			s = s[1:]
		}
		exponent := int64(0)
		if i := strings.IndexAny(s, "eE"); i >= 0 {
			var err error
			exponent, err = strconv.ParseInt(s[i+1:], 10, 32)
			if err != nil || exponent > 131072 || exponent < -16383 {
				return nil, fmt.Errorf("JSON number exponent outside PostgreSQL numeric range")
			}
			s = s[:i]
		}
		if i := strings.IndexByte(s, '.'); i >= 0 {
			exponent -= int64(len(s) - i - 1)
			s = s[:i] + s[i+1:]
		}
		s = strings.TrimLeft(s, "0")
		if s == "" {
			return number("0"), nil
		}
		trimmed := strings.TrimRight(s, "0")
		exponent += int64(len(s) - len(trimmed))
		s = trimmed
		if exponent != 0 {
			s += "e" + strconv.FormatInt(exponent, 10)
		}
		return number(sign + s), nil
	case []any:
		for i, item := range x {
			n, err := numbers(item)
			if err != nil {
				return nil, err
			}
			x[i] = n
		}
	case map[string]any:
		for k, item := range x {
			n, err := numbers(item)
			if err != nil {
				return nil, err
			}
			x[k] = n
		}
	}
	return v, nil
}
