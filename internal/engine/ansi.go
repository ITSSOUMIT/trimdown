package engine

import "regexp"

// ansiRE matches CSI escape sequences (colors, cursor moves) emitted by tools
// that detect a TTY. Compiled once at package load.
var ansiRE = regexp.MustCompile("\x1b\\[[0-9;?]*[ -/]*[@-~]")

// StripANSI removes ANSI escape sequences from s.
func StripANSI(s string) string {
	if !containsESC(s) {
		return s
	}
	return ansiRE.ReplaceAllString(s, "")
}

func containsESC(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] == 0x1b {
			return true
		}
	}
	return false
}
