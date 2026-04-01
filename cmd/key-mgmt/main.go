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
	errClientExists  = errors.New("client already exists")
	errClientMissing = errors.New("client not found")
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(1)
	}

	keysFile := os.Getenv("MCP_API_KEYS_FILE")
	if keysFile == "" {
		keysFile = defaultKeysFile
	}

	switch os.Args[1] {
	case "create":
		if len(os.Args) < 3 {
			fmt.Fprintf(os.Stderr, "usage: key-mgmt create <client-id>\n")
			os.Exit(1)
		}
		clientID := os.Args[2]
		if err := createKey(keysFile, clientID); err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
	case "disable":
		if len(os.Args) < 3 {
			fmt.Fprintf(os.Stderr, "usage: key-mgmt disable <client-id>\n")
			os.Exit(1)
		}
		clientID := os.Args[2]
		if err := disableKey(keysFile, clientID); err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
	case "enable":
		if len(os.Args) < 3 {
			fmt.Fprintf(os.Stderr, "usage: key-mgmt enable <client-id>\n")
			os.Exit(1)
		}
		clientID := os.Args[2]
		if err := enableKey(keysFile, clientID); err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
	case "list":
		if err := listKeys(keysFile); err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
	default:
		usage()
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprintf(os.Stderr, "usage: key-mgmt <command> [args]\n\n")
	fmt.Fprintf(os.Stderr, "commands:\n")
	fmt.Fprintf(os.Stderr, "  create <client-id>   Generate a new API key for a client\n")
	fmt.Fprintf(os.Stderr, "  disable <client-id>  Disable an existing API key\n")
	fmt.Fprintf(os.Stderr, "  enable <client-id>   Re-enable a disabled API key\n")
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

func createKey(path, clientID string) error {
	entries, err := loadOrInit(path)
	if err != nil {
		return err
	}

	for _, e := range entries {
		if e.ID == clientID {
			return fmt.Errorf("client %q (use 'enable' to re-activate a disabled key): %w", clientID, errClientExists)
		}
	}

	key, err := mcpkeys.GenerateKey()
	if err != nil {
		return err
	}

	entries = append(entries, mcpkeys.KeyEntry{
		ID:        clientID,
		Key:       key,
		Enabled:   true,
		CreatedAt: time.Now().UTC(),
	})

	if err := mcpkeys.SaveKeysFile(path, entries); err != nil {
		return err
	}

	fmt.Printf("Created key for %q:\n\n", clientID)
	fmt.Printf("  Bearer %s\n\n", key)
	fmt.Printf("Store this key securely — it cannot be retrieved later.\n")
	fmt.Printf("Keys file: %s\n", path)
	return nil
}

func disableKey(path, clientID string) error {
	return setEnabled(path, clientID, false)
}

func enableKey(path, clientID string) error {
	return setEnabled(path, clientID, true)
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
	fmt.Fprintf(w, "CLIENT ID\tSTATUS\tCREATED\tKEY PREFIX\n")
	fmt.Fprintf(w, "---------\t------\t-------\t----------\n")
	for _, e := range entries {
		status := "enabled"
		if !e.Enabled {
			status = "disabled"
		}
		prefix := e.Key
		if len(prefix) > 8 {
			prefix = prefix[:8] + "..."
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\n",
			e.ID,
			status,
			e.CreatedAt.Format("2006-01-02"),
			prefix,
		)
	}
	return w.Flush()
}
