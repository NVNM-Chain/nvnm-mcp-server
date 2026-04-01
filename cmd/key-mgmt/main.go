package main

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"text/tabwriter"
	"time"

	mcpkeys "github.com/inveniam/nvnm-mcp-server/internal/mcp"
)

const defaultKeysFile = ".mcp-keys.json"

var (
	errClientExists    = errors.New("client already exists")
	errClientMissing   = errors.New("client not found")
	errInvalidApproval = errors.New("write-approval must be \"required\", \"auto\", or empty")
)

var validApprovals = map[string]bool{"required": true, "auto": true, "": true}

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
	case "set-approval":
		return runSetApproval(keysFile, args[1:])
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
			"usage: key-mgmt create <client-id> [--write-approval required|auto]\n")
		return errUsage
	}
	approval := parseApprovalFlag(args[1:])
	return createKey(keysFile, args[0], approval)
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

func runSetApproval(keysFile string, args []string) error {
	if len(args) < 2 {
		fmt.Fprintf(os.Stderr,
			"usage: key-mgmt set-approval <client-id> <required|auto>\n")
		return errUsage
	}
	return setApproval(keysFile, args[0], args[1])
}

func usage() {
	fmt.Fprintf(os.Stderr, "usage: key-mgmt <command> [args]\n\n")
	fmt.Fprintf(os.Stderr, "commands:\n")
	fmt.Fprintf(os.Stderr, "  create <client-id> [--write-approval required|auto]\n")
	fmt.Fprintf(os.Stderr, "                       Generate a new API key for a client\n")
	fmt.Fprintf(os.Stderr, "  disable <client-id>  Disable an existing API key\n")
	fmt.Fprintf(os.Stderr, "  enable <client-id>   Re-enable a disabled API key\n")
	fmt.Fprintf(os.Stderr, "  set-approval <client-id> <required|auto>\n")
	fmt.Fprintf(os.Stderr, "                       Set write-approval policy for a client\n")
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

func parseApprovalFlag(args []string) string {
	for i, a := range args {
		if a == "--write-approval" && i+1 < len(args) {
			return args[i+1]
		}
	}
	return ""
}

func createKey(path, clientID, writeApproval string) error {
	if !validApprovals[writeApproval] {
		return fmt.Errorf("%q: %w", writeApproval, errInvalidApproval)
	}

	entries, err := loadOrInit(path)
	if err != nil {
		return err
	}

	for _, e := range entries {
		if e.ID == clientID {
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

	entries = append(entries, mcpkeys.KeyEntry{
		ID:            clientID,
		Key:           key,
		Enabled:       true,
		CreatedAt:     time.Now().UTC(),
		WriteApproval: writeApproval,
	})

	if err := mcpkeys.SaveKeysFile(path, entries); err != nil {
		return err
	}

	fmt.Printf("Created key for %q:\n\n", clientID)
	fmt.Printf("  Bearer %s\n\n", key)
	if writeApproval != "" {
		fmt.Printf("  Write approval: %s\n", writeApproval)
	}
	fmt.Printf("Store this key securely — it cannot be retrieved later.\n")
	fmt.Printf("Keys file: %s\n", path)
	return nil
}

func setApproval(path, clientID, approval string) error {
	if approval != "required" && approval != "auto" {
		return fmt.Errorf("%q: %w", approval, errInvalidApproval)
	}

	entries, err := loadOrInit(path)
	if err != nil {
		return err
	}

	found := false
	for i := range entries {
		if entries[i].ID == clientID {
			entries[i].WriteApproval = approval
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

	fmt.Printf("Client %q write-approval set to %q.\n", clientID, approval)
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
	fmt.Fprintf(w, "CLIENT ID\tSTATUS\tWRITE APPROVAL\tCREATED\tKEY PREFIX\n")
	fmt.Fprintf(w, "---------\t------\t--------------\t-------\t----------\n")
	for _, e := range entries {
		status := "enabled"
		if !e.Enabled {
			status = "disabled"
		}
		prefix := e.Key
		if len(prefix) > 8 {
			prefix = prefix[:8] + "..."
		}
		approval := e.WriteApproval
		if approval == "" {
			approval = "(default)"
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n",
			e.ID,
			status,
			approval,
			e.CreatedAt.Format("2006-01-02"),
			prefix,
		)
	}
	return w.Flush()
}
