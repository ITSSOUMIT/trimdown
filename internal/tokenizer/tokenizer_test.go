package tokenizer

import "testing"

func TestNaive(t *testing.T) {
	if got := (naive{}).Count(""); got != 0 {
		t.Fatalf("empty: got %d, want 0", got)
	}
	if got := (naive{}).Count("12345678"); got != 2 {
		t.Fatalf("8 chars: got %d, want 2", got)
	}
}

func TestDefaultBPE(t *testing.T) {
	tok := Default()
	if tok.Name() != "cl100k_base" {
		t.Fatalf("expected embedded BPE codec, got %q (offline load failed?)", tok.Name())
	}
	// "hello world" is 2 tokens in cl100k_base.
	if got := tok.Count("hello world"); got != 2 {
		t.Fatalf("hello world: got %d tokens, want 2", got)
	}
	if got := tok.Count(""); got != 0 {
		t.Fatalf("empty: got %d, want 0", got)
	}
}
