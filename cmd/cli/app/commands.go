// Package app implements the RepoRangler operator CLI (login, user/group/repo/
// token management, publish) on top of cobra. It replaces the legacy PHP cli
// tool, folding in the documented fixes: health-check no longer needs a token,
// login no longer prints the token, any 2xx is treated as success, and errors
// surface as non-zero exit codes instead of mid-run os.Exit/die().
package app

import (
	"fmt"
	"os"
	"sort"

	"github.com/spf13/cobra"

	"github.com/reporangler/reporangler/internal/types"
)

// Execute builds the default app (real config path, real streams) and runs the
// command tree. It returns a non-zero exit code on error instead of aborting
// mid-command.
func Execute() int {
	a := &App{Out: os.Stdout, Err: os.Stderr}
	root := NewRootCmd(a)
	if err := root.Execute(); err != nil {
		fmt.Fprintln(a.Err, "error:", err)
		return 1
	}
	return 0
}

// NewRootCmd wires the whole command tree onto the given App. Config is loaded
// lazily in PersistentPreRunE (unless the App already carries one, as tests do).
func NewRootCmd(a *App) *cobra.Command {
	root := &cobra.Command{
		Use:           "reporangler",
		Short:         "RepoRangler operator CLI",
		SilenceUsage:  true,
		SilenceErrors: true,
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			return a.ensureConfig()
		},
	}
	if a.Out != nil {
		root.SetOut(a.Out)
	}
	if a.Err != nil {
		root.SetErr(a.Err)
	}
	for _, c := range subcommands(a) {
		root.AddCommand(c)
	}
	return root
}

// CommandNames returns the sorted set of registered top-level command names.
// Used by tests to assert the command table stays complete.
func CommandNames(root *cobra.Command) []string {
	var names []string
	for _, c := range root.Commands() {
		if c.Name() == "help" || c.Name() == "completion" {
			continue
		}
		names = append(names, c.Name())
	}
	sort.Strings(names)
	return names
}

func subcommands(a *App) []*cobra.Command {
	return []*cobra.Command{
		cmdHealthCheck(a),
		cmdLogin(a),
		cmdListUser(a),
		cmdCreateUser(a),
		cmdDeleteUser(a),
		cmdUserInfo(a),
		cmdListPackageGroup(a),
		cmdCreatePackageGroup(a),
		cmdDeletePackageGroup(a),
		cmdListRepository(a),
		cmdCreateRepository(a),
		cmdUpdateRepository(a),
		cmdDeleteRepository(a),
		cmdAddAccessToken(a),
		cmdListAccessToken(a),
		cmdRemoveAccessToken(a),
		cmdJoinPackageGroup(a),
		cmdLeavePackageGroup(a),
		cmdJoinRepository(a),
		cmdLeaveRepository(a),
		cmdProtectPackageGroup(a),
		cmdUnprotectPackageGroup(a),
		cmdPublish(a),
	}
}

// --- health-check ---

func cmdHealthCheck(a *App) *cobra.Command {
	return &cobra.Command{
		Use:   "health-check",
		Short: "Ping every configured service root (no login required)",
		RunE: func(cmd *cobra.Command, args []string) error {
			ep := a.Config.Endpoints
			targets := []struct{ name, url string }{
				{"auth", ep.Auth},
				{"metadata", ep.Metadata},
				{"php", ep.PHP},
				{"npm", ep.NPM},
				{"storage", ep.Storage},
			}
			var failed int
			for _, t := range targets {
				// withAuth=false — the health-check must never require a token
				// (fixes the legacy bug where it sent a bearer and 401'd).
				err := a.request(cmd.Context(), "GET", joinURL(t.url, "/"), nil, nil, false, nil)
				if err != nil {
					failed++
					fmt.Fprintf(a.Out, "%-9s FAIL  %s\n", t.name, err)
					continue
				}
				fmt.Fprintf(a.Out, "%-9s OK    %s\n", t.name, t.url)
			}
			if failed > 0 {
				return fmt.Errorf("%d of %d services unhealthy", failed, len(targets))
			}
			return nil
		},
	}
}

