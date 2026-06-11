package server

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"pkm-sync/internal/vectorstore"
)

const (
	defaultSearchLimit = 10
	defaultEmailLimit  = 10
	defaultSlackLimit  = 20
	maxLimit           = 200
)

// handleSearch performs semantic (vector KNN) search over vectors.db.
// Query params: q (required), source_type, source_name, limit, min_score.
// The response shape matches `pkm-sync search --format json`.
func (s *Server) handleSearch(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query().Get("q")
	if query == "" {
		writeError(w, http.StatusBadRequest, "missing required query parameter: q")

		return
	}

	if s.embedder == nil {
		writeError(w, http.StatusServiceUnavailable, "no embedding provider configured; semantic search unavailable")

		return
	}

	minScore := 0.0

	if raw := r.URL.Query().Get("min_score"); raw != "" {
		if v, err := strconv.ParseFloat(raw, 64); err == nil {
			minScore = v
		}
	}

	embedding, err := s.embedder.Embed(r.Context(), query)
	if err != nil {
		writeError(w, http.StatusBadGateway, "failed to embed query: "+err.Error())

		return
	}

	store, err := s.vectors()
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "vector store unavailable: "+err.Error())

		return
	}

	filters := vectorstore.SearchFilters{
		SourceType: r.URL.Query().Get("source_type"),
		SourceName: r.URL.Query().Get("source_name"),
		MinScore:   minScore,
	}

	results, err := store.Search(embedding, queryInt(r, "limit", defaultSearchLimit, maxLimit), filters)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "search failed: "+err.Error())

		return
	}

	type searchResult struct {
		Score        float64        `json:"score"`
		ThreadID     string         `json:"thread_id"`
		Title        string         `json:"title"`
		Content      string         `json:"content"`
		SourceType   string         `json:"source_type"`
		SourceName   string         `json:"source_name"`
		MessageCount int            `json:"message_count"`
		CreatedAt    string         `json:"created_at"`
		UpdatedAt    string         `json:"updated_at"`
		Metadata     map[string]any `json:"metadata"`
	}

	out := make([]searchResult, len(results))
	for i, res := range results {
		out[i] = searchResult{
			Score:        res.Score,
			ThreadID:     res.ThreadID,
			Title:        res.Title,
			Content:      res.Content,
			SourceType:   res.SourceType,
			SourceName:   res.SourceName,
			MessageCount: res.MessageCount,
			CreatedAt:    res.CreatedAt.Format(time.RFC3339),
			UpdatedAt:    res.UpdatedAt.Format(time.RFC3339),
			Metadata:     res.Metadata,
		}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"query":         query,
		"total_results": len(out),
		"results":       out,
	})
}

