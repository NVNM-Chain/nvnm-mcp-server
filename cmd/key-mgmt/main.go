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
	"time"

	"github.com/NVNM-Chain/nvnm-mcp-server/internal/auth"
	"github.com/NVNM-Chain/nvnm-mcp-server/internal/config"
	mcpkeys "github.com/NVNM-Chain/nvnm-mcp-server/internal/mcp"
)

const defaultKeysFile = ".mcp-keys.json"

var (
	errClientExists  = errors.New("client already exists")
	errClientMissing = errors.New("client not found")
	errInvalidRole   = errors.New("invalid role; must be one of: reader, writer, admin, automation")
	// errNoRoles is returned by runCreate when the caller omits --roles.
	// Default-deny makes a roleless key inert, so issuance must assign a role.
	errNoRoles = errors.New(
		"at least one role is required (--roles reader,writer,...); " +
			"a key with no roles authorizes nothing",
	)
	// errRenewTTLRequired is returned by runRenew when --ttl is omitted.
	// Renew does not apply a default TTL; the operator must be explicit.
	errRenewTTLRequired = errors.New("--ttl is required for renew")
	// errTTLMustBePositive is returned by resolveCLIExpiry when --ttl parses
	// to a non-positive duration (e.g. --ttl -1h).
	errTTLMustBePositive = errors.New("--ttl must be positive")
)

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
	case "renew":
		return runRenew(keysFile, args[1:])
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
			"usage: key-mgmt create <client-id> [--roles reader,writer,...] [--ttl <dur|0>]\n")
		return errUsage
	}
	rolesStr := parseFlag(args[1:], "--roles")
	roles, err := parseRoles(rolesStr)
	if err != nil {
		return err
	}
	if len(roles) == 0 {
		return errNoRoles
	}

	defaultTTL, err := defaultKeyTTL()
	if err != nil {
		return err
	}
	ttlStr := parseFlag(args[1:], "--ttl")
	expiresAt, err := resolveCLIExpiry(ttlStr, defaultTTL, time.Now())
	if err != nil {
		return err
	}

	return createKey(keysFile, args[0], roles, expiresAt)
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
	fmt.Fprintf(os.Stderr, "  create <client-id> [--roles reader,writer,...] [--ttl <dur|0>]\n")
	fmt.Fprintf(os.Stderr, "                       Generate a new API key for a client\n")
	fmt.Fprintf(os.Stderr,
		"                       --ttl: key lifetime (e.g. 24h, 90d)."+
			" Default: KEY_DEFAULT_TTL (8760h).\n")
	fmt.Fprintf(os.Stderr, "                       Use --ttl 0 (or none/never) for no expiry.\n")
	fmt.Fprintf(os.Stderr, "  renew <client-id> --ttl <dur|0>\n")
	fmt.Fprintf(os.Stderr, "                       Update the expiry of an existing key\n")
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
		if !auth.IsValidRole(r) {
			return nil, fmt.Errorf("%q: %w", r, errInvalidRole)
		}
		roles = append(roles, r)
	}
	return roles, nil
}

// defaultKeyTTL reads KEY_DEFAULT_TTL from the environment (default "8760h")
// and returns it as a time.Duration. A zero duration means no expiry.
func defaultKeyTTL() (time.Duration, error) {
	s := os.Getenv("KEY_DEFAULT_TTL")
	if s == "" {
		s = "8760h"
	}
	d, err := time.ParseDuration(s)
	if err != nil {
		return 0, fmt.Errorf("invalid KEY_DEFAULT_TTL %q: %w", s, err)
	}
	return d, nil
}

// resolveCLIExpiry maps a CLI --ttl string to an absolute expiry time.
//
//   - ttlStr == ""               → now + defaultTTL (zero defaultTTL → no expiry)
//   - ttlStr in "0","none","never" → zero time.Time (no expiry)
//   - else                       → now + parsed duration; invalid → error
func resolveCLIExpiry(ttlStr string, defaultTTL time.Duration, now time.Time) (time.Time, error) {
	if ttlStr == "" {
		if defaultTTL == 0 {
			return time.Time{}, nil
		}
		return now.Add(defaultTTL), nil
	}
	switch ttlStr {
	case "0", "none", "never":
		return time.Time{}, nil
	}
	d, err := time.ParseDuration(ttlStr)
	if err != nil {
		return time.Time{}, fmt.Errorf("invalid --ttl %q: %w", ttlStr, err)
	}
	if d <= 0 {
		return time.Time{}, fmt.Errorf("%w: got %q", errTTLMustBePositive, ttlStr)
	}
	return now.Add(d), nil
}

// runRenew implements: key-mgmt renew <client-id> --ttl <dur|0>
func runRenew(keysFile string, args []string) error {
	if len(args) < 1 {
		fmt.Fprintf(os.Stderr, "usage: key-mgmt renew <client-id> --ttl <dur|0>\n")
		return errUsage
	}
	clientID := args[0]
	ttlStr := parseFlag(args[1:], "--ttl")
	if ttlStr == "" {
		return errRenewTTLRequired
	}

	// For renew we do not apply the default TTL; the operator must be explicit.
	expiresAt, err := resolveCLIExpiry(ttlStr, 0, time.Now())
	if err != nil {
		return err
	}

	entries, err := loadOrInit(keysFile)
	if err != nil {
		return err
	}

	found := false
	for i := range entries {
		if entries[i].ID == clientID {
			entries[i].ExpiresAt = expiresAt
			found = true
			break
		}
	}
	if !found {
		return fmt.Errorf("client %q: %w", clientID, errClientMissing)
	}

	if err := mcpkeys.SaveKeysFile(keysFile, entries); err != nil {
		return err
	}

	if expiresAt.IsZero() {
		fmt.Printf("Client %q expiry cleared (no expiry).\n", clientID)
	} else {
		fmt.Printf("Client %q renewed; expires: %s\n", clientID, expiresAt.UTC().Format(time.RFC3339))
	}
	return nil
}

func createKey(path, clientID string, roles []string, expiresAt time.Time) error {
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

	active := os.Getenv("KEY_HMAC_PEPPER")
	prev := os.Getenv("KEY_HMAC_PEPPER_PREVIOUS")
	if prev != "" && active == "" {
		return config.ErrPepperPreviousWithoutActive
	}
	fmt.Fprintf(os.Stderr, "key hashing: peppered=%t rotation_window=%t\n", active != "", prev != "")

	key, err := mcpkeys.GenerateKey()
	if err != nil {
		return err
	}

	hasher := auth.NewKeyHasher([]byte(active), []byte(prev))
	e := mcpkeys.NewKeyEntryWithHasher(clientID, key, roles, hasher)
	e.ExpiresAt = expiresAt
	entries = append(entries, e)

	if err := mcpkeys.SaveKeysFile(path, entries); err != nil {
		return err
	}

	fmt.Printf("Created key for %q:\n\n", clientID)
	fmt.Printf("  Bearer %s\n\n", key)
	if len(roles) > 0 {
		fmt.Printf("  Roles: %s\n", strings.Join(roles, ", "))
	}
	if expiresAt.IsZero() {
		fmt.Printf("  Expires: no expiry\n")
	} else {
		fmt.Printf("  Expires: %s\n", expiresAt.UTC().Format(time.RFC3339))
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
