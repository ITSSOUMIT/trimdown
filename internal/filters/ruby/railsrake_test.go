package ruby

import (
	"strings"
	"testing"

	"github.com/itssoumit/trimdown/internal/engine"
	"github.com/itssoumit/trimdown/internal/ir"
	"github.com/itssoumit/trimdown/internal/registry"
)

func itemsToString(items []ir.Item) string {
	var b strings.Builder
	for _, it := range items {
		b.WriteString(it.Key)
		b.WriteString("=")
		b.WriteString(it.Val)
		b.WriteString("\n")
	}
	return b.String()
}

func TestRailsDBMigrate(t *testing.T) {
	out := `== 20230101120000 CreateUsers: migrating ======================================
-- create_table(:users)
   -> 0.0123s
== 20230101120000 CreateUsers: migrated (0.0150s) =============================

== 20230102120000 AddEmailToUsers: migrating ==================================
-- add_column(:users, :email, :string)
   -> 0.0040s
== 20230102120000 AddEmailToUsers: migrated (0.0050s) =========================`
	rep, _ := rails{}.Parse(engine.CaptureResult{Stdout: out, ExitCode: 0}, registry.Opts{Args: []string{"db:migrate"}})
	if !rep.Filtered || rep.Status != ir.StatusOK {
		t.Fatalf("filtered/status: %+v", rep)
	}
	if rep.Summary != "ok db:migrate: 2 migrations" {
		t.Fatalf("summary=%q", rep.Summary)
	}
	if len(rep.Items) != 2 {
		t.Fatalf("items=%v", itemsToString(rep.Items))
	}
	if rep.Items[0].Key != "CreateUsers" || !strings.Contains(rep.Items[0].Val, "0.0150s") {
		t.Fatalf("item0=%+v", rep.Items[0])
	}
	// "--" / "->" detail noise must be dropped.
	if strings.Contains(itemsToString(rep.Items), "create_table") {
		t.Fatalf("detail noise leaked: %v", itemsToString(rep.Items))
	}
}

func TestRakeDBMigrate(t *testing.T) {
	out := `== 20230101120000 CreateWidgets: migrating ====================================
-- create_table(:widgets)
   -> 0.0100s
== 20230101120000 CreateWidgets: migrated (0.0110s) ===========================`
	rep, _ := rake{}.Parse(engine.CaptureResult{Stdout: out, ExitCode: 0}, registry.Opts{Args: []string{"db:migrate"}})
	if !rep.Filtered || rep.Tool != "rake" {
		t.Fatalf("rep=%+v", rep)
	}
	if rep.Summary != "ok db:migrate: 1 migration" {
		t.Fatalf("summary=%q", rep.Summary)
	}
}

func TestRailsRoutes(t *testing.T) {
	out := `   Prefix Verb   URI Pattern               Controller#Action
    users GET    /users(.:format)          users#index
          POST   /users(.:format)          users#create
 new_user GET    /users/new(.:format)      users#new
     user GET    /users/:id(.:format)      users#show`
	rep, _ := rails{}.Parse(engine.CaptureResult{Stdout: out, ExitCode: 0}, registry.Opts{Args: []string{"routes"}})
	if !rep.Filtered {
		t.Fatalf("not filtered: %+v", rep)
	}
	if rep.Summary != "4 routes" {
		t.Fatalf("summary=%q", rep.Summary)
	}
	if len(rep.Items) != 4 {
		t.Fatalf("items=%v", itemsToString(rep.Items))
	}
	if rep.Items[0].Key != "GET /users(.:format)" || rep.Items[0].Val != "users#index" {
		t.Fatalf("item0=%+v", rep.Items[0])
	}
}

func TestRailsGenerate(t *testing.T) {
	out := `      invoke  active_record
      create    db/migrate/20230101_create_posts.rb
      create    app/models/post.rb
      invoke    test_unit
      create      test/models/post_test.rb
      conflict  app/models/post.rb`
	rep, _ := rails{}.Parse(engine.CaptureResult{Stdout: out, ExitCode: 0}, registry.Opts{Args: []string{"generate", "model", "Post"}})
	if !rep.Filtered {
		t.Fatalf("not filtered: %+v", rep)
	}
	if !strings.Contains(rep.Summary, "3 files created") || !strings.Contains(rep.Summary, "1 conflicts") {
		t.Fatalf("summary=%q", rep.Summary)
	}
	if rep.Status != ir.StatusWarn {
		t.Fatalf("status=%v (conflict should warn)", rep.Status)
	}
}

func TestRakeTasks(t *testing.T) {
	out := `rake db:migrate        # Migrate the database
rake db:rollback       # Roll back the schema
rake routes            # List routes
rake test              # Run tests`
	rep, _ := rake{}.Parse(engine.CaptureResult{Stdout: out, ExitCode: 0}, registry.Opts{Args: []string{"-T"}})
	if !rep.Filtered || rep.Summary != "4 tasks" {
		t.Fatalf("summary=%q rep=%+v", rep.Summary, rep)
	}
	if len(rep.Items) != 4 || rep.Items[0].Key != "db:migrate" || rep.Items[0].Val != "Migrate the database" {
		t.Fatalf("items=%v", itemsToString(rep.Items))
	}
}

func TestZeitwerkGood(t *testing.T) {
	out := "Hold on, I am eager loading the application.\nAll is good!"
	rep, _ := rails{}.Parse(engine.CaptureResult{Stdout: out, ExitCode: 0}, registry.Opts{Args: []string{"zeitwerk:check"}})
	if !rep.Filtered || rep.Status != ir.StatusOK || !strings.Contains(rep.Summary, "all good") {
		t.Fatalf("rep=%+v", rep)
	}
}

