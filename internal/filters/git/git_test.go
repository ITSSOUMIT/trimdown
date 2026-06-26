package git

import (
	"strings"
	"testing"

	"github.com/itssoumit/trimdown/internal/ir"
)

func TestParseInvocation(t *testing.T) {
	cases := []struct {
		args       []string
		wantGlobal []string
		wantSub    string
		wantArgs   []string
	}{
		{[]string{"status"}, nil, "status", []string{}},
		{[]string{"-C", "/repo", "status", "-s"}, []string{"-C", "/repo"}, "status", []string{"-s"}},
		{[]string{"-c", "user.name=x", "log"}, []string{"-c", "user.name=x"}, "log", []string{}},
		{[]string{"--no-pager", "diff", "HEAD"}, []string{"--no-pager"}, "diff", []string{"HEAD"}},
		{[]string{"--git-dir=/tmp/.git", "branch"}, []string{"--git-dir=/tmp/.git"}, "branch", []string{}},
	}
	for _, c := range cases {
		inv := parseInvocation(c.args)
		if inv.sub != c.wantSub {
			t.Errorf("%v: sub=%q want %q", c.args, inv.sub, c.wantSub)
		}
		if strings.Join(inv.global, ",") != strings.Join(c.wantGlobal, ",") {
			t.Errorf("%v: global=%v want %v", c.args, inv.global, c.wantGlobal)
		}
	}
}

func TestParseStatus(t *testing.T) {
	in := "## main...origin/main [ahead 1, behind 2]\n M src/foo.go\nA  new.go\n?? junk.txt\n"
	rep := parseStatus(in)
	if rep.Summary != "2 changed, 1 untracked" {
		t.Fatalf("summary=%q", rep.Summary)
	}
	if !strings.Contains(rep.Text, "* main ↑1 ↓2") {
		t.Fatalf("branch header missing:\n%s", rep.Text)
	}
	if rep.Status != ir.StatusWarn {
		t.Fatalf("status=%v want warn", rep.Status)
	}
}

func TestParseStatusClean(t *testing.T) {
	rep := parseStatus("## main...origin/main\n")
	if rep.Summary != "clean" || rep.Status != ir.StatusOK {
		t.Fatalf("clean: summary=%q status=%v", rep.Summary, rep.Status)
	}
	if rep.Text != "* main" {
		t.Fatalf("text=%q", rep.Text)
	}
}

func TestFormatBranch(t *testing.T) {
	cases := map[string]string{
		"main...origin/main":                     "main",
		"main...origin/main [ahead 3]":           "main ↑3",
		"feat...origin/feat [behind 1]":          "feat ↓1",
		"main...origin/main [ahead 1, behind 2]": "main ↑1 ↓2",
		"HEAD (no branch)":                       "(detached)",
		"dev":                                    "dev",
	}
	for in, want := range cases {
		if got := formatBranch(in); got != want {
			t.Errorf("formatBranch(%q)=%q want %q", in, got, want)
		}
	}
}

func TestParseCommit(t *testing.T) {
	rep := parseCommit("[main abc1234def] fix the thing\n 1 file changed, 2 insertions(+)")
	if rep.Summary != "ok abc1234" {
		t.Fatalf("summary=%q want 'ok abc1234'", rep.Summary)
	}
	rep = parseCommit("[main (root-commit) deadbeef0] initial")
	if rep.Summary != "ok deadbee" {
		t.Fatalf("root-commit summary=%q", rep.Summary)
	}
}

func TestParsePush(t *testing.T) {
	if got := parsePush("Everything up-to-date").Summary; got != "ok (up-to-date)" {
		t.Fatalf("up-to-date=%q", got)
	}
	stderr := "Enumerating objects: 5, done.\nTo github.com:me/repo.git\n   abc123..def456  main -> main\n"
	if got := parsePush(stderr).Summary; got != "ok main" {
		t.Fatalf("push ref=%q want 'ok main'", got)
	}
}

func TestParsePull(t *testing.T) {
	if got := parsePull("Already up to date.\n", "").Summary; got != "ok (up-to-date)" {
		t.Fatalf("pull up-to-date=%q", got)
	}
	out := "Updating abc..def\nFast-forward\n 3 files changed, 10 insertions(+), 2 deletions(-)\n"
	if got := parsePull(out, "").Summary; got != "ok 3 files +10 -2" {
		t.Fatalf("pull stat=%q", got)
	}
}

func TestParseDiff(t *testing.T) {
	diff := `diff --git a/main.go b/main.go
index 111..222 100644
--- a/main.go
+++ b/main.go
@@ -1,3 +1,4 @@ func main()
 ctx := x
+	added := 1
-	removed := 2
 done()
`
	rep := parseDiff(diff)
	if rep.Summary != "1 files, +1 -1" {
		t.Fatalf("summary=%q", rep.Summary)
	}
	if !strings.Contains(rep.Text, "main.go | +1 -1") {
		t.Fatalf("stat missing:\n%s", rep.Text)
	}
	if !strings.Contains(rep.Text, "+\tadded := 1") {
		t.Fatalf("added line missing:\n%s", rep.Text)
	}
	if strings.Contains(rep.Text, "index 111") {
		t.Fatalf("metadata should be stripped:\n%s", rep.Text)
	}
}

func TestParseLogCaps(t *testing.T) {
	var lines []string
	for i := 0; i < 25; i++ {
		lines = append(lines, "abc123 commit subject")
	}
	rep := parseLog(strings.Join(lines, "\n"), invocation{})
	if rep.Summary != "25 commits" {
		t.Fatalf("summary=%q", rep.Summary)
	}
	if !strings.Contains(rep.Text, "+5 more commits") {
		t.Fatalf("expected cap note:\n%s", rep.Text)
	}
}

func TestBuildLogArgsInjectsFormat(t *testing.T) {
	got := buildLogArgs([]string{"-5"})
	if got[0] != "log" {
		t.Fatalf("first arg=%q", got[0])
	}
	joined := strings.Join(got, " ")
	if !strings.Contains(joined, "--pretty=format:") {
		t.Fatalf("format not injected: %v", got)
	}
	// user-provided format suppresses injection
	got = buildLogArgs([]string{"--oneline"})
	if strings.Contains(strings.Join(got, " "), "--pretty=format:") {
		t.Fatalf("should not inject when --oneline present: %v", got)
	}
}

func TestParseBranch(t *testing.T) {
	in := "* main\n  dev\n  remotes/origin/main\n  remotes/origin/HEAD -> origin/main\n"
	rep := parseBranch(in)
	if !strings.Contains(rep.Text, "* main") || !strings.Contains(rep.Text, "  dev") {
		t.Fatalf("branch text:\n%s", rep.Text)
	}
	if !strings.Contains(rep.Summary, "remote") {
		t.Fatalf("summary=%q", rep.Summary)
	}
	if strings.Contains(rep.Text, "origin/HEAD") {
		t.Fatalf("origin/HEAD should be dropped:\n%s", rep.Text)
	}
}
