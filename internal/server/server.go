// Package server implements the pkm-sync HTTP API: read-only search and
// status endpoints over the SQLite databases maintained by sync/index runs,
// plus a Prometheus /metrics endpoint (a port of the former Python exporter).
package server

import (
	"crypto/subtle"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"

	"pkm-sync/internal/embeddings"
	"pkm-sync/internal/vectorstore"
)

// Config holds the listen settings and data-file paths for the API server.
// All database access is read-only and best-effort: a missing file yields
// empty results (or 503 for search) rather than a crash.
type Config struct {
	Token         string // bearer token required on /api/* routes; empty disables auth
	StatusDir     string // directory of pipeline-status *.json files
	MetricsDBPath string // agent_metrics.db
	VectorDBPath  string // vectors.db
	ArchiveDBPath string // archive.db (Gmail FTS archive)
	SlackDBPath   string // slack.db (Slack message archive)
	UserCachePath string // slack-user-cache.json (user ID -> display name)
	Dimensions    int    // embedding dimensions, must match vectors.db
}

// Server is the pkm-sync HTTP API server.
type Server struct {
	cfg      Config
	embedder embeddings.Provider // nil when no embedding provider is configured

	// Database handles are opened lazily and cached for the server's
	// lifetime. Holding them open avoids SQLite's close-time WAL checkpoint
	// (a write we don't want a logically read-only server performing while
	// sync jobs hold the databases) and per-request open overhead.
	mu       sync.Mutex
	dbs      map[string]*sql.DB
	vecStore *vectorstore.Store
}

// New creates a Server. The embedder may be nil, in which case /api/search
// responds 503 (metadata-only mode has nothing to search with).
func New(cfg Config, embedder embeddings.Provider) *Server {
	return &Server{
		cfg:      cfg,
		embedder: embedder,
		dbs:      make(map[string]*sql.DB),
	}
}

// Handler returns the root http.Handler with all routes registered.
// /healthz and /metrics are always unauthenticated (Kubernetes probes and
// Prometheus scrapes don't carry bearer tokens); /api/* requires the token.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /healthz", s.handleHealthz)
	mux.HandleFunc("GET /metrics", s.handleMetrics)
	mux.Handle("GET /api/search", s.auth(http.HandlerFunc(s.handleSearch)))
	mux.Handle("GET /api/emails", s.auth(http.HandlerFunc(s.handleEmails)))
	mux.Handle("GET /api/slack/messages", s.auth(http.HandlerFunc(s.handleSlackMessages)))
	mux.Handle("GET /api/status", s.auth(http.HandlerFunc(s.handleStatus)))

	return mux
}

// auth wraps a handler with bearer-token authentication. When no token is
// configured the handler is served as-is.
func (s *Server) auth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.cfg.Token == "" {
			next.ServeHTTP(w, r)

			return
		}

		const prefix = "Bearer "

		header := r.Header.Get("Authorization")
		if !strings.HasPrefix(header, prefix) ||
			subtle.ConstantTimeCompare([]byte(strings.TrimPrefix(header, prefix)), []byte(s.cfg.Token)) != 1 {
			writeError(w, http.StatusUnauthorized, "missing or invalid bearer token")

			return
		}

		next.ServeHTTP(w, r)
	})
}

// handleHealthz reports liveness plus existence of each backing data file,
// mirroring the former Python exporter's /healthz payload.
func (s *Server) handleHealthz(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":                   true,
		"status_dir_exists":    pathExists(s.cfg.StatusDir),
		"agent_metrics_exists": pathExists(s.cfg.MetricsDBPath),
		"vectors_db_exists":    pathExists(s.cfg.VectorDBPath),
		"archive_db_exists":    pathExists(s.cfg.ArchiveDBPath),
		"slack_db_exists":      pathExists(s.cfg.SlackDBPath),
	})
}

// db returns a cached handle for the SQLite database at dbPath, opening it on
// first use. It fails when the file does not exist (sql.Open alone is lazy
// and would not notice). Connections are mode=rw — not ro — because WAL-mode
// databases cannot be reliably opened read-only once their -shm/-wal sidecars
// have been checkpointed away; handlers must only issue reads.
func (s *Server) db(dbPath string) (*sql.DB, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if cached, ok := s.dbs[dbPath]; ok {
		return cached, nil
	}

	if _, err := os.Stat(dbPath); err != nil {
		return nil, fmt.Errorf("database not available at %s: %w", dbPath, err)
	}

	conn, err := sql.Open("sqlite3", "file:"+dbPath+"?mode=rw&_busy_timeout=5000")
	if err != nil {
		return nil, fmt.Errorf("failed to open %s: %w", dbPath, err)
	}

	s.dbs[dbPath] = conn

	return conn, nil
}

// vectors returns the cached vector store, opening it on first use.
func (s *Server) vectors() (*vectorstore.Store, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.vecStore != nil {
		return s.vecStore, nil
	}

	store, err := vectorstore.NewQueryStore(s.cfg.VectorDBPath, s.cfg.Dimensions)
	if err != nil {
		return nil, err
	}

	s.vecStore = store

	return store, nil
}

// pathExists reports whether the given path exists.
func pathExists(path string) bool {
	_, err := os.Stat(path)

	return err == nil
}

// queryInt parses an integer query parameter, returning def when absent or
// invalid, clamped to [1, maxVal].
func queryInt(r *http.Request, name string, def, maxVal int) int {
	raw := r.URL.Query().Get(name)
	if raw == "" {
		return def
	}

	n, err := strconv.Atoi(raw)
	if err != nil || n < 1 {
		return def
	}

	if n > maxVal {
		return maxVal
	}

	return n
}

// writeJSON serializes v as the response body with the given status code.
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)

	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	_ = enc.Encode(v)
}

// writeError sends a JSON error payload.
func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}