// --- login ---

func cmdLogin(a *App) *cobra.Command {
	var username, password string
	c := &cobra.Command{
		Use:   "login",
		Short: "Authenticate against auth-service and store the login token",
		RunE: func(cmd *cobra.Command, args []string) error {
			headers := map[string]string{
				"reporangler-login-type":     "database",
				"reporangler-login-username": username,
				"reporangler-login-password": password,
			}
			var user types.User
			// GET /login/api, credentials in headers. withAuth=false: login is
			// the one call that does not carry a bearer.
			if err := a.request(cmd.Context(), "GET", joinURL(a.Config.Endpoints.Auth, "/login/api"), nil, headers, false, &user); err != nil {
				return err
			}
			if user.Token == "" {
				return fmt.Errorf("login succeeded but no token was returned")
			}
			a.Config.LoginToken = user.Token
			a.Config.UserID = user.ID
			if _, err := SaveConfig(a.ConfigPath, *a.Config); err != nil {
				return fmt.Errorf("save config: %w", err)
			}
			// Never print the token (fixes the legacy secret leak).
			fmt.Fprintf(a.Out, "logged in as %s (user id %d)\n", user.Username, user.ID)
			return nil
		},
	}
	c.Flags().StringVar(&username, "username", "", "account username")
	c.Flags().StringVar(&password, "password", "", "account password")
	_ = c.MarkFlagRequired("username")
	_ = c.MarkFlagRequired("password")
	return c
}

// --- users (auth-service) ---

func cmdListUser(a *App) *cobra.Command {
	return &cobra.Command{
		Use:   "list-user",
		Short: "List users",
		RunE: func(cmd *cobra.Command, args []string) error {
			var out any
			if err := a.request(cmd.Context(), "GET", joinURL(a.Config.Endpoints.Auth, "/user"), nil, nil, true, &out); err != nil {
				return err
			}
			return a.printJSON(out)
		},
	}
}

func cmdCreateUser(a *App) *cobra.Command {
	var username, password, email string
	c := &cobra.Command{
		Use:   "create-user",
		Short: "Create a user",
		RunE: func(cmd *cobra.Command, args []string) error {
			body := map[string]any{"username": username, "password": password, "email": email}
			var out any
			if err := a.request(cmd.Context(), "POST", joinURL(a.Config.Endpoints.Auth, "/user"), body, nil, true, &out); err != nil {
				return err
			}
			return a.printJSON(out)
		},
	}
	c.Flags().StringVar(&username, "username", "", "new username")
	c.Flags().StringVar(&password, "password", "", "new password")
	c.Flags().StringVar(&email, "email", "", "new email")
	_ = c.MarkFlagRequired("username")
	_ = c.MarkFlagRequired("password")
	return c
}

func cmdDeleteUser(a *App) *cobra.Command {
	var id int64
	c := &cobra.Command{
		Use:   "delete-user",
		Short: "Delete a user by id",
		RunE: func(cmd *cobra.Command, args []string) error {
			url := joinURL(a.Config.Endpoints.Auth, fmt.Sprintf("/user/%d", id))
			if err := a.request(cmd.Context(), "DELETE", url, nil, nil, true, nil); err != nil {
				return err
			}
			fmt.Fprintf(a.Out, "deleted user %d\n", id)
			return nil
		},
	}
	c.Flags().Int64Var(&id, "id", 0, "user id")
	_ = c.MarkFlagRequired("id")
	return c
}

