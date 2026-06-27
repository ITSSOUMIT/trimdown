// Package rewrite turns a shell command line into one where covered tools are
// prefixed with `trimdown`, so an agent's commands compact transparently. It is
// the single source of truth for the safety boundary: it wraps ONLY a top-level
// simple command whose first word is a registered tool, and never touches
// anything inside a substitution, pipe, redirect, assignment, control-flow
// keyword, or a known-interactive invocation. When in doubt it leaves the
// segment untouched — false negatives are safe, false positives are not.
package rewrite

import (
	"strings"
	"sync"

	"github.com/itssoumit/trimdown/internal/registry"
)

// Rewrite rewrites a full command line, wrapping eligible top-level segments.
// It returns the (possibly unchanged) command and whether anything changed.
func Rewrite(cmdline string) (string, bool) {
	return rewriteWith(cmdline, registryHasTool)
}

// registryTools is the set of registered tool names, built lazily on first use
// (after all filter init() functions have run in the real binary).
var registryTools = sync.OnceValue(func() map[string]bool {
	m := map[string]bool{}
	for _, t := range registry.RegisteredTools() {
		m[t] = true
	}
	return m
})

func registryHasTool(name string) bool { return registryTools()[name] }

// rewriteWith is the testable core: isTool decides whether a first word is a
// covered tool, so unit tests can supply a fixed set without the registry.
func rewriteWith(cmdline string, isTool func(string) bool) (string, bool) {
	segs := splitTopLevel(cmdline)
	changed := false
	var b strings.Builder
	for _, sg := range segs {
		text := sg.text
		if !sg.piped {
			if rw, ok := wrapSegment(text, isTool); ok {
				text = rw
				changed = true
			}
		}
		b.WriteString(text)
		b.WriteString(sg.sep)
	}
	if !changed {
		return cmdline, false
	}
	return b.String(), true
}

// segment is a top-level command piece plus the operator that followed it.
// Spacing around operators stays inside text, so segs reassemble losslessly.
type segment struct {
	text  string
	sep   string // "&&", "||", ";", "|", or "" (last segment)
	piped bool   // part of a pipeline → never wrapped
}

// splitTopLevel splits on top-level &&, ||, ;, and | while respecting single/
// double quotes, backslash escapes, backticks, and ( … ) / $( … ) nesting so
// operators inside substitutions or quotes never split a segment.
func splitTopLevel(s string) []segment {
	var segs []segment
	var cur strings.Builder
	var inS, inD, bt bool
	depth := 0

	push := func(sep string) {
		segs = append(segs, segment{text: cur.String(), sep: sep})
		cur.Reset()
	}

	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case inS:
			cur.WriteByte(c)
			if c == '\'' {
				inS = false
			}
		case inD:
			cur.WriteByte(c)
			if c == '\\' && i+1 < len(s) {
				i++
				cur.WriteByte(s[i])
			} else if c == '"' {
				inD = false
			}
		case bt:
			cur.WriteByte(c)
			if c == '`' {
				bt = false
			}
		case c == '\'':
			inS = true
			cur.WriteByte(c)
		case c == '"':
			inD = true
			cur.WriteByte(c)
		case c == '`':
			bt = true
			cur.WriteByte(c)
		case c == '\\':
			cur.WriteByte(c)
			if i+1 < len(s) {
				i++
				cur.WriteByte(s[i])
			}
		case c == '(':
			depth++
			cur.WriteByte(c)
		case c == ')':
			if depth > 0 {
				depth--
			}
			cur.WriteByte(c)
		case c == '$':
			cur.WriteByte(c)
			if i+1 < len(s) && s[i+1] == '(' {
				depth++
				i++
				cur.WriteByte('(')
			}
		case c == '&' && depth == 0 && i+1 < len(s) && s[i+1] == '&':
			push("&&")
			i++
		case c == '|' && depth == 0:
			if i+1 < len(s) && s[i+1] == '|' {
				push("||")
				i++
			} else {
				push("|")
			}
		case c == ';' && depth == 0:
			push(";")
		default:
			cur.WriteByte(c)
		}
	}
	push("")

	for i := range segs {
		if segs[i].sep == "|" {
			segs[i].piped = true
			if i+1 < len(segs) {
				segs[i+1].piped = true
			}
		}
	}
	return segs
}

// wrapSegment prepends `trimdown ` to a single segment if it is an eligible
// top-level simple command; otherwise it returns the segment unchanged.
func wrapSegment(text string, isTool func(string) bool) (string, bool) {
	trimmedL := strings.TrimLeft(text, " \t")
	prefix := text[:len(text)-len(trimmedL)]
	body := strings.TrimRight(trimmedL, " \t")
	trailing := trimmedL[len(body):]
	if body == "" {
		return "", false
	}
	if containsUnsafe(body) {
		return "", false
	}
	words := shellWords(body)
	if len(words) == 0 {
		return "", false
	}
	first := words[0]
	switch {
	case first == "trimdown": // already wrapped (idempotent)
		return "", false
	case isAssignment(first): // VAR=val …
		return "", false
	case controlKeyword[first]: // control flow / command-runner prefix
		return "", false
	case !isTool(first): // not a tool we compact
		return "", false
	case isInteractive(words): // would need a TTY / editor
		return "", false
	}
	return prefix + "trimdown " + body + trailing, true
}

