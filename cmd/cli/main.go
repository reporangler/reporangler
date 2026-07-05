// Command cli is the RepoRangler operator CLI (login, user/group/repo/token
// management, publish), built on cobra. The command tree lives in the app
// subpackage so it can be unit-tested; main just runs it and maps errors to a
// non-zero exit code.
package main

import (
	"os"

	"github.com/reporangler/reporangler/cmd/cli/app"
)

func main() {
	os.Exit(app.Execute())
}