// handleEmails searches the Gmail archive (archive.db). Query params: q
// (FTS4 MATCH), from (sender substring), since (date_sent >= YYYY-MM-DD),
// limit, body (include body text). At least one of q/from/since is required.
// The query and response shapes match the pkm-search skill's email_search.py.
func (s *Server) handleEmails(w http.ResponseWriter, r *http.Request) {
	params := r.URL.Query()
	query, fromAddr, since := params.Get("q"), params.Get("from"), params.Get("since")
	includeBody := params.Get("body") == "1" || params.Get("body") == "true"

	if query == "" && fromAddr == "" && since == "" {
		writeError(w, http.StatusBadRequest, "provide at least one of: q, from, since")

		return
	}

	db, err := s.db(s.cfg.ArchiveDBPath)
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, err.Error())

		return
	}

	const selectCols = `SELECT m.gmail_id, m.thread_id, m.subject, m.from_addr,
		m.to_addrs, m.cc_addrs, m.date_sent, m.source_name, fc.c1body`

	var (
		sqlText string
		args    []any
	)

	if query != "" {
		sqlText = selectCols + `
			FROM messages_fts f
			JOIN messages m ON f.rowid = m.rowid
			JOIN messages_fts_content fc ON fc.docid = m.rowid
			WHERE messages_fts MATCH ?`

		args = append(args, query)
	} else {
		sqlText = selectCols + `
			FROM messages m
			LEFT JOIN messages_fts_content fc ON fc.docid = m.rowid
			WHERE 1=1`
	}

	if fromAddr != "" {
		sqlText += " AND m.from_addr LIKE ?"

		args = append(args, "%"+fromAddr+"%")
	}

	if since != "" {
		sqlText += " AND m.date_sent >= ?"

		args = append(args, since)
	}

	sqlText += " ORDER BY m.date_sent DESC LIMIT ?"

	args = append(args, queryInt(r, "limit", defaultEmailLimit, maxLimit))

	rows, err := db.Query(sqlText, args...)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "email search failed: "+err.Error())

		return
	}
	defer rows.Close()

	type emailResult struct {
		Subject  string   `json:"subject"`
		From     string   `json:"from"`
		To       []string `json:"to"`
		CC       []string `json:"cc"`
		Date     string   `json:"date"`
		GmailID  string   `json:"gmail_id"`
		ThreadID string   `json:"thread_id"`
		Source   string   `json:"source"`
		Body     *string  `json:"body,omitempty"`
	}

	results := []emailResult{}

	for rows.Next() {
		var (
			res                      emailResult
			toRaw, ccRaw             string
			body                     sql.NullString
			gmailID, threadID, srcNm string
		)

		err := rows.Scan(&gmailID, &threadID, &res.Subject, &res.From, &toRaw, &ccRaw, &res.Date, &srcNm, &body)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to scan email row: "+err.Error())

			return
		}

		res.GmailID, res.ThreadID, res.Source = gmailID, threadID, srcNm
		res.To, res.CC = parseAddrList(toRaw), parseAddrList(ccRaw)

		if includeBody {
			bodyText := body.String
			res.Body = &bodyText
		}

		results = append(results, res)
	}

	if err := rows.Err(); err != nil {
		writeError(w, http.StatusInternalServerError, "email search failed: "+err.Error())

		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"query":   query,
		"filters": map[string]string{"from": fromAddr, "since": since},
		"count":   len(results),
		"results": results,
	})
}

// parseAddrList decodes the JSON-encoded address arrays stored in archive.db.
func parseAddrList(raw string) []string {
	if raw == "" {
		return []string{}
	}

	var addrs []string
	if err := json.Unmarshal([]byte(raw), &addrs); err != nil {
		return []string{raw}
	}

	return addrs
}

