// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Inveniam Capital Partners

package main

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/NVNM-Chain/nvnm-mcp-server/internal/anchor"
	"github.com/NVNM-Chain/nvnm-mcp-server/internal/auth"
	"github.com/NVNM-Chain/nvnm-mcp-server/internal/config"
	mcpserver "github.com/NVNM-Chain/nvnm-mcp-server/internal/mcp"
)

// lazyDeadPool returns a real *pgxpool.Pool aimed at a loopback port
// nothing listens on. pgxpool is lazy -- no dial happens until a query
// -- so this is a hermetic stand-in wherever a non-nil pool is needed
// and any query is expected (and allowed) to fail.
func lazyDeadPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	pool, err := pgxpool.New(context.Background(),
		"postgres://u:p@127.0.0.1:1/db?sslmode=disable&connect_timeout=1")
	if err != nil {
		t.Fatalf("lazy pool: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

// --- extractHost ---

func TestExtractHost(t *testing.T) {
	if got := extractHost("https://rpc.example.com:8545/path"); got != "rpc.example.com:8545" {
		t.Errorf("extractHost = %q, want rpc.example.com:8545", got)
	}
	// An unparseable URL must degrade to the "unknown" sentinel rather
	// than leaking the raw string into a metric label.
	if got := extractHost("http://[::1"); got != "unknown" {
		t.Errorf("extractHost(invalid) = %q, want unknown", got)
	}
}

// --- buildOriginAllowlist ---

func TestBuildOriginAllowlist(t *testing.T) {
	if got := buildOriginAllowlist(&config.Config{}); got != nil {
		t.Errorf("empty AllowedOrigins must return nil (default localhost-only), got %v", got)
	}
	got := buildOriginAllowlist(&config.Config{AllowedOrigins: []string{"https://app.example.com"}})
	if got == nil {
		t.Error("non-empty AllowedOrigins must return a non-nil allowlist")
	}
}

// --- buildEmailSender ---

func TestBuildEmailSender_NoSMTPFallsBackToLogOnly(t *testing.T) {
	sender, err := buildEmailSender(&config.Config{}, discardLogger())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := sender.(*mcpserver.LogOnlyEmailSender); !ok {
		t.Errorf("no-SMTP sender = %T, want *LogOnlyEmailSender", sender)
	}
}

func TestBuildEmailSender_SMTPConfigured(t *testing.T) {
	cfg := &config.Config{
		SMTPHost: "smtp.example.com",
		SMTPPort: 587,
		SMTPFrom: "keys@example.com",
	}
	sender, err := buildEmailSender(cfg, discardLogger())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := sender.(*mcpserver.SMTPEmailSender); !ok {
		t.Errorf("sender = %T, want *SMTPEmailSender", sender)
	}
}

// F4: a broken SMTP configuration must fail the boot rather than
// silently downgrade to logging minted keys -- unless the operator
// explicitly acknowledged NVNM_ALLOW_KEY_IN_LOGS.
func TestBuildEmailSender_BrokenSMTPFailsClosed(t *testing.T) {
	cfg := &config.Config{
		SMTPHost: "smtp.example.com",
		// SMTPPort deliberately zero: NewSMTPEmailSender rejects it.
	}
	if _, err := buildEmailSender(cfg, discardLogger()); err == nil {
		t.Fatal("expected error when SMTP construction fails without NVNM_ALLOW_KEY_IN_LOGS")
	}

	cfg.AllowKeyInLogs = true
	sender, err := buildEmailSender(cfg, discardLogger())
	if err != nil {
		t.Fatalf("unexpected error with NVNM_ALLOW_KEY_IN_LOGS acknowledged: %v", err)
	}
	if _, ok := sender.(*mcpserver.LogOnlyEmailSender); !ok {
		t.Errorf("acknowledged fallback sender = %T, want *LogOnlyEmailSender", sender)
	}
}

// --- newPendingAndEmail ---

func TestNewPendingAndEmail_Disabled(t *testing.T) {
	store, sender, err := newPendingAndEmail(&config.Config{}, discardLogger())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if store != nil {
		t.Error("pending store must be nil when KeyRequestEnabled is false")
	}
	if sender == nil {
		t.Error("email sender must always be built")
	}
}

func TestNewPendingAndEmail_Enabled(t *testing.T) {
	cfg := &config.Config{
		KeyRequestEnabled: true,
		KeyPendingFile:    filepath.Join(t.TempDir(), "pending.json"),
	}
	store, sender, err := newPendingAndEmail(cfg, discardLogger())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if store == nil {
		t.Error("pending store must be built when KeyRequestEnabled is true")
	}
	if sender == nil {
		t.Error("email sender must always be built")
	}
}

func TestNewPendingAndEmail_CorruptPendingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "pending.json")
	if err := os.WriteFile(path, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{KeyRequestEnabled: true, KeyPendingFile: path}
	if _, _, err := newPendingAndEmail(cfg, discardLogger()); err == nil {
		t.Fatal("expected error for corrupt pending store, got nil")
	}
}

// --- newHTTPLimiters ---

func TestNewHTTPLimiters(t *testing.T) {
	cfg := &config.Config{
		MCPRateLimit:  60,
		MCPRateBurst:  10,
		AnonRateLimit: 5,
		AnonRateBurst: 5,
	}
	mcpL, anonL, failL, stop := newHTTPLimiters(cfg, discardLogger())
	defer stop()
	if mcpL == nil || anonL == nil || failL == nil {
		t.Fatal("all three limiters must be constructed")
	}
}

// The hop-count env var is meaningless without trusting proxy headers;
// the warn branch must be taken (observable only via coverage, but the
// call must not panic or error).
func TestNewHTTPLimiters_HopsWithoutTrustWarns(t *testing.T) {
	t.Setenv("NVNM_TRUSTED_PROXY_HOPS", "2")
	cfg := &config.Config{
		MCPRateLimit:      60,
		MCPRateBurst:      10,
		AnonRateLimit:     5,
		AnonRateBurst:     5,
		TrustProxyHeaders: false,
	}
	_, _, _, stop := newHTTPLimiters(cfg, discardLogger())
	stop()
}

// --- newKeyRequestHandler ---

func TestNewKeyRequestHandler_Disabled(t *testing.T) {
	// Feature off.
	h, stop, err := newKeyRequestHandler(&config.Config{Transport: "http"}, discardLogger())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if h != nil {
		t.Error("handler must be nil when KeyRequestEnabled is false")
	}
	stop() // must be a callable no-op

	// Feature on but transport is stdio: endpoint is HTTP-only.
	h, stop, err = newKeyRequestHandler(
		&config.Config{KeyRequestEnabled: true, Transport: "stdio"}, discardLogger())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if h != nil {
		t.Error("handler must be nil on non-HTTP transport")
	}
	stop()
}

func TestNewKeyRequestHandler_Enabled(t *testing.T) {
	cfg := &config.Config{
		KeyRequestEnabled:      true,
		Transport:              "http",
		KeyPendingFile:         filepath.Join(t.TempDir(), "pending.json"),
		KeyRequestRateLimit:    1,
		KeyRequestRateBurst:    1,
		KeyRequestMaxBodyBytes: 4096,
	}
	h, stop, err := newKeyRequestHandler(cfg, discardLogger())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer stop()
	if h == nil {
		t.Fatal("expected a non-nil handler when enabled on HTTP")
	}
}

func TestNewKeyRequestHandler_StoreInitError(t *testing.T) {
	cfg := &config.Config{
		KeyRequestEnabled: true,
		Transport:         "http",
		KeyPendingFile:    "", // empty path is rejected by NewPendingKeyStore
	}
	_, stop, err := newKeyRequestHandler(cfg, discardLogger())
	if err == nil {
		t.Fatal("expected error for empty pending-file path, got nil")
	}
	stop() // stop must be callable even on the error path
}

// --- loadAuth / loadFusionAuth ---

func TestLoadAuth_DefaultIsAPIKeys(t *testing.T) {
	cfg := &config.Config{Transport: "stdio", AuthProvider: "apikey"}
	v, keys, cleanup, err := loadAuth(cfg, discardLogger())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if v != nil || keys != nil || cleanup != nil {
		t.Error("stdio with no keys must yield nil validator/store/cleanup")
	}
}

func TestLoadAuth_FusionAuthInitError(t *testing.T) {
	// No FusionAuthURL: NewFusionAuthValidator fails before any network I/O.
	cfg := &config.Config{AuthProvider: "fusionauth"}
	_, _, _, err := loadAuth(cfg, discardLogger())
	if err == nil {
		t.Fatal("expected FusionAuth init error, got nil")
	}
}

func TestLoadFusionAuth_HappyPath(t *testing.T) {
	// Serve an empty JWKS from a local listener so validator
	// construction (which fetches JWKS eagerly) succeeds hermetically.
	jwks := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"keys":[]}`))
	}))
	defer jwks.Close()

	cfg := &config.Config{
		AuthProvider:              "fusionauth",
		FusionAuthURL:             jwks.URL,
		FusionAuthAppID:           "app-uuid",
		FusionAuthClientIDHMACKey: "hmac-key-for-tests",
	}
	v, keys, cleanup, err := loadAuth(cfg, discardLogger())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if v == nil {
		t.Error("expected a non-nil validator")
	}
	if keys != nil {
		t.Error("FusionAuth mode must not return a key-store backend")
	}
	if cleanup == nil {
		t.Fatal("expected a cleanup function")
	}
	cleanup()
}

// --- loadAPIKeys (file-backed paths) ---

func TestLoadAPIKeys_KeysFileHappyPath(t *testing.T) {
	path := filepath.Join(t.TempDir(), "keys.json")
	hasher := auth.NewKeyHasher(nil, nil)
	entry := mcpserver.NewKeyEntryWithHasher("client-a", "raw-key-material", []string{"reader"}, hasher)
	if err := mcpserver.SaveKeysFile(path, []mcpserver.KeyEntry{entry}); err != nil {
		t.Fatalf("save keys file: %v", err)
	}
	cfg := &config.Config{Transport: "http", AuthProvider: "apikey", APIKeysFile: path}
	v, keys, _, err := loadAPIKeys(cfg, discardLogger())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if v == nil || keys == nil {
		t.Error("expected validator and key store for a populated keys file")
	}
}

func TestLoadAPIKeys_EmptyKeysFileHTTPFailsClosed(t *testing.T) {
	path := filepath.Join(t.TempDir(), "keys.json")
	if err := os.WriteFile(path, []byte(`[]`), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{Transport: "http", AuthProvider: "apikey", APIKeysFile: path}
	_, _, _, err := loadAPIKeys(cfg, discardLogger())
	if !errors.Is(err, config.ErrHTTPAuthRequired) {
		t.Fatalf("err = %v, want ErrHTTPAuthRequired", err)
	}
}

func TestLoadAPIKeys_CorruptKeysFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "keys.json")
	if err := os.WriteFile(path, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{
		Transport:    "stdio",
		AuthProvider: "apikey",
		APIKeysFile:  path,
	}
	if _, _, _, err := loadAPIKeys(cfg, discardLogger()); err == nil {
		t.Fatal("expected error for corrupt keys file, got nil")
	}
}

// --- loadPostgresKeyStore ---

func TestLoadPostgresKeyStore_InvalidDSN(t *testing.T) {
	cfg := &config.Config{
		AuthProvider:    "apikey",
		KeyStoreBackend: "postgres",
		KeyStoreDSN:     "postgres://u:p@localhost:not-a-port/db",
	}
	// Route through loadAPIKeys to also cover its postgres dispatch branch.
	_, _, _, err := loadAPIKeys(cfg, discardLogger())
	if !errors.Is(err, errKeyStoreDSNInvalid) {
		t.Fatalf("err = %v, want errKeyStoreDSNInvalid", err)
	}
	// The sentinel must not echo the DSN (it can embed a password).
	if err != nil && containsAny(err.Error(), "u:p@", "not-a-port") {
		t.Errorf("error leaks DSN contents: %v", err)
	}
}

func containsAny(s string, subs ...string) bool {
	for _, sub := range subs {
		if sub != "" && len(s) >= len(sub) {
			for i := 0; i+len(sub) <= len(s); i++ {
				if s[i:i+len(sub)] == sub {
					return true
				}
			}
		}
	}
	return false
}

func TestLoadPostgresKeyStore_UnreachableDB(t *testing.T) {
	// Port 1 on loopback: connection refused, fails fast and hermetically.
	hasher := auth.NewKeyHasher(nil, nil)
	cfg := &config.Config{
		KeyStoreDSN: "postgres://u:p@127.0.0.1:1/db?sslmode=disable&connect_timeout=2",
	}
	if _, _, _, err := loadPostgresKeyStore(cfg, hasher, discardLogger()); err == nil {
		t.Fatal("expected connect/ping error for unreachable postgres, got nil")
	}
}

func TestLoadPostgresKeyStore_PepperRequired(t *testing.T) {
	dsn := os.Getenv("NVNM_TEST_PG_DSN")
	if dsn == "" {
		t.Skip("NVNM_TEST_PG_DSN not set; skipping Postgres-backed test")
	}
	hasher := auth.NewKeyHasher(nil, nil)
	cfg := &config.Config{KeyStoreDSN: dsn} // KeyHMACPepper deliberately empty
	_, _, _, err := loadPostgresKeyStore(cfg, hasher, discardLogger())
	if !errors.Is(err, config.ErrPepperRequired) {
		t.Fatalf("err = %v, want ErrPepperRequired", err)
	}
}

func TestLoadPostgresKeyStore_HappyPathStdio(t *testing.T) {
	dsn := os.Getenv("NVNM_TEST_PG_DSN")
	if dsn == "" {
		t.Skip("NVNM_TEST_PG_DSN not set; skipping Postgres-backed test")
	}
	hasher := auth.NewKeyHasher([]byte("pepper"), nil)
	cfg := &config.Config{
		Transport:     "stdio", // stdio skips the empty-store fail-closed check
		KeyStoreDSN:   dsn,
		KeyHMACPepper: "pepper",
	}
	// The validator is nil when the shared test database happens to have
	// no enabled keys (NewAPIKeyValidatorWithHasher returns nil for an
	// empty store), so only the store and cleanup are asserted here.
	_, store, cleanup, err := loadPostgresKeyStore(cfg, hasher, discardLogger())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer cleanup()
	if store == nil {
		t.Error("expected a postgres key store")
	}
	if cleanup == nil {
		t.Error("expected a cleanup function")
	}
}

// --- startAdminServer ---

func testManagedKeys(t *testing.T) mcpserver.KeyStoreBackend {
	t.Helper()
	hasher := auth.NewKeyHasher(nil, nil)
	entry := mcpserver.NewKeyEntryWithHasher("client-a", "raw-key", []string{"reader"}, hasher)
	return mcpserver.NewManagedKeyStoreFromEntriesWithHasher("", []mcpserver.KeyEntry{entry}, hasher)
}

func TestStartAdminServer_NotConfigured(t *testing.T) {
	shutdown, err := startAdminServer(&config.Config{}, nil, nil, nil, nil, nil, nil, discardLogger())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if shutdown != nil {
		t.Error("no admin key configured must not start a server")
	}
}

func TestStartAdminServer_NonHTTPTransportSkips(t *testing.T) {
	cfg := &config.Config{AdminAPIKey: "k", Transport: "stdio", AuthProvider: "apikey"}
	shutdown, err := startAdminServer(cfg, testManagedKeys(t), nil, nil, nil, nil, nil, discardLogger())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if shutdown != nil {
		t.Error("non-HTTP transport must not start the admin server")
	}
}

func TestStartAdminServer_FusionAuthSkips(t *testing.T) {
	cfg := &config.Config{AdminAPIKey: "k", Transport: "http", AuthProvider: "fusionauth"}
	shutdown, err := startAdminServer(cfg, testManagedKeys(t), nil, nil, nil, nil, nil, discardLogger())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if shutdown != nil {
		t.Error("FusionAuth provider must not start the key-management admin server")
	}
}

func TestStartAdminServer_NilKeysErrors(t *testing.T) {
	cfg := &config.Config{AdminAPIKey: "k", Transport: "http", AuthProvider: "apikey"}
	_, err := startAdminServer(cfg, nil, nil, nil, nil, nil, nil, discardLogger())
	if !errors.Is(err, config.ErrAdminKeyWithoutFile) {
		t.Fatalf("err = %v, want ErrAdminKeyWithoutFile", err)
	}
}

func TestStartAdminServer_AdminKeysFileError(t *testing.T) {
	cfg := &config.Config{
		AdminAPIKey:      "k",
		Transport:        "http",
		AuthProvider:     "apikey",
		AdminAPIKeysFile: filepath.Join(t.TempDir(), "missing.json"),
	}
	if _, err := startAdminServer(cfg, testManagedKeys(t), nil, nil, nil, nil, nil, discardLogger()); err == nil {
		t.Fatal("expected error for missing admin keys file, got nil")
	}
}

func TestStartAdminServer_HappyPath(t *testing.T) {
	cfg := &config.Config{
		AdminAPIKey:  "admin-secret",
		AdminAPIAddr: "127.0.0.1:0",
		Transport:    "http",
		AuthProvider: "apikey",
	}
	shutdown, err := startAdminServer(cfg, testManagedKeys(t), nil, nil, nil, nil, nil, discardLogger())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if shutdown == nil {
		t.Fatal("expected a shutdown function for a started admin server")
	}
	shutdown()
}

// --- startRetentionPurge ---

// stubAnchorClient returns an anchor.Client with no ABI loaded, so
// MethodSelector("grantRole") yields ("", false).
func stubAnchorClient() anchor.Client {
	return anchor.NewClient(nil, anchor.PrecompileAddress, 1, "", discardLogger())
}

func TestStartRetentionPurge_DisabledIsNoop(t *testing.T) {
	err := startRetentionPurge(context.Background(), &config.Config{}, nil, nil, discardLogger())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestStartRetentionPurge_NoPoolWarnsAndSkips(t *testing.T) {
	cfg := &config.Config{Retention: config.RetentionConfig{
		WriteAudit:    time.Hour,
		PurgeInterval: time.Hour,
	}}
	// nil pool: retention configured but nothing persisted -- warn, no error.
	err := startRetentionPurge(context.Background(), cfg, nil, stubAnchorClient(), discardLogger())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// A grantRole retention window without a loaded ABI must fail loud:
// grantRole rows could not be distinguished from ordinary writes and
// would be purged on the shorter window.
func TestStartRetentionPurge_GrantRoleWindowWithoutABI(t *testing.T) {
	cfg := &config.Config{Retention: config.RetentionConfig{
		WriteAuditGrantRole: 2 * time.Hour,
		PurgeInterval:       time.Hour,
	}}
	err := startRetentionPurge(
		context.Background(), cfg, lazyDeadPool(t), stubAnchorClient(), discardLogger())
	if err == nil {
		t.Fatal("expected error for grantRole window without an ABI selector, got nil")
	}
}

func TestStartRetentionPurge_StartsPurger(t *testing.T) {
	cfg := &config.Config{Retention: config.RetentionConfig{
		WriteAudit:    24 * time.Hour,
		PurgeInterval: time.Hour,
	}}
	// Pre-canceled context: the purge goroutine's first sweep fails fast
	// against the dead pool and the goroutine exits immediately.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := startRetentionPurge(ctx, cfg, lazyDeadPool(t), stubAnchorClient(), discardLogger())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// --- loadWriteAudit (hermetic connection-failure path) ---

func TestLoadWriteAudit_UnreachableDB(t *testing.T) {
	cfg := &config.Config{
		KeylessPGDSN: "postgres://u:p@127.0.0.1:1/db?sslmode=disable&connect_timeout=2",
	}
	_, _, _, _, _, _, err := loadWriteAudit(cfg, discardLogger())
	if err == nil {
		t.Fatal("expected connect/ping error for unreachable keyless postgres, got nil")
	}
}

// --- startAuditAndAdmin ---

func TestStartAuditAndAdmin_AllOff(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	audit, quota, blacklist, cleanup, err := startAuditAndAdmin(
		ctx, &config.Config{}, nil, nil, nil, stubAnchorClient(), discardLogger())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer cleanup()
	if audit != nil || quota != nil || blacklist != nil {
		t.Error("nothing configured must provision nothing")
	}
}

func TestStartAuditAndAdmin_InvalidKeylessDSN(t *testing.T) {
	cfg := &config.Config{KeylessPGDSN: "postgres://u:p@localhost:not-a-port/db"}
	_, _, _, _, err := startAuditAndAdmin(
		context.Background(), cfg, nil, nil, nil, stubAnchorClient(), discardLogger())
	if !errors.Is(err, errKeylessDSNInvalid) {
		t.Fatalf("err = %v, want errKeylessDSNInvalid", err)
	}
}

func TestStartAuditAndAdmin_AdminErrorTriggersCleanup(t *testing.T) {
	// Admin API configured, apikey provider, HTTP transport, but keys is
	// nil: startAdminServer must fail and startAuditAndAdmin must
	// propagate the error.
	cfg := &config.Config{
		AdminAPIKey:  "k",
		Transport:    "http",
		AuthProvider: "apikey",
	}
	_, _, _, _, err := startAuditAndAdmin(
		context.Background(), cfg, nil, nil, nil, stubAnchorClient(), discardLogger())
	if !errors.Is(err, config.ErrAdminKeyWithoutFile) {
		t.Fatalf("err = %v, want ErrAdminKeyWithoutFile", err)
	}
}

// --- runTransport ---

func TestRunTransport_UnknownTransport(t *testing.T) {
	cfg := &config.Config{Transport: "carrier-pigeon"}
	err := runTransport(context.Background(), nil, cfg, nil, nil, nil, nil, nil, nil, "")
	if !errors.Is(err, config.ErrInvalidTransport) {
		t.Fatalf("err = %v, want ErrInvalidTransport", err)
	}
}
