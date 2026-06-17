// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Inveniam Capital Partners

package main

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"strings"
	"text/tabwriter"

	mcpkeys "github.com/NVNM-Chain/nvnm-mcp-server/internal/mcp"
)

const defaultKeysFile = ".mcp-keys.json"

var (
	errClientExists  = errors.New("client already exists")
	errClientMissing = errors.New("client not found")
	errInvalidRole   = errors.New("invalid role; must be one of: reader, writer, admin, automation")
)

var validRoles = map[string]bool{
	"reader":     true,
	"writer":     true,
	"admin":      true,
	"automation": true,
}

var errUsage = errors.New("see usage")

func main() {
	if err := run(os.Args[1:]); err != nil {
		if !errors.Is(err, errUsage) {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
		}
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		usage()
		return errUsage
	}

	keysFile := os.Getenv("MCP_API_KEYS_FILE")
	if keysFile == "" {
		keysFile = defaultKeysFile
	}

	switch args[0] {
	case "create":
		return runCreate(keysFile, args[1:])
	case "disable":
		return runSetEnabled(keysFile, args[1:], false)
	case "enable":
		return runSetEnabled(keysFile, args[1:], true)
	case "set-roles":
		return runSetRoles(keysFile, args[1:])
	case "list":
		return listKeys(keysFile)
	default:
		usage()
		return errUsage
	}
}

func runCreate(keysFile string, args []string) error {
	if len(args) < 1 {
		fmt.Fprintf(os.Stderr,
			"usage: key-mgmt create <client-id> [--roles reader,writer,...]\n")
		return errUsage
	}
	rolesStr := parseFlag(args[1:], "--roles")
	roles, err := parseRoles(rolesStr)
	if err != nil {
		return err
	}
	return createKey(keysFile, args[0], roles)
}

func runSetEnabled(keysFile string, args []string, enabled bool) error {
	if len(args) < 1 {
		verb := "disable"
		if enabled {
			verb = "enable"
		}
		fmt.Fprintf(os.Stderr, "usage: key-mgmt %s <client-id>\n", verb)
		return errUsage
	}
	return setEnabled(keysFile, args[0], enabled)
}

func runSetRoles(keysFile string, args []string) error {
	if len(args) < 2 {
		fmt.Fprintf(os.Stderr,
			"usage: key-mgmt set-roles <client-id> <role1,role2,...|\"\">\n")
		fmt.Fprintf(os.Stderr, "  Pass empty string \"\" to remove all roles.\n")
		fmt.Fprintf(os.Stderr, "  Valid roles: reader, writer, admin, automation\n")
		return errUsage
	}
	roles, err := parseRoles(args[1])
	if err != nil {
		return err
	}
	return setRoles(keysFile, args[0], roles)
}

func usage() {
	fmt.Fprintf(os.Stderr, "usage: key-mgmt <command> [args]\n\n")
	fmt.Fprintf(os.Stderr, "commands:\n")
	fmt.Fprintf(os.Stderr, "  create <client-id> [--roles reader,writer,...]\n")
	fmt.Fprintf(os.Stderr, "                       Generate a new API key for a client\n")
	fmt.Fprintf(os.Stderr, "  disable <client-id>  Disable an existing API key\n")
	fmt.Fprintf(os.Stderr, "  enable <client-id>   Re-enable a disabled API key\n")
	fmt.Fprintf(os.Stderr, "  set-roles <client-id> <role1,role2,...|\"\">\n")
	fmt.Fprintf(os.Stderr, "                       Set RBAC roles for a client (reader/writer/admin/automation)\n")
	fmt.Fprintf(os.Stderr, "  list                 List all API keys and their status\n")
	fmt.Fprintf(os.Stderr, "\nSet MCP_API_KEYS_FILE to override the default path (%s).\n", defaultKeysFile)
}

func loadOrInit(path string) ([]mcpkeys.KeyEntry, error) {
	entries, err := mcpkeys.LoadKeysFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	return entries, nil
}

func parseFlag(args []string, flag string) string {
	for i, a := range args {
		if a == flag && i+1 < len(args) {
			return args[i+1]
		}
	}
	return ""
}

