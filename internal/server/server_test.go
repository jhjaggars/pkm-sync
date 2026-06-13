package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"pkm-sync/internal/vectorstore"

	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeEmbedder returns a fixed vector for every input.
type fakeEmbedder struct {
	vec []float32
}

func (f *fakeEmbedder) Embed(_ context.Context, _ string) ([]float32, error) {
	return f.vec, nil
}

func (f *fakeEmbedder) EmbedBatch(_ context.Context, texts []string) ([][]float32, error) {
	out := make([][]float32, len(texts))
	for i := range texts {
		out[i] = f.vec
	}

	return out, nil
}

func (f *fakeEmbedder) Dimensions() int { return len(f.vec) }
func (f *fakeEmbedder) Close() error    { return nil }

// newTestServer builds a Server over fixture databases in a temp dir.
func newTestServer(t *testing.T) (*Server, string) {
	t.Helper()

	dir := t.TempDir()

	cfg := Config{
		Token:         "test-token",
		StatusDir:     filepath.Join(dir, "pipeline-status"),
		MetricsDBPath: filepath.Join(dir, "agent_metrics.db"),
		VectorDBPath:  filepath.Join(dir, "vectors.db"),
		ArchiveDBPath: filepath.Join(dir, "archive.db"),
		SlackDBPath:   filepath.Join(dir, "slack.db"),
		UserCachePath: filepath.Join(dir, "slack-user-cache.json"),
		Dimensions:    3,
	}

	seedVectors(t, cfg.VectorDBPath)
	seedArchive(t, cfg.ArchiveDBPath)
	seedSlack(t, cfg.SlackDBPath)
	seedAgentMetrics(t, cfg.MetricsDBPath)
	seedStatus(t, cfg.StatusDir)
	seedUserCache(t, cfg.UserCachePath)

	return New(cfg, &fakeEmbedder{vec: []float32{1, 0, 0}}), cfg.Token
}

func seedVectors(t *testing.T, path string) {
	t.Helper()

	store, err := vectorstore.NewStore(path, 3)
	require.NoError(t, err)

	defer store.Close()

	now := time.Now().UTC()

	docs := []struct {
		doc vectorstore.Document
		vec []float32
	}{
		{
			doc: vectorstore.Document{
				SourceID: "m1", ThreadID: "t1", Title: "Deploy failed in prod",
				Content: "the deploy failed", SourceType: "slack", SourceName: "slack_redhat",
				MessageCount: 2, Metadata: map[string]interface{}{"participants": []string{"alice"}},
				CreatedAt: now, UpdatedAt: now,
			},
			vec: []float32{1, 0, 0},
		},
		{
			doc: vectorstore.Document{
				SourceID: "m2", ThreadID: "t2", Title: "Lunch plans",
				Content: "tacos?", SourceType: "slack", SourceName: "slack_redhat",
				MessageCount: 1, Metadata: map[string]interface{}{},
				CreatedAt: now, UpdatedAt: now,
			},
			vec: []float32{0, 1, 0},
		},
	}

	for _, d := range docs {
		require.NoError(t, store.UpsertDocument(d.doc, d.vec))
	}
}

func seedArchive(t *testing.T, path string) {
	t.Helper()

	db, err := sql.Open("sqlite3", path)
	require.NoError(t, err)

	defer db.Close()

	_, err = db.Exec(`
		CREATE TABLE messages (
			gmail_id TEXT PRIMARY KEY, thread_id TEXT NOT NULL DEFAULT '',
			subject TEXT NOT NULL DEFAULT '', from_addr TEXT NOT NULL DEFAULT '',
			to_addrs TEXT NOT NULL DEFAULT '[]', cc_addrs TEXT NOT NULL DEFAULT '[]',
			date_sent DATETIME NOT NULL, date_archived DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			source_name TEXT NOT NULL DEFAULT ''
		);
		CREATE TABLE sync_state (
			source_name TEXT PRIMARY KEY, last_sync_time DATETIME NOT NULL,
			message_count INTEGER NOT NULL DEFAULT 0
		);
		CREATE VIRTUAL TABLE messages_fts USING fts4(subject, body, from_addr, tokenize=porter);
	`)
	require.NoError(t, err)

	_, err = db.Exec(`
		INSERT INTO messages (gmail_id, thread_id, subject, from_addr, to_addrs, date_sent, source_name)
		VALUES ('g1', 'th1', 'ROSA boundary review', 'alice@example.com',
		        '["bob@example.com"]', '2026-06-01T12:00:00Z', 'gmail_work');
		INSERT INTO messages_fts (rowid, subject, body, from_addr)
		VALUES (1, 'ROSA boundary review', 'please review the rosa boundary doc', 'alice@example.com');
		INSERT INTO sync_state (source_name, last_sync_time, message_count)
		VALUES ('gmail_work', '2026-06-10T08:00:00Z', 1);
	`)
	require.NoError(t, err)
}

