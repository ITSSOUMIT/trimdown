// Command trimdown is a CLI proxy that compresses tool output to cut LLM token use.
package main

import (
	"os"

	"github.com/itssoumit/trimdown/internal/dispatch"

	// Register all filters via their init() side effects.
	_ "github.com/itssoumit/trimdown/internal/allfilters"
)

// Go's runtime already terminates the process on SIGPIPE when writing to a
// closed stdout/stderr (e.g. `trimdown git log | head`), so unlike rtk (Rust
// ignores SIGPIPE) we need no explicit handler here.
func main() {
	os.Exit(dispatch.Main(os.Args[1:]))
}
