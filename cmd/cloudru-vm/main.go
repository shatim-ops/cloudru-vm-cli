// Command cloudru-vm is the legacy build path of evo, kept until v1.1 for scripts that run "go build ./cmd/cloudru-vm"; the binary prints the rename notice and works as evo; the code is in internal/cli.
package main

import "github.com/shatim-ops/cloudru-vm-cli/internal/cli"

// Build identity, set by the release build:
//
//	-ldflags "-X main.version=1.2.3 -X main.commit=<sha> -X main.date=<RFC 3339>"
var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

func main() { cli.Main(version, commit, date) }