func seedSlack(t *testing.T, path string) {
	t.Helper()

	db, err := sql.Open("sqlite3", path)
	require.NoError(t, err)

	defer db.Close()

	_, err = db.Exec(`
		CREATE TABLE slack_messages (
			rowid INTEGER PRIMARY KEY AUTOINCREMENT,
			id TEXT UNIQUE NOT NULL, channel_id TEXT NOT NULL, channel_name TEXT NOT NULL,
			workspace TEXT NOT NULL, author TEXT NOT NULL, content TEXT NOT NULL,
			message_url TEXT NOT NULL, item_type TEXT NOT NULL,
			thread_ts TEXT NOT NULL DEFAULT '', is_thread_root INTEGER NOT NULL DEFAULT 0,
			reply_count INTEGER NOT NULL DEFAULT 0, created_at TEXT NOT NULL, synced_at TEXT NOT NULL
		);
	`)
	require.NoError(t, err)

	_, err = db.Exec(`
		INSERT INTO slack_messages
			(id, channel_id, channel_name, workspace, author, content, message_url,
			 item_type, created_at, synced_at)
		VALUES
			('s1', 'C1', 'forum-rosa-eng', 'redhat', 'UHASH1', 'the deploy failed again',
			 'https://slack/1', 'message', '2026-06-10T10:00:00Z', '2026-06-10T10:05:00Z'),
			('s2', 'C2', 'forum-mcp', 'redhat', 'UHASH2', 'mcp server question',
			 'https://slack/2', 'message', '2026-06-09T10:00:00Z', '2026-06-09T10:05:00Z');
	`)
	require.NoError(t, err)
}

func seedAgentMetrics(t *testing.T, path string) {
	t.Helper()

	db, err := sql.Open("sqlite3", path)
	require.NoError(t, err)

	defer db.Close()

	// completed is declared BOOLEAN to match the real metrics.py schema: the
	// sqlite driver returns Go bool for BOOLEAN columns, not int64.
	_, err = db.Exec(`
		CREATE TABLE agent_runs (
			agent_name TEXT, run_date TEXT, completed BOOLEAN, wall_time_s REAL,
			prompt_tokens INTEGER, completion_tokens INTEGER, thinking_tokens INTEGER,
			cache_read_tokens INTEGER, cache_write_tokens INTEGER, error_message TEXT
		);
		INSERT INTO agent_runs VALUES
			('summarizer', date('now'), 1, 42.5, 1000, 200, 0, 0, 0, NULL),
			('summarizer', date('now'), 0, 10.0, 500, 0, 0, 0, 0, 'boom');
	`)
	require.NoError(t, err)
}

func seedStatus(t *testing.T, dir string) {
	t.Helper()

	require.NoError(t, os.MkdirAll(dir, 0o755))

	status := `{
		"job": "pkm-data-sync", "mode": "data-only",
		"started": "2026-06-10T12:00:00Z", "finished": "2026-06-10T12:05:00Z",
		"overall_ok": true,
		"steps": [{"name": "pkm_sync_sync", "ok": true, "duration_s": 45, "critical": true}]
	}`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "pkm-data-sync.json"), []byte(status), 0o644))
}

func seedUserCache(t *testing.T, path string) {
	t.Helper()

	cache := `{"UHASH1": "Alice Anderson", "UHASH2": "Bob Brown"}`
	require.NoError(t, os.WriteFile(path, []byte(cache), 0o644))
}

// get performs an authenticated GET against the test server.
func get(t *testing.T, srv *Server, token, path string) *httptest.ResponseRecorder {
	t.Helper()

	req := httptest.NewRequest(http.MethodGet, path, nil)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	return rec
}

