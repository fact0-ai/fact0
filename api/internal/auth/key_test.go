package auth

import (
	"strings"
	"testing"
)

func TestGenerateKey_PrefixAndUniqueness(t *testing.T) {
	t.Parallel()
	seen := map[string]bool{}
	for i := 0; i < 100; i++ {
		k, _, err := GenerateKey("test_id")
		if err != nil {
			t.Fatalf("generate: %v", err)
		}
		if !strings.HasPrefix(k, KeyPrefixLive) {
			t.Fatalf("missing prefix: %q", k)
		}
		if seen[k] {
			t.Fatalf("duplicate key generated: %q", k)
		}
		seen[k] = true
	}
}

func TestHash_Stable(t *testing.T) {
	t.Parallel()
	h1 := Hash("alk_live_x")
	h2 := Hash("alk_live_x")
	if h1 != h2 {
		t.Fatal("hash not deterministic")
	}
	if Hash("a") == Hash("b") {
		t.Fatal("hash collisions for distinct inputs")
	}
	if got, want := len(Hash("anything")), 64; got != want {
		t.Fatalf("hash length = %d, want %d", got, want)
	}
}

func TestLooksLikeKey(t *testing.T) {
	t.Parallel()
	cases := map[string]bool{
		"":                                   false,
		"f0_live_":                           false,
		"alk_live_":                          false,
		"f0_live_short":                      false,
		"f0_live_abcdefghij":                 true,
		"alk_live_abcdefghij":                true,
		"sk_live_abcdefghij":                 false, // gitleaks:allow synthetic format fixture
		"f0_live_" + strings.Repeat("a", 40): true,
	}
	for in, want := range cases {
		if got := LooksLikeKey(in); got != want {
			t.Errorf("LooksLikeKey(%q) = %v, want %v", in, got, want)
		}
	}
}