func cmdUserInfo(a *App) *cobra.Command {
	var id int64
	var username string
	c := &cobra.Command{
		Use:   "user-info",
		Short: "Show a user (by --id/--username, or the logged-in user)",
		RunE: func(cmd *cobra.Command, args []string) error {
			var url string
			switch {
			case id > 0:
				url = joinURL(a.Config.Endpoints.Auth, fmt.Sprintf("/user/%d", id))
			case username != "":
				url = joinURL(a.Config.Endpoints.Auth, "/user/"+username)
			default:
				// No selector: introspect the current login token.
				url = joinURL(a.Config.Endpoints.Auth, "/login/token")
			}
			var out any
			if err := a.request(cmd.Context(), "GET", url, nil, nil, true, &out); err != nil {
				return err
			}
			return a.printJSON(out)
		},
	}
	c.Flags().Int64Var(&id, "id", 0, "user id")
	c.Flags().StringVar(&username, "username", "", "username")
	return c
}

// --- package groups (metadata-service) ---

func cmdListPackageGroup(a *App) *cobra.Command {
	return &cobra.Command{
		Use:   "list-package-group",
		Short: "List package groups",
		RunE: func(cmd *cobra.Command, args []string) error {
			var out any
			if err := a.request(cmd.Context(), "GET", joinURL(a.Config.Endpoints.Metadata, "/package-group/"), nil, nil, true, &out); err != nil {
				return err
			}
			return a.printJSON(out)
		},
	}
}

func cmdCreatePackageGroup(a *App) *cobra.Command {
	var name string
	c := &cobra.Command{
		Use:   "create-package-group",
		Short: "Create a package group",
		RunE: func(cmd *cobra.Command, args []string) error {
			var out any
			if err := a.request(cmd.Context(), "POST", joinURL(a.Config.Endpoints.Metadata, "/package-group/"), map[string]any{"name": name}, nil, true, &out); err != nil {
				return err
			}
			return a.printJSON(out)
		},
	}
	c.Flags().StringVar(&name, "name", "", "package group name")
	_ = c.MarkFlagRequired("name")
	return c
}

func cmdDeletePackageGroup(a *App) *cobra.Command {
	var id int64
	c := &cobra.Command{
		Use:   "delete-package-group",
		Short: "Delete a package group by id",
		RunE: func(cmd *cobra.Command, args []string) error {
			url := joinURL(a.Config.Endpoints.Metadata, fmt.Sprintf("/package-group/%d", id))
			if err := a.request(cmd.Context(), "DELETE", url, nil, nil, true, nil); err != nil {
				return err
			}
			fmt.Fprintf(a.Out, "deleted package group %d\n", id)
			return nil
		},
	}
	c.Flags().Int64Var(&id, "id", 0, "package group id")
	_ = c.MarkFlagRequired("id")
	return c
}

// --- repositories (metadata-service) ---

func cmdListRepository(a *App) *cobra.Command {
	return &cobra.Command{
		Use:   "list-repository",
		Short: "List repositories",
		RunE: func(cmd *cobra.Command, args []string) error {
			var out any
			if err := a.request(cmd.Context(), "GET", joinURL(a.Config.Endpoints.Metadata, "/repository/"), nil, nil, true, &out); err != nil {
				return err
			}
			return a.printJSON(out)
		},
	}
}

func cmdCreateRepository(a *App) *cobra.Command {
	var name string
	c := &cobra.Command{
		Use:   "create-repository",
		Short: "Create a repository",
		RunE: func(cmd *cobra.Command, args []string) error {
			var out any
			if err := a.request(cmd.Context(), "POST", joinURL(a.Config.Endpoints.Metadata, "/repository/"), map[string]any{"name": name}, nil, true, &out); err != nil {
				return err
			}
			return a.printJSON(out)
		},
	}
	c.Flags().StringVar(&name, "name", "", "repository name")
	_ = c.MarkFlagRequired("name")
	return c
}

func cmdUpdateRepository(a *App) *cobra.Command {
	var id int64
	var name string
	c := &cobra.Command{
		Use:   "update-repository",
		Short: "Rename a repository by id",
		RunE: func(cmd *cobra.Command, args []string) error {
			url := joinURL(a.Config.Endpoints.Metadata, fmt.Sprintf("/repository/%d", id))
			var out any
			if err := a.request(cmd.Context(), "PUT", url, map[string]any{"name": name}, nil, true, &out); err != nil {
				return err
			}
			return a.printJSON(out)
		},
	}
	c.Flags().Int64Var(&id, "id", 0, "repository id")
	c.Flags().StringVar(&name, "name", "", "new repository name")
	_ = c.MarkFlagRequired("id")
	_ = c.MarkFlagRequired("name")
	return c
}