// handleSlackMessages performs keyword search over slack.db. Query params:
// q (content substring), channel (exact channel name), author (display-name
// substring or raw user ID), since (created_at >= RFC3339 or YYYY-MM-DD),
// limit. Author IDs are resolved to display names via the user cache; the
// author column itself holds opaque hashed IDs.
func (s *Server) handleSlackMessages(w http.ResponseWriter, r *http.Request) {
	params := r.URL.Query()

	db, err := s.db(s.cfg.SlackDBPath)
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, err.Error())

		return
	}

	userCache := s.loadUserCache()

	where := []string{"1=1"}

	var args []any

	if q := params.Get("q"); q != "" {
		where = append(where, "content LIKE ?")
		args = append(args, "%"+q+"%")
	}

	if channel := params.Get("channel"); channel != "" {
		where = append(where, "channel_name = ?")
		args = append(args, channel)
	}

	if since := params.Get("since"); since != "" {
		where = append(where, "created_at >= ?")
		args = append(args, since)
	}

	if author := params.Get("author"); author != "" {
		// The author column stores display names for most rows, but older
		// rows may hold raw user IDs — match the name directly and any
		// cached IDs whose display name matches.
		clause := "(author LIKE ?"

		args = append(args, "%"+author+"%")

		ids := resolveAuthorIDs(author, userCache)

		if len(ids) > 0 {
			placeholders := strings.Repeat("?,", len(ids))
			clause += " OR author IN (" + placeholders[:len(placeholders)-1] + ")"

			for _, id := range ids {
				args = append(args, id)
			}
		}

		where = append(where, clause+")")
	}

	sqlText := `SELECT id, channel_name, author, content, message_url, thread_ts,
		is_thread_root, reply_count, created_at
		FROM slack_messages WHERE ` + strings.Join(where, " AND ") +
		" ORDER BY created_at DESC LIMIT ?"

	args = append(args, queryInt(r, "limit", defaultSlackLimit, maxLimit))

	rows, err := db.Query(sqlText, args...)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "slack search failed: "+err.Error())

		return
	}
	defer rows.Close()

	type slackResult struct {
		ID           string `json:"id"`
		Channel      string `json:"channel"`
		AuthorID     string `json:"author_id"`
		Author       string `json:"author"`
		Content      string `json:"content"`
		URL          string `json:"url"`
		ThreadTS     string `json:"thread_ts"`
		IsThreadRoot bool   `json:"is_thread_root"`
		ReplyCount   int    `json:"reply_count"`
		CreatedAt    string `json:"created_at"`
	}

	results := []slackResult{}

	for rows.Next() {
		var res slackResult

		err := rows.Scan(&res.ID, &res.Channel, &res.AuthorID, &res.Content, &res.URL,
			&res.ThreadTS, &res.IsThreadRoot, &res.ReplyCount, &res.CreatedAt)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to scan slack row: "+err.Error())

			return
		}

		res.Author = res.AuthorID
		if name, ok := userCache[res.AuthorID]; ok {
			res.Author = name
		}

		results = append(results, res)
	}

	if err := rows.Err(); err != nil {
		writeError(w, http.StatusInternalServerError, "slack search failed: "+err.Error())

		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"count": len(results), "results": results})
}

// loadUserCache reads the Slack user cache (user ID -> display name).
// A missing or unreadable cache yields an empty map; results then show IDs.
func (s *Server) loadUserCache() map[string]string {
	cache := map[string]string{}

	data, err := os.ReadFile(s.cfg.UserCachePath)
	if err != nil {
		return cache
	}

	_ = json.Unmarshal(data, &cache)

	return cache
}

// resolveAuthorIDs returns the user IDs whose display name contains the given
// term (case-insensitive). A term that exactly matches a cached ID is also
// accepted, so callers can pass raw IDs.
func resolveAuthorIDs(term string, cache map[string]string) []string {
	var ids []string

	lower := strings.ToLower(term)

	for id, name := range cache {
		if id == term || strings.Contains(strings.ToLower(name), lower) {
			ids = append(ids, id)
		}
	}

	if len(ids) == 0 {
		// Unknown to the cache — pass through as a literal ID so direct ID
		// queries still work before the cache has been populated.
		ids = append(ids, term)
	}

	return ids
}

// handleStatus returns the parsed contents of every pipeline-status JSON
// file, the same files the orchestration scripts write per job.
func (s *Server) handleStatus(w http.ResponseWriter, _ *http.Request) {
	jobs := []map[string]any{}

	for _, raw := range s.readStatusFiles() {
		var parsed map[string]any
		if err := json.Unmarshal(raw, &parsed); err != nil {
			continue
		}

		if _, ok := parsed["job"]; ok {
			jobs = append(jobs, parsed)
		}
	}

	writeJSON(w, http.StatusOK, map[string]any{"count": len(jobs), "jobs": jobs})
}

// readStatusFiles returns the raw bytes of each *.json file in the status
// directory, in sorted filename order. Unreadable files are skipped.
func (s *Server) readStatusFiles() [][]byte {
	paths, err := filepath.Glob(filepath.Join(s.cfg.StatusDir, "*.json"))
	if err != nil {
		return nil
	}

	var out [][]byte

	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}

		out = append(out, data)
	}

	return out
}
