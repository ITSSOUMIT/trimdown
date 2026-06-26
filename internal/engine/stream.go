package engine

import (
	"bufio"
	"io"
	"strings"
)

// ScanLines calls fn for each line of s (newline stripped). It's the simple,
// bounded-memory primitive most parsers use. For truly large/streaming output,
// filters can wire a pipe to ScanReader instead.
func ScanLines(s string, fn func(line string)) {
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			line := s[start:i]
			fn(strings.TrimSuffix(line, "\r"))
			start = i + 1
		}
	}
	if start < len(s) {
		fn(strings.TrimSuffix(s[start:], "\r"))
	}
}

// ScanReader streams lines from r, raising the token-size cap so single very
// long lines (e.g. minified JSON) don't break scanning.
func ScanReader(r io.Reader, fn func(line string)) error {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), MaxCapture)
	for sc.Scan() {
		fn(sc.Text())
	}
	return sc.Err()
}