func cmdDeleteRepository(a *App) *cobra.Command {
	var id int64
	c := &cobra.Command{
		Use:   "delete-repository",
		Short: "Delete a repository by id",
		RunE: func(cmd *cobra.Command, args []string) error {
			url := joinURL(a.Config.Endpoints.Metadata, fmt.Sprintf("/repository/%d", id))
			if err := a.request(cmd.Context(), "DELETE", url, nil, nil, true, nil); err != nil {
				return err
			}
			fmt.Fprintf(a.Out, "deleted repository %d\n", id)
			return nil
		},
	}
	c.Flags().Int64Var(&id, "id", 0, "repository id")
	_ = c.MarkFlagRequired("id")
	return c
}

// --- access tokens (auth-service) ---

// resolveUserID falls back to the logged-in user's id when the flag is unset.
func (a *App) resolveUserID(flag int64) (int64, error) {
	if flag > 0 {
		return flag, nil
	}
	if a.Config.UserID > 0 {
		return a.Config.UserID, nil
	}
	return 0, fmt.Errorf("no user id: pass --user-id or log in first")
}

func cmdAddAccessToken(a *App) *cobra.Command {
	var userID int64
	c := &cobra.Command{
		Use:   "add-access-token",
		Short: "Create a long-lived access token for a user",
		RunE: func(cmd *cobra.Command, args []string) error {
			uid, err := a.resolveUserID(userID)
			if err != nil {
				return err
			}
			url := joinURL(a.Config.Endpoints.Auth, fmt.Sprintf("/access-token/%d", uid))
			var out any
			if err := a.request(cmd.Context(), "POST", url, map[string]any{}, nil, true, &out); err != nil {
				return err
			}
			return a.printJSON(out)
		},
	}
	c.Flags().Int64Var(&userID, "user-id", 0, "user id (defaults to logged-in user)")
	return c
}

func cmdListAccessToken(a *App) *cobra.Command {
	var userID int64
	c := &cobra.Command{
		Use:   "list-access-token",
		Short: "List a user's access tokens",
		RunE: func(cmd *cobra.Command, args []string) error {
			uid, err := a.resolveUserID(userID)
			if err != nil {
				return err
			}
			url := joinURL(a.Config.Endpoints.Auth, fmt.Sprintf("/access-token/%d", uid))
			var out any
			if err := a.request(cmd.Context(), "GET", url, nil, nil, true, &out); err != nil {
				return err
			}
			return a.printJSON(out)
		},
	}
	c.Flags().Int64Var(&userID, "user-id", 0, "user id (defaults to logged-in user)")
	return c
}

func cmdRemoveAccessToken(a *App) *cobra.Command {
	var userID, tokenID int64
	c := &cobra.Command{
		Use:   "remove-access-token",
		Short: "Delete one of a user's access tokens",
		RunE: func(cmd *cobra.Command, args []string) error {
			uid, err := a.resolveUserID(userID)
			if err != nil {
				return err
			}
			url := joinURL(a.Config.Endpoints.Auth, fmt.Sprintf("/access-token/%d/%d", uid, tokenID))
			if err := a.request(cmd.Context(), "DELETE", url, nil, nil, true, nil); err != nil {
				return err
			}
			fmt.Fprintf(a.Out, "removed access token %d for user %d\n", tokenID, uid)
			return nil
		},
	}
	c.Flags().Int64Var(&userID, "user-id", 0, "user id (defaults to logged-in user)")
	c.Flags().Int64Var(&tokenID, "token-id", 0, "access token id")
	_ = c.MarkFlagRequired("token-id")
	return c
}

// --- permissions (auth-service /permission/...) ---

