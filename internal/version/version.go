package version

// Version is the canonical server version string. All packages reference this
// single constant so bumps are atomic.
//
// Override at build time with:
//
//	go build -ldflags "-X github.com/inveniam/nvnm-mcp-server/internal/version.Version=1.2.3"
var Version = "0.4.0"