func decodeBody(t *testing.T, rec *httptest.ResponseRecorder) map[string]any {
	t.Helper()

	var body map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))

	return body
}

func TestAuthRequired(t *testing.T) {
	srv, token := newTestServer(t)

	assert.Equal(t, http.StatusUnauthorized, get(t, srv, "", "/api/status").Code)
	assert.Equal(t, http.StatusUnauthorized, get(t, srv, "wrong-token", "/api/status").Code)
	assert.Equal(t, http.StatusOK, get(t, srv, token, "/api/status").Code)

	// Probes and scrapes must work without a token.
	assert.Equal(t, http.StatusOK, get(t, srv, "", "/healthz").Code)
	assert.Equal(t, http.StatusOK, get(t, srv, "", "/metrics").Code)
}

func TestSearch(t *testing.T) {
	srv, token := newTestServer(t)

	rec := get(t, srv, token, "/api/search?q=deploy+failures&limit=5")
	require.Equal(t, http.StatusOK, rec.Code)

	body := decodeBody(t, rec)
	results, ok := body["results"].([]any)
	require.True(t, ok)
	require.NotEmpty(t, results)

	// The fake embedder returns [1,0,0]; doc t1 has the same vector, so it
	// must rank first with a perfect score.
	first, ok := results[0].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "Deploy failed in prod", first["title"])
	assert.Equal(t, "t1", first["thread_id"])
	assert.InDelta(t, 1.0, first["score"], 0.001)
}

func TestSearchRequiresQuery(t *testing.T) {
	srv, token := newTestServer(t)

	assert.Equal(t, http.StatusBadRequest, get(t, srv, token, "/api/search").Code)
}

func TestSearchSourceTypeFilter(t *testing.T) {
	srv, token := newTestServer(t)

	rec := get(t, srv, token, "/api/search?q=deploy&source_type=gmail")
	require.Equal(t, http.StatusOK, rec.Code)

	body := decodeBody(t, rec)
	assert.Equal(t, float64(0), body["total_results"])
}

func TestEmails(t *testing.T) {
	srv, token := newTestServer(t)

	rec := get(t, srv, token, "/api/emails?q=rosa")
	require.Equal(t, http.StatusOK, rec.Code)

	body := decodeBody(t, rec)
	require.Equal(t, float64(1), body["count"])

	results, ok := body["results"].([]any)
	require.True(t, ok)
	first, ok := results[0].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "ROSA boundary review", first["subject"])
	assert.Equal(t, "alice@example.com", first["from"])
	assert.Nil(t, first["body"]) // body excluded unless requested

	// With body=1 the FTS content body is included.
	rec = get(t, srv, token, "/api/emails?q=rosa&body=1")
	body = decodeBody(t, rec)
	results = body["results"].([]any)
	first = results[0].(map[string]any)
	assert.Contains(t, first["body"], "rosa boundary doc")
}

func TestEmailsFilterOnly(t *testing.T) {
	srv, token := newTestServer(t)

	rec := get(t, srv, token, "/api/emails?from=alice&since=2026-01-01")
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, float64(1), decodeBody(t, rec)["count"])

	assert.Equal(t, http.StatusBadRequest, get(t, srv, token, "/api/emails").Code)
}

func TestSlackMessages(t *testing.T) {
	srv, token := newTestServer(t)

	rec := get(t, srv, token, "/api/slack/messages?q=deploy")
	require.Equal(t, http.StatusOK, rec.Code)

	body := decodeBody(t, rec)
	require.Equal(t, float64(1), body["count"])

	results := body["results"].([]any)
	first := results[0].(map[string]any)
	assert.Equal(t, "forum-rosa-eng", first["channel"])
	assert.Equal(t, "Alice Anderson", first["author"]) // resolved from user cache
	assert.Equal(t, "UHASH1", first["author_id"])
}

func TestSlackAuthorFilter(t *testing.T) {
	srv, token := newTestServer(t)

	// Display-name substring resolves to the hashed ID via the cache.
	rec := get(t, srv, token, "/api/slack/messages?author=alice")
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, float64(1), decodeBody(t, rec)["count"])

	// Unknown author falls through as a literal ID and matches nothing.
	rec = get(t, srv, token, "/api/slack/messages?author=nobody-here")
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, float64(0), decodeBody(t, rec)["count"])
}

