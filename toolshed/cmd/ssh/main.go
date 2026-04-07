// Entry point for the ToolShed v2 SSH server.
//
// The SSH server is the primary interface for ToolShed — agents connect via SSH,
// authenticate with their public key (which IS their identity), and issue commands
// that return YAML.
//
// Configuration via environment variables:
//
//	TOOLSHED_SSH_PORT       - TCP port to listen on (default "2222")
//	TOOLSHED_HOST_KEY_PATH  - path to host key file (default ".ssh/toolshed_host_key")
//	TOOLSHED_REGISTRY_DSN   - Dolt registry DSN (default "root@tcp(localhost:3306)/toolshed_registry?parseTime=true")
//	TOOLSHED_LEDGER_DSN     - Dolt ledger DSN (default "root@tcp(localhost:3306)/toolshed_ledger?parseTime=true")
//	TOOLSHED_MODEL_DIR      - path to ONNX model directory containing model.onnx + tokenizer.json (enables semantic search)
//	TOOLSHED_ONNX_LIB       - path to ONNX Runtime shared library (auto-detected if not set)
//
// Hardening (all optional — sensible defaults are applied):
//
//	TOOLSHED_PROXY_PROTOCOL - enable PROXY protocol for real client IPs on Fly.io (default "false")
//	TOOLSHED_RATE_PER_IP    - max new connections per IP per minute (default "20")
//	TOOLSHED_MAX_PER_IP     - max concurrent connections per IP (default "10")
//	TOOLSHED_MAX_TOTAL      - max total concurrent connections (default "200")
//	TOOLSHED_BAN_AFTER      - ban IP after N rate-limit violations (default "5")
//	TOOLSHED_BAN_DURATION   - ban duration e.g. "15m" (default "15m")
//	TOOLSHED_MAX_SESSION    - max session duration e.g. "30m" (default "30m")
//	TOOLSHED_IDLE_TIMEOUT   - idle timeout e.g. "5m" (default "5m")
//	TOOLSHED_MAX_AUTH_TRIES - max auth attempts per connection (default "3")
package main

import (
	"context"
	"crypto/sha256"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/toolshed/toolshed/internal/dolt"
	"github.com/toolshed/toolshed/internal/embeddings"
	sshserver "github.com/toolshed/toolshed/internal/ssh"
)

func main() {
	log.SetFlags(log.LstdFlags | log.Lshortfile)

	portStr := envOr("TOOLSHED_SSH_PORT", "2222")
	hostKeyPath := envOr("TOOLSHED_HOST_KEY_PATH", ".ssh/toolshed_host_key")
	registryDSN := envOr("TOOLSHED_REGISTRY_DSN", "root@tcp(localhost:3306)/toolshed_registry?parseTime=true")
	ledgerDSN := envOr("TOOLSHED_LEDGER_DSN", "root@tcp(localhost:3306)/toolshed_ledger?parseTime=true")
	webPort := envOr("TOOLSHED_WEB_PORT", "8080")
	webRoot := envOr("TOOLSHED_WEB_ROOT", "./public")
	modelDir := os.Getenv("TOOLSHED_MODEL_DIR")
	onnxLib := os.Getenv("TOOLSHED_ONNX_LIB")

	port, err := strconv.Atoi(portStr)
	if err != nil {
		log.Fatalf("invalid TOOLSHED_SSH_PORT %q: %v", portStr, err)
	}

	// ── Hardening config ────────────────────────────────────────────────
	hardenCfg := sshserver.DefaultHardenConfig()
	hardenCfg.ProxyProtocol = envBool("TOOLSHED_PROXY_PROTOCOL", hardenCfg.ProxyProtocol)
	hardenCfg.PerIPRate = envInt("TOOLSHED_RATE_PER_IP", hardenCfg.PerIPRate)
	hardenCfg.MaxPerIP = envInt("TOOLSHED_MAX_PER_IP", hardenCfg.MaxPerIP)
	hardenCfg.MaxTotal = envInt("TOOLSHED_MAX_TOTAL", hardenCfg.MaxTotal)
	hardenCfg.BanAfter = envInt("TOOLSHED_BAN_AFTER", hardenCfg.BanAfter)
	hardenCfg.BanDuration = envDuration("TOOLSHED_BAN_DURATION", hardenCfg.BanDuration)
	hardenCfg.MaxSessionTime = envDuration("TOOLSHED_MAX_SESSION", hardenCfg.MaxSessionTime)
	hardenCfg.IdleTimeout = envDuration("TOOLSHED_IDLE_TIMEOUT", hardenCfg.IdleTimeout)
	hardenCfg.MaxAuthTries = envInt("TOOLSHED_MAX_AUTH_TRIES", hardenCfg.MaxAuthTries)

	log.Println("toolshed-ssh starting")
	log.Printf("  port:     %d", port)
	log.Printf("  host key: %s", hostKeyPath)
	log.Printf("  registry: %s", registryDSN)
	log.Printf("  ledger:   %s", ledgerDSN)
	log.Printf("  web:      :%s (root: %s)", webPort, webRoot)

	// Connect to Dolt.
	registry, err := dolt.NewRegistry(registryDSN, ledgerDSN)
	if err != nil {
		log.Fatalf("failed to connect to Dolt: %v", err)
	}
	defer registry.Close()
	log.Println("connected to Dolt registry and ledger")

	// Optional: set up local ONNX embedding model for semantic search.
	var embedder embeddings.Embedder
	if modelDir != "" {
		if err := embeddings.InitONNXRuntime(onnxLib); err != nil {
			log.Fatalf("failed to init ONNX runtime: %v", err)
		}
		defer embeddings.DestroyONNXRuntime()

		onnxEmb, err := embeddings.NewONNXEmbedder(modelDir)
		if err != nil {
			log.Fatalf("failed to create ONNX embedder: %v", err)
		}
		defer onnxEmb.Close()
		embedder = onnxEmb
		log.Printf("embeddings: enabled (model: %s, %d dims, local ONNX)", onnxEmb.Model(), onnxEmb.Dimensions())
	} else {
		log.Println("embeddings: disabled (set TOOLSHED_MODEL_DIR to enable semantic search)")
	}

	// Backfill embeddings for any tools that don't have them yet (e.g. seeded data).
	if embedder != nil {
		backfillEmbeddings(registry, embedder)
	}

	// Create the SSH server with hardening.
	srv, err := sshserver.NewServer(registry, embedder, hostKeyPath, port, hardenCfg)
	if err != nil {
		log.Fatalf("failed to create SSH server: %v", err)
	}

	// Static file server for the website (toolshed.sh).
	// Fly routes HTTP 80/443 → internal port 8080.
	absWebRoot, _ := filepath.Abs(webRoot)
	httpSrv := &http.Server{
		Addr:    ":" + webPort,
		Handler: http.FileServer(http.Dir(absWebRoot)),
	}
	go func() {
		log.Printf("http: serving %s on :%s", absWebRoot, webPort)
		if err := httpSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Printf("http: server error: %v", err)
		}
	}()

	// Start the server in a goroutine.
	errCh := make(chan error, 1)
	go func() {
		errCh <- srv.Start()
	}()

	// Wait for interrupt signal or server error.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	select {
	case sig := <-sigCh:
		log.Printf("received signal %v, shutting down...", sig)
	case err := <-errCh:
		if err != nil {
			log.Fatalf("SSH server error: %v", err)
		}
	}

	// Graceful shutdown with a 10-second deadline.
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := httpSrv.Shutdown(ctx); err != nil {
		log.Printf("http: shutdown error: %v", err)
	}
	if err := srv.Shutdown(ctx); err != nil {
		log.Fatalf("shutdown error: %v", err)
	}

	log.Println("toolshed-ssh stopped")
}