// containsUnsafe reports whether a segment contains an unquoted construct that
// makes wrapping unsafe: command substitution `$(`, backticks, or a redirect
// (`<`/`>`, which also covers heredocs `<<`). Parameter expansion `${…}` and
// `$VAR` are safe and allowed.
func containsUnsafe(s string) bool {
	var inS, inD bool
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case inS:
			if c == '\'' {
				inS = false
			}
		case inD:
			if c == '\\' && i+1 < len(s) {
				i++
			} else if c == '"' {
				inD = false
			}
		case c == '\'':
			inS = true
		case c == '"':
			inD = true
		case c == '`':
			return true
		case c == '<', c == '>':
			return true
		case c == '$':
			if i+1 < len(s) && s[i+1] == '(' {
				return true
			}
		}
	}
	return false
}

// shellWords splits a segment into words on unquoted whitespace, honoring
// single and double quotes (quote characters are stripped from the words).
func shellWords(s string) []string {
	var words []string
	var cur strings.Builder
	var inS, inD, started bool
	flush := func() {
		if started {
			words = append(words, cur.String())
			cur.Reset()
			started = false
		}
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case inS:
			started = true
			if c == '\'' {
				inS = false
			} else {
				cur.WriteByte(c)
			}
		case inD:
			started = true
			if c == '\\' && i+1 < len(s) {
				i++
				cur.WriteByte(s[i])
			} else if c == '"' {
				inD = false
			} else {
				cur.WriteByte(c)
			}
		case c == '\'':
			inS, started = true, true
		case c == '"':
			inD, started = true, true
		case c == ' ' || c == '\t':
			flush()
		case c == '\\' && i+1 < len(s):
			i++
			cur.WriteByte(s[i])
			started = true
		default:
			cur.WriteByte(c)
			started = true
		}
	}
	flush()
	return words
}

// isAssignment reports whether a word is a `NAME=value` assignment prefix.
func isAssignment(w string) bool {
	eq := strings.IndexByte(w, '=')
	if eq <= 0 {
		return false
	}
	for i := 0; i < eq; i++ {
		c := w[i]
		ok := c == '_' || (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') || (i > 0 && c >= '0' && c <= '9')
		if !ok {
			return false
		}
	}
	return true
}

// controlKeyword holds shell keywords and command-runner prefixes after which a
// real command follows — wrapping these is complex, so we skip the segment.
var controlKeyword = map[string]bool{
	"if": true, "then": true, "else": true, "elif": true, "fi": true,
	"for": true, "while": true, "until": true, "do": true, "done": true,
	"case": true, "esac": true, "select": true, "function": true, "in": true,
	"{": true, "}": true, "!": true, "[": true, "[[": true, "coproc": true,
	"time": true, "exec": true, "eval": true, "command": true, "builtin": true,
	"source": true, ".": true, "sudo": true, "doas": true, "env": true,
	"nohup": true, "xargs": true, "timeout": true, "nice": true, "ionice": true,
	"stdbuf": true, "watch": true,
}

// IsInteractive reports whether running `tool args…` needs a TTY/editor (an
// editor-opening commit, an interactive rebase, `-it` containers, a `-f` log
// follow, …) and so must run with inherited stdio rather than being captured.
// Shared by the hook rewriter and the direct-run guard.
func IsInteractive(tool string, args []string) bool {
	return isInteractive(append([]string{tool}, args...))
}

// isInteractive reports whether an invocation needs a TTY/editor and so must
// not be captured. Bias is toward true (skip) when uncertain.
func isInteractive(words []string) bool {
	if len(words) == 0 {
		return false
	}
	tool := words[0]
	args := words[1:]
	hasFlag := func(fs ...string) bool {
		for _, a := range args {
			for _, f := range fs {
				if a == f {
					return true
				}
			}
		}
		return false
	}
	sub := firstNonFlag(args)

	switch tool {
	case "git":
		switch sub {
		case "commit":
			return !gitCommitHasMessage(args)
		case "rebase":
			return hasFlag("-i", "--interactive")
		case "add":
			return hasFlag("-p", "--patch", "-i", "--interactive")
		case "mergetool", "difftool", "gui", "citool":
			return true
		}
	case "docker", "podman", "kubectl", "oc":
		if hasFlag("-it", "-ti") || (hasFlag("-i") && hasFlag("-t")) {
			return true
		}
		for _, a := range args {
			if len(a) > 1 && a[0] == '-' && a[1] != '-' &&
				strings.ContainsRune(a, 'i') && strings.ContainsRune(a, 't') {
				return true
			}
		}
	}
	// Streaming follow → never finishes under capture.
	if (sub == "logs" || tool == "tail") && hasFlag("-f", "--follow") {
		return true
	}
	return false
}

// gitCommitHasMessage reports whether a `git commit` invocation supplies a
// message or skips the editor (so it won't block on one).
func gitCommitHasMessage(args []string) bool {
	for _, a := range args {
		switch {
		case a == "-m" || a == "--message" || a == "-F" || a == "--file" ||
			a == "-C" || a == "--reuse-message" || a == "--no-edit":
			return true
		case strings.HasPrefix(a, "--message=") || strings.HasPrefix(a, "--file="):
			return true
		case len(a) > 1 && a[0] == '-' && a[1] != '-' && strings.ContainsRune(a, 'm'):
			return true // combined short cluster, e.g. -am
		}
	}
	return false
}

// firstNonFlag returns the first word that doesn't start with '-'.
func firstNonFlag(args []string) string {
	for _, a := range args {
		if a != "" && a[0] != '-' {
			return a
		}
	}
	return ""
}