func parseRoles(rolesStr string) ([]string, error) {
	if rolesStr == "" {
		return nil, nil
	}
	parts := strings.Split(rolesStr, ",")
	roles := make([]string, 0, len(parts))
	for _, r := range parts {
		r = strings.TrimSpace(r)
		if r == "" {
			continue
		}
		if !validRoles[r] {
			return nil, fmt.Errorf("%q: %w", r, errInvalidRole)
		}
		roles = append(roles, r)
	}
	return roles, nil
}

func createKey(path, clientID string, roles []string) error {
	entries, err := loadOrInit(path)
	if err != nil {
		return err
	}

	for i := range entries {
		if entries[i].ID == clientID {
			return fmt.Errorf(
				"client %q (use 'enable' to re-activate a disabled key): %w",
				clientID, errClientExists,
			)
		}
	}

	key, err := mcpkeys.GenerateKey()
	if err != nil {
		return err
	}

	entries = append(entries, mcpkeys.NewKeyEntry(clientID, key, roles))

	if err := mcpkeys.SaveKeysFile(path, entries); err != nil {
		return err
	}

	fmt.Printf("Created key for %q:\n\n", clientID)
	fmt.Printf("  Bearer %s\n\n", key)
	if len(roles) > 0 {
		fmt.Printf("  Roles: %s\n", strings.Join(roles, ", "))
	}
	fmt.Printf("Store this key securely — it cannot be retrieved later.\n")
	fmt.Printf("Keys file: %s\n", path)
	return nil
}

func setRoles(path, clientID string, roles []string) error {
	entries, err := loadOrInit(path)
	if err != nil {
		return err
	}

	found := false
	for i := range entries {
		if entries[i].ID == clientID {
			entries[i].Roles = roles
			found = true
			break
		}
	}
	if !found {
		return fmt.Errorf("client %q: %w", clientID, errClientMissing)
	}

	if err := mcpkeys.SaveKeysFile(path, entries); err != nil {
		return err
	}

	if len(roles) == 0 {
		fmt.Printf("Client %q roles cleared (no RBAC enforcement).\n", clientID)
	} else {
		fmt.Printf("Client %q roles set to: %s\n", clientID, strings.Join(roles, ", "))
	}
	return nil
}

func setEnabled(path, clientID string, enabled bool) error {
	entries, err := loadOrInit(path)
	if err != nil {
		return err
	}

	found := false
	for i := range entries {
		if entries[i].ID == clientID {
			entries[i].Enabled = enabled
			found = true
			break
		}
	}
	if !found {
		return fmt.Errorf("client %q: %w", clientID, errClientMissing)
	}

	if err := mcpkeys.SaveKeysFile(path, entries); err != nil {
		return err
	}

	action := "disabled"
	if enabled {
		action = "enabled"
	}
	fmt.Printf("Client %q %s.\n", clientID, action)
	return nil
}

func listKeys(path string) error {
	entries, err := loadOrInit(path)
	if err != nil {
		return err
	}

	if len(entries) == 0 {
		fmt.Printf("No keys found in %s\n", path)
		return nil
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintf(w, "CLIENT ID\tSTATUS\tROLES\tCREATED\tKEY PREFIX\n")
	fmt.Fprintf(w, "---------\t------\t-----\t-------\t----------\n")
	for i := range entries {
		e := &entries[i]
		status := "enabled"
		if !e.Enabled {
			status = "disabled"
		}
		prefix := e.KeyPrefix
		if prefix == "" && e.Key != "" {
			// Pre-8.6 entry that has not yet been migrated. Render a
			// best-effort prefix from the raw key so listings remain
			// useful before the next migration cycle.
			prefix = e.Key
			if len(prefix) > 8 {
				prefix = prefix[:8] + "..."
			}
		}
		roles := strings.Join(e.Roles, ",")
		if roles == "" {
			roles = "(none)"
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n",
			e.ID,
			status,
			roles,
			e.CreatedAt.Format("2006-01-02"),
			prefix,
		)
	}
	return w.Flush()
}
