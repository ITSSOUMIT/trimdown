package meta

import (
	"fmt"
	"os"
	"time"

	"github.com/itssoumit/trimdown/internal/engine"
	"github.com/itssoumit/trimdown/internal/run"
)

// Passthrough runs a command unfiltered (inherited stdio), records a usage
// event (0% savings, so it shows up in `trimdown savings` without diluting the
// rate), and returns its exit code.
func Passthrough(args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "trimdown passthrough: need a command to run")
		return 1
	}
	start := time.Now()
	code := engine.Passthrough(args[0], args[1:])
	run.RecordPassthrough(args[0], args[1:], time.Since(start))
	return code
}