func TestZeitwerkBad(t *testing.T) {
	out := `Hold on, I am eager loading the application.
expected file app/models/user.rb to define constant User, but didn't`
	rep, _ := rails{}.Parse(engine.CaptureResult{Stdout: out, ExitCode: 1}, registry.Opts{Args: []string{"zeitwerk:check"}})
	if !rep.Filtered || rep.Status != ir.StatusFail {
		t.Fatalf("rep=%+v", rep)
	}
	if len(rep.Notes) == 0 || !strings.Contains(strings.Join(rep.Notes, " "), "User") {
		t.Fatalf("notes=%v", rep.Notes)
	}
}

func TestRubySyntaxCheckOK(t *testing.T) {
	rep, _ := rubyInterp{}.Parse(engine.CaptureResult{Stdout: "Syntax OK", ExitCode: 0}, registry.Opts{Args: []string{"-c", "foo.rb"}})
	if !rep.Filtered || rep.Status != ir.StatusOK || !strings.Contains(rep.Summary, "syntax OK") {
		t.Fatalf("rep=%+v", rep)
	}
}

func TestRubySyntaxCheckBad(t *testing.T) {
	out := "foo.rb:3: syntax error, unexpected end-of-input, expecting `end'"
	rep, _ := rubyInterp{}.Parse(engine.CaptureResult{Stderr: out, ExitCode: 1}, registry.Opts{Args: []string{"-c", "foo.rb"}})
	if !rep.Filtered || rep.Status != ir.StatusFail {
		t.Fatalf("rep=%+v", rep)
	}
	if len(rep.Diagnostics) == 0 {
		t.Fatalf("expected syntax error diagnostics: %+v", rep)
	}
}

func TestRubyScriptBacktrace(t *testing.T) {
	out := `Starting up
app/foo.rb:10:in 'bar': something went wrong (RuntimeError)
	from app/baz.rb:5:in 'qux'
	from script.rb:2:in '<main>'`
	rep, _ := rubyInterp{}.Parse(engine.CaptureResult{Stderr: out, ExitCode: 1}, registry.Opts{Args: []string{"script.rb"}})
	if !rep.Filtered {
		t.Fatalf("backtrace must be compacted, got passthrough: %+v", rep)
	}
	if rep.Summary != "RuntimeError: something went wrong" {
		t.Fatalf("summary=%q", rep.Summary)
	}
	if len(rep.Diagnostics) != 1 || rep.Diagnostics[0].Rule != "RuntimeError" {
		t.Fatalf("diags=%+v", rep.Diagnostics)
	}
	ctx := strings.Join(rep.Diagnostics[0].Context, " | ")
	if !strings.Contains(ctx, "app/foo.rb:10") || !strings.Contains(ctx, "app/baz.rb:5") {
		t.Fatalf("frames=%q", ctx)
	}
}

func TestRubyScriptClean(t *testing.T) {
	out := "hello world\nall done\n"
	rep, _ := rubyInterp{}.Parse(engine.CaptureResult{Stdout: out, ExitCode: 0}, registry.Opts{Args: []string{"script.rb"}})
	if rep.Filtered {
		t.Fatalf("clean run should pass through, got filtered: %+v", rep)
	}
}

func TestRailsRunnerPassthrough(t *testing.T) {
	rep, _ := rails{}.Parse(engine.CaptureResult{Stdout: "arbitrary output", ExitCode: 0}, registry.Opts{Args: []string{"runner", "puts 1"}})
	if rep.Filtered {
		t.Fatalf("rails runner should pass through: %+v", rep)
	}
}

func TestRailsCredentialsPassthrough(t *testing.T) {
	rep, _ := rails{}.Parse(engine.CaptureResult{Stdout: "secret_key_base: abc123", ExitCode: 0}, registry.Opts{Args: []string{"credentials:show"}})
	if rep.Filtered {
		t.Fatalf("credentials should pass through (sensitive): %+v", rep)
	}
}

func TestRakeUnknownTaskPassthrough(t *testing.T) {
	rep, _ := rake{}.Parse(engine.CaptureResult{Stdout: "did something custom", ExitCode: 0}, registry.Opts{Args: []string{"my:custom:task"}})
	if rep.Filtered {
		t.Fatalf("unknown rake task should pass through: %+v", rep)
	}
}

func TestRakeTestStillMinitest(t *testing.T) {
	out := "Finished in 0.1s\n3 runs, 4 assertions, 0 failures, 0 errors, 0 skips"
	rep, _ := rake{}.Parse(engine.CaptureResult{Stdout: out, ExitCode: 0}, registry.Opts{Args: []string{"test"}})
	if !rep.Filtered || rep.Summary != "3 runs, 0 failures" {
		t.Fatalf("rake test not minitest: %+v", rep)
	}
}

func TestRailsLookupResolution(t *testing.T) {
	// `rails test` must resolve to railsTest, other rails subcommands to rails.
	f, ok := registry.Lookup("rails", []string{"test"})
	if !ok {
		t.Fatal("rails test not found")
	}
	if _, isTest := f.(railsTest); !isTest {
		t.Fatalf("rails test resolved to %T, want railsTest", f)
	}
	f, ok = registry.Lookup("rails", []string{"db:migrate"})
	if !ok {
		t.Fatal("rails db:migrate not found")
	}
	if _, isRails := f.(rails); !isRails {
		t.Fatalf("rails db:migrate resolved to %T, want rails", f)
	}
	if _, ok := registry.Lookup("ruby", []string{"script.rb"}); !ok {
		t.Fatal("ruby not registered")
	}
}
