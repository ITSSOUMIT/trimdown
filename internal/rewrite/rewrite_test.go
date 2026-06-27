package rewrite

import "testing"

// fixedTools is the covered-tool set used by the tests (independent of the
// registry, so these tests pin the safety boundary precisely).
var fixedTools = map[string]bool{
	"git": true, "go": true, "npm": true, "node": true, "pytest": true,
	"docker": true, "kubectl": true, "rake": true, "rails": true, "tail": true,
}

func isFixedTool(name string) bool { return fixedTools[name] }

func rw(cmd string) (string, bool) { return rewriteWith(cmd, isFixedTool) }

func TestWrapSimple(t *testing.T) {
	cases := map[string]string{
		"git status":              "trimdown git status",
		"go test ./...":           "trimdown go test ./...",
		"npm install":             "trimdown npm install",
		"pytest -q":               "trimdown pytest -q",
		"git commit -m 'wip'":     "trimdown git commit -m 'wip'",
		"git commit --amend -m x": "trimdown git commit --amend -m x",
	}
	for in, want := range cases {
		got, changed := rw(in)
		if !changed || got != want {
			t.Errorf("rw(%q) = %q,%v; want %q,true", in, got, changed, want)
		}
	}
}

func TestSkipUnsafe(t *testing.T) {
	// Every one of these must be left EXACTLY as-is (changed=false).
	skip := []string{
		"FILES=$(git diff --name-only)", // command substitution + assignment
		"echo $(git rev-parse HEAD)",    // substitution, and echo isn't a tool
		"git log | head",                // pipe feeding another command
		"git log | head -5",             //
		"cat foo | git apply",           // git is downstream of a pipe
		"git diff > out.txt",            // redirect to file
		"git diff >> out.txt",           // append redirect
		"git show < patch",              // input redirect
		"git log 2>&1",                  // fd redirect
		"VAR=1 git status",              // leading assignment (env-style)
		"sudo git status",               // command-runner prefix
		"env git status",                //
		"time git status",               //
		"git commit",                    // interactive editor (no message)
		"git commit --amend",            // interactive editor (amend, no -m)
		"git rebase -i HEAD~3",          // interactive rebase
		"git add -p",                    // interactive add
		"git mergetool",                 //
		"docker run -it ubuntu bash",    // interactive TTY
		"docker run -i -t ubuntu",       // split -i -t
		"kubectl exec -it pod -- sh",    // interactive
		"tail -f log.txt",               // streaming follow
		"kubectl logs -f pod",           // streaming follow
		"trimdown git status",           // already wrapped (idempotent)
		"cargo build",                   // unknown tool
		"ls -la",                        // unknown tool
		"`git status`",                  // backtick substitution
		"if true; then ok; fi",          // control flow keywords
		"",                              // empty
		"   ",                           // whitespace only
	}
	for _, in := range skip {
		got, changed := rw(in)
		if changed || got != in {
			t.Errorf("rw(%q) should be unchanged; got %q,%v", in, got, changed)
		}
	}
}

func TestChainsWrapOnlyEligibleSegments(t *testing.T) {
	cases := map[string]string{
		"cat x && git status":             "cat x && trimdown git status",
		"git status && git log":           "trimdown git status && trimdown git log",
		"git status; npm install":         "trimdown git status; trimdown npm install",
		"git status || echo fail":         "trimdown git status || echo fail",
		"mkdir d && cd d && git init":     "mkdir d && cd d && trimdown git init",
		"git fetch && git rebase -i main": "trimdown git fetch && git rebase -i main", // rebase -i stays raw
	}
	for in, want := range cases {
		got, changed := rw(in)
		if got != want {
			t.Errorf("rw(%q) = %q (changed=%v); want %q", in, got, changed, want)
		}
		if got != in && !changed {
			t.Errorf("rw(%q): changed should be true", in)
		}
	}
}

func TestSpacingPreserved(t *testing.T) {
	in := "git status   &&   go test ./..."
	want := "trimdown git status   &&   trimdown go test ./..."
	if got, _ := rw(in); got != want {
		t.Errorf("spacing not preserved: got %q want %q", got, want)
	}
}

func TestPipeInsideSubstitutionNotSplit(t *testing.T) {
	// The pipe is inside $( ), so the whole thing is one segment; echo isn't a
	// tool, so nothing is wrapped.
	in := "echo $(git log | head)"
	if got, changed := rw(in); changed || got != in {
		t.Errorf("rw(%q) = %q,%v; want unchanged", in, got, changed)
	}
}

func TestParamExpansionAllowed(t *testing.T) {
	// ${VAR} is parameter expansion (safe), unlike $( ) — should still wrap.
	in := "git -C ${DIR} status"
	want := "trimdown git -C ${DIR} status"
	if got, changed := rw(in); !changed || got != want {
		t.Errorf("rw(%q) = %q,%v; want %q,true", in, got, changed, want)
	}
}

func TestIdempotent(t *testing.T) {
	once, _ := rw("git status && go build")
	twice, changed := rw(once)
	if changed || twice != once {
		t.Errorf("rewrite not idempotent: %q -> %q (changed=%v)", once, twice, changed)
	}
}