// permissionCmd builds a command that POSTs to
// /permission/<entity>/<action> with the entity name (and, for the
// membership actions, a user id).
func permissionCmd(a *App, use, short, entity, action, flagName string, withUser bool) *cobra.Command {
	var value string
	var userID int64
	c := &cobra.Command{
		Use:   use,
		Short: short,
		RunE: func(cmd *cobra.Command, args []string) error {
			body := map[string]any{entity: value}
			if withUser {
				uid, err := a.resolveUserID(userID)
				if err != nil {
					return err
				}
				body["user_id"] = uid
			}
			url := joinURL(a.Config.Endpoints.Auth, fmt.Sprintf("/permission/%s/%s", entity, action))
			var out any
			if err := a.request(cmd.Context(), "POST", url, body, nil, true, &out); err != nil {
				return err
			}
			if out == nil {
				fmt.Fprintf(a.Out, "%s %s ok\n", action, value)
				return nil
			}
			return a.printJSON(out)
		},
	}
	c.Flags().StringVar(&value, flagName, "", entity+" name")
	_ = c.MarkFlagRequired(flagName)
	if withUser {
		c.Flags().Int64Var(&userID, "user-id", 0, "user id (defaults to logged-in user)")
	}
	return c
}

func cmdJoinPackageGroup(a *App) *cobra.Command {
	return permissionCmd(a, "join-package-group", "Grant a user access to a package group", "package-group", "join", "package-group", true)
}
func cmdLeavePackageGroup(a *App) *cobra.Command {
	return permissionCmd(a, "leave-package-group", "Revoke a user's access to a package group", "package-group", "leave", "package-group", true)
}
func cmdJoinRepository(a *App) *cobra.Command {
	return permissionCmd(a, "join-repository", "Grant a user access to a repository", "repository", "join", "repository", true)
}
func cmdLeaveRepository(a *App) *cobra.Command {
	return permissionCmd(a, "leave-repository", "Revoke a user's access to a repository", "repository", "leave", "repository", true)
}
func cmdProtectPackageGroup(a *App) *cobra.Command {
	return permissionCmd(a, "protect-package-group", "Protect a package group", "package-group", "protect", "package-group", false)
}
func cmdUnprotectPackageGroup(a *App) *cobra.Command {
	return permissionCmd(a, "unprotect-package-group", "Unprotect a package group", "package-group", "unprotect", "package-group", false)
}

// --- publish ---

func cmdPublish(a *App) *cobra.Command {
	var repo, packageGroup, url string
	c := &cobra.Command{
		Use:   "publish",
		Short: "Publish a package to a repository facade (best-effort)",
		RunE: func(cmd *cobra.Command, args []string) error {
			base, err := a.publishBase(repo)
			if err != nil {
				return err
			}
			body := map[string]any{"package_group": packageGroup, "url": url}
			var out any
			if err := a.request(cmd.Context(), "POST", joinURL(base, "/publish"), body, nil, true, &out); err != nil {
				return err
			}
			if out == nil {
				fmt.Fprintf(a.Out, "published to %s\n", repo)
				return nil
			}
			return a.printJSON(out)
		},
	}
	c.Flags().StringVar(&repo, "repo", "", "target repository facade (php|npm)")
	c.Flags().StringVar(&packageGroup, "package-group", "", "package group to publish under")
	c.Flags().StringVar(&url, "url", "", "source URL of the package to ingest")
	_ = c.MarkFlagRequired("repo")
	_ = c.MarkFlagRequired("package-group")
	_ = c.MarkFlagRequired("url")
	return c
}

// publishBase resolves the facade base URL for a --repo value. Only php and npm
// facades are configured in the CLI config; others error out.
func (a *App) publishBase(repo string) (string, error) {
	switch repo {
	case "php":
		return a.Config.Endpoints.PHP, nil
	case "npm":
		return a.Config.Endpoints.NPM, nil
	default:
		return "", fmt.Errorf("unknown --repo %q: only php and npm facades are configured", repo)
	}
}
