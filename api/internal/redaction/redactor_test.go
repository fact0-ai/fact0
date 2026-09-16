package redaction

import "testing"

func TestRedact_PositiveCases(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want Kind
	}{
		{"aws_access_key", "creds: AKIAIOSFODNN7EXAMPLE done", KindAWSAccessKey},
		{"aws_secret_key", "secret: wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY xx", KindAWSSecretKey},
		{"github_pat", "tok: ghp_abcdefghijklmnopqrstuvwxyz0123456789 end", KindGitHubToken},
		{"slack_token", "slack: xoxb-1234567890-abcdef end", KindSlackToken},
		{"openai_key", "sk-proj-abcdefghijklmnopqrst123", KindOpenAIKey}, // gitleaks:allow synthetic redaction fixture
		{"jwt", "Authorization: eyJhbGciOi.eyJzdWIiOi.SignaturePart_ok end", KindJWT},
		{"email", "from alice@example.com to bob", KindEmail},
		{"ssn", "ssn 123-45-6789 logged", KindSSN},
		{"visa_test", "card 4111 1111 1111 1111 stored", KindCreditCard},
		{"bearer", "Authorization: Bearer abc.def_ghi-jkl", KindBearerToken},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			out, changed := Redact(c.in)
			if !changed {
				t.Fatalf("expected match for %s, got passthrough: %q", c.name, out)
			}
			marker := "[REDACTED:" + string(c.want) + "]"
			if !contains(out, marker) {
				t.Fatalf("expected %s marker in output, got %q", c.want, out)
			}
		})
	}
}

func TestRedact_NegativeCases(t *testing.T) {
	cases := []string{
		// SHA-256 hash - random hex, should not look like AWS secret.
		"f7c3bc1d808e04732adf679965ccc34ca7ae3441c1e9a9f1d9e9c8e8c8e8c8e8",
		// ULID - alphanumeric only, no /+, fails AWS heuristic.
		"01ARZ3NDEKTSV4RRFFQ69G5FAV",
		// Plain text.
		"the quick brown fox jumps over the lazy dog",
		// Card-shaped number that fails Luhn.
		"4111 1111 1111 1112",
		// Generic 9-digit number - should NOT be redacted (SSN needs dashes).
		"order id 123456789",
	}
	for _, in := range cases {
		out, changed := Redact(in)
		if changed {
			t.Errorf("expected no redaction for %q, got %q", in, out)
		}
	}
}

func TestRedact_PreservesNonMatchingContent(t *testing.T) {
	in := "user sk-test123notlongenough other stuff"
	out, _ := Redact(in)
	if !contains(out, "other stuff") {
		t.Fatalf("expected non-matching tail to be preserved, got %q", out)
	}
}

func TestRedactStringMap_MutatesInPlace(t *testing.T) {
	m := map[string]string{
		"query": "select * where user='alice@example.com'",
		"id":    "exec_01HXX",
	}
	changed := RedactStringMap(m)
	if !changed {
		t.Fatal("expected changed=true")
	}
	if !contains(m["query"], "[REDACTED:email]") {
		t.Errorf("expected email redacted, got %q", m["query"])
	}
	if m["id"] != "exec_01HXX" {
		t.Errorf("non-sensitive value mutated: %q", m["id"])
	}
}

func TestRedactJSONMap_Recurses(t *testing.T) {
	m := map[string]interface{}{
		"request": map[string]interface{}{
			"headers": []interface{}{
				"Authorization: Bearer abc.def-ghi",
				"Content-Type: application/json",
			},
			"body": map[string]interface{}{
				"email":  "x@y.com",
				"intval": float64(42),
			},
		},
	}
	if !RedactJSONMap(m) {
		t.Fatal("expected redactions")
	}
	body := m["request"].(map[string]interface{})["body"].(map[string]interface{})
	if !contains(body["email"].(string), "[REDACTED:email]") {
		t.Errorf("nested email not redacted: %v", body["email"])
	}
	if body["intval"].(float64) != 42 {
		t.Errorf("non-string leaf mutated: %v", body["intval"])
	}
	headers := m["request"].(map[string]interface{})["headers"].([]interface{})
	if !contains(headers[0].(string), "[REDACTED:bearer_token]") {
		t.Errorf("bearer token not redacted: %v", headers[0])
	}
}

func TestLuhnValid(t *testing.T) {
	good := []string{"4111111111111111", "4111 1111 1111 1111", "5500-0000-0000-0004"}
	bad := []string{"4111111111111112", "abc", "12", "1234567890123456789012"}
	for _, g := range good {
		if !luhnValid(g) {
			t.Errorf("expected %q to pass Luhn", g)
		}
	}
	for _, b := range bad {
		if luhnValid(b) {
			t.Errorf("expected %q to fail Luhn", b)
		}
	}
}

func contains(s, sub string) bool {
	return len(sub) == 0 || indexOf(s, sub) >= 0
}

func indexOf(s, sub string) int {
	n, m := len(s), len(sub)
	if m == 0 {
		return 0
	}
	for i := 0; i+m <= n; i++ {
		if s[i:i+m] == sub {
			return i
		}
	}
	return -1
}
