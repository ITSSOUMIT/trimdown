// Package tokenizer estimates how many LLM tokens a piece of text costs. The
// estimate is what `trimdown savings` reports, so it should be credible — a
// real BPE tokenizer by default, not rtk's crude len/4.
package tokenizer

import (
	"sync"

	"github.com/tiktoken-go/tokenizer"
)

// Tokenizer estimates the token count of a string.
type Tokenizer interface {
	Count(s string) int
	Name() string
}

// naive is the fallback: ~4 chars per token (rtk's heuristic). Only used if the
// BPE codec fails to load, which shouldn't happen since its tables are embedded.
type naive struct{}

func (naive) Name() string { return "naive" }
func (naive) Count(s string) int {
	if s == "" {
		return 0
	}
	return (len(s) + 3) / 4
}

// bpe wraps an embedded cl100k_base BPE codec (the encoding used by recent
// OpenAI/Claude-class tokenizers — close enough for cross-model estimates).
type bpe struct{ codec tokenizer.Codec }

func (bpe) Name() string { return "cl100k_base" }
func (b bpe) Count(s string) int {
	if s == "" {
		return 0
	}
	ids, _, err := b.codec.Encode(s)
	if err != nil {
		return naive{}.Count(s)
	}
	return len(ids)
}

var (
	defaultOnce sync.Once
	defaultTok  Tokenizer
)

// Default returns the process-wide default tokenizer (BPE if available, else
// naive). Loaded once and reused.
func Default() Tokenizer {
	defaultOnce.Do(func() {
		if codec, err := tokenizer.Get(tokenizer.Cl100kBase); err == nil {
			defaultTok = bpe{codec: codec}
		} else {
			defaultTok = naive{}
		}
	})
	return defaultTok
}