// backfillEmbeddings generates embeddings for any tools in the registry that
// don't have one yet. This runs synchronously at startup so semantic search
// works immediately — even for seeded data that was inserted before the
// embedder was configured.
func backfillEmbeddings(registry *dolt.Registry, emb embeddings.Embedder) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	tools, err := registry.GetToolsMissingEmbeddings(ctx)
	if err != nil {
		log.Printf("backfill: failed to query tools missing embeddings: %v", err)
		return
	}

	if len(tools) == 0 {
		log.Println("backfill: all tools have embeddings")
		return
	}

	log.Printf("backfill: generating embeddings for %d tools...", len(tools))

	count := 0
	for _, tw := range tools {
		text := embeddings.BuildToolText(
			tw.Listing.Name,
			tw.Listing.Description,
			tw.Listing.ProviderDomain,
			tw.Definition.Capabilities,
		)

		vec, err := emb.Embed(ctx, text)
		if err != nil {
			log.Printf("backfill: failed to embed %s: %v", tw.Listing.ID, err)
			continue
		}

		textHash := fmt.Sprintf("sha256:%x", sha256Hash([]byte(text)))

		te := embeddings.ToolEmbedding{
			ToolID:     tw.Listing.ID,
			Embedding:  vec,
			Model:      emb.Model(),
			Dimensions: emb.Dimensions(),
			TextHash:   textHash,
		}

		if err := registry.StoreEmbedding(ctx, te); err != nil {
			log.Printf("backfill: failed to store embedding for %s: %v", tw.Listing.ID, err)
			continue
		}

		count++
		log.Printf("backfill: ✓ %s (%d/%d)", tw.Listing.ID, count, len(tools))
	}

	log.Printf("backfill: done — %d/%d tools embedded", count, len(tools))
}

// sha256Hash computes a SHA-256 hash.
func sha256Hash(data []byte) [32]byte {
	return sha256.Sum256(data)
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	fmt.Printf("  env %s not set, using default: %s\n", key, fallback)
	return fallback
}

// envInt reads an integer from the environment, falling back to fallback.
func envInt(key string, fallback int) int {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		log.Printf("warning: invalid %s=%q, using default %d", key, v, fallback)
		return fallback
	}
	return n
}

// envDuration reads a time.Duration from the environment, falling back to fallback.
func envDuration(key string, fallback time.Duration) time.Duration {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		log.Printf("warning: invalid %s=%q, using default %v", key, v, fallback)
		return fallback
	}
	return d
}

// envBool reads a boolean from the environment, falling back to fallback.
// Truthy values: "true", "1", "yes". Everything else is false.
func envBool(key string, fallback bool) bool {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	switch strings.ToLower(v) {
	case "true", "1", "yes":
		return true
	default:
		return false
	}
}