func TestStatus(t *testing.T) {
	srv, token := newTestServer(t)

	rec := get(t, srv, token, "/api/status")
	require.Equal(t, http.StatusOK, rec.Code)

	body := decodeBody(t, rec)
	require.Equal(t, float64(1), body["count"])

	jobs := body["jobs"].([]any)
	first := jobs[0].(map[string]any)
	assert.Equal(t, "pkm-data-sync", first["job"])
	assert.Equal(t, true, first["overall_ok"])
}

func TestMetrics(t *testing.T) {
	srv, _ := newTestServer(t)

	rec := get(t, srv, "", "/metrics")
	require.Equal(t, http.StatusOK, rec.Code)

	out := rec.Body.String()

	// Pipeline status metrics.
	assert.Contains(t, out, `pkm_run_overall_ok{job="pkm-data-sync"} 1`)
	assert.Contains(t, out, `pkm_step_success{job="pkm-data-sync",step="pkm_sync_sync"} 1`)
	assert.Contains(t, out, `pkm_step_duration_seconds{job="pkm-data-sync",step="pkm_sync_sync"} 45`)
	assert.Contains(t, out, `pkm_run_last_finish_timestamp{job="pkm-data-sync"}`)

	// Agent metrics: the latest run (rowid 2) failed.
	assert.Contains(t, out, `pkm_agent_last_run_ok{agent="summarizer"} 0`)
	assert.Contains(t, out, `pkm_agent_runs_7d_total{agent="summarizer"} 2`)
	assert.Contains(t, out, `pkm_agent_errors_7d_total{agent="summarizer"} 1`)
	assert.Contains(t, out, `pkm_agent_last_run_tokens{agent="summarizer",kind="prompt"} 500`)
	assert.Contains(t, out, `pkm_agent_errors_3run_total{agent="summarizer"} 1`)

	// Freshness for all three databases.
	assert.Contains(t, out, `pkm_source_newest_timestamp{source="slack_redhat",db="vectors"}`)
	assert.Contains(t, out, `pkm_source_newest_timestamp{source="gmail_work",db="archive"}`)
	assert.Contains(t, out, `pkm_source_newest_timestamp{source="slack",db="slack"}`)
}

func TestMetricsMissingFilesAreBestEffort(t *testing.T) {
	dir := t.TempDir()

	srv := New(Config{
		StatusDir:     filepath.Join(dir, "nope"),
		MetricsDBPath: filepath.Join(dir, "nope.db"),
		VectorDBPath:  filepath.Join(dir, "nope2.db"),
		ArchiveDBPath: filepath.Join(dir, "nope3.db"),
		SlackDBPath:   filepath.Join(dir, "nope4.db"),
		Dimensions:    3,
	}, nil)

	rec := get(t, srv, "", "/metrics")
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.NotContains(t, rec.Body.String(), "pkm_source_newest_timestamp{")
}

func TestSearchWithoutEmbedder(t *testing.T) {
	srv := New(Config{Dimensions: 3}, nil)

	rec := get(t, srv, "", "/api/search?q=anything")
	assert.Equal(t, http.StatusServiceUnavailable, rec.Code)
}

func TestHealthz(t *testing.T) {
	srv, _ := newTestServer(t)

	rec := get(t, srv, "", "/healthz")
	require.Equal(t, http.StatusOK, rec.Code)

	body := decodeBody(t, rec)
	assert.Equal(t, true, body["ok"])
	assert.Equal(t, true, body["vectors_db_exists"])
	assert.Equal(t, true, body["slack_db_exists"])
}

func TestQueryStore(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "vectors.db")

	seedVectors(t, path)

	store, err := vectorstore.NewQueryStore(path, 3)
	require.NoError(t, err)

	defer store.Close()

	results, err := store.Search([]float32{1, 0, 0}, 5, vectorstore.SearchFilters{})
	require.NoError(t, err)
	assert.NotEmpty(t, results)

	// A missing database is an immediate error, not a lazily-created file.
	_, err = vectorstore.NewQueryStore(filepath.Join(dir, "missing.db"), 3)
	assert.Error(t, err)
}
