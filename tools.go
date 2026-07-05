//go:build tools

// This file pins build/runtime dependencies used by individual services so
// they're present in go.mod up front (and parallel work never needs to edit
// go.mod). It is excluded from normal builds by the `tools` build tag.
package tools

import (
	_ "github.com/spf13/cobra"  // cmd/cli
	_ "golang.org/x/mod/semver" // cmd/goproxy (Go semver ordering)
)
