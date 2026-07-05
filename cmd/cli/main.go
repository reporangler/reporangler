// Command cli is the RepoRangler operator CLI (login, user/group/repo/token
// management, publish). Target: cobra + viper. Scaffold: prints usage.
package main

import (
	"fmt"
	"os"
)

func main() {
	fmt.Fprintln(os.Stderr, "reporangler (scaffold) — CLI commands coming soon")
	fmt.Fprintln(os.Stderr, "planned: login, list/create/delete-user, *-package-group, *-repository, publish")
}
