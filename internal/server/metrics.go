package server

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// This file is a Go port of the former Python pkm-sync-exporter. Metric and
// label names are kept identical so existing dashboards and PrometheusRule
// alerts keep working: pkm_step_success, pkm_step_duration_seconds,
// pkm_run_last_finish_timestamp, pkm_run_overall_ok, pkm_agent_*,
// pkm_source_newest_timestamp.

// sample is one metric line: a label set and a value.
type sample struct {
	labels [][2]string
	value  float64
}

// handleMetrics renders all metrics in the Prometheus text exposition format,
// rebuilt from the data files on every scrape. All reads are best-effort:
// missing or corrupt files produce empty metric sets, never errors.
func (s *Server) handleMetrics(w http.ResponseWriter, _ *http.Request) {
	var b strings.Builder

	s.writePipelineMetrics(&b)
	s.writeAgentMetrics(&b)
	s.writeFreshnessMetrics(&b)

	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	_, _ = io.WriteString(w, b.String())
}

// pipelineStatus mirrors the JSON written by the orchestration scripts.
type pipelineStatus struct {
	Job       string `json:"job"`
	Finished  string `json:"finished"`
	OverallOK bool   `json:"overall_ok"`
	Steps     []struct {
		Name      string  `json:"name"`
		OK        bool    `json:"ok"`
		DurationS float64 `json:"duration_s"`
	} `json:"steps"`
}

func (s *Server) writePipelineMetrics(b *strings.Builder) {
	var stepSuccess, stepDuration, runFinish, runOK []sample

	for _, raw := range s.readStatusFiles() {
		var status pipelineStatus
		if err := json.Unmarshal(raw, &status); err != nil || status.Job == "" {
			continue
		}

		jobLabel := [2]string{"job", status.Job}

		if ts, ok := parseTimestamp(status.Finished); ok {
			runFinish = append(runFinish, sample{[][2]string{jobLabel}, ts})
		}

		runOK = append(runOK, sample{[][2]string{jobLabel}, boolToFloat(status.OverallOK)})

		for _, step := range status.Steps {
			labels := [][2]string{jobLabel, {"step", step.Name}}
			stepSuccess = append(stepSuccess, sample{labels, boolToFloat(step.OK)})
			stepDuration = append(stepDuration, sample{labels, step.DurationS})
		}
	}

	writeGauge(b, "pkm_step_success",
		"1 if the most recent pipeline step succeeded, 0 if it failed.", stepSuccess)
	writeGauge(b, "pkm_step_duration_seconds",
		"Duration of the most recent execution of each pipeline step.", stepDuration)
	writeGauge(b, "pkm_run_last_finish_timestamp",
		"Unix timestamp when the pipeline job last finished.", runFinish)
	writeGauge(b, "pkm_run_overall_ok",
		"1 if the last pipeline run reported overall_ok=true.", runOK)
}

func (s *Server) writeAgentMetrics(b *strings.Builder) {
	var lastTS, lastOK, lastWall, tokens, runs7d, errors7d, errors3run []sample

	db, err := s.db(s.cfg.MetricsDBPath)
	if err == nil {
		lastTS, lastOK, lastWall, tokens = collectLatestAgentRuns(db)
		runs7d, errors7d = collectAgentAggregates(db)
		errors3run = collectAgentErrors3Run(db)
	}

	writeGauge(b, "pkm_agent_last_run_timestamp",
		"Unix timestamp of the most recent agent run (based on run_date).", lastTS)
	writeGauge(b, "pkm_agent_last_run_ok",
		"1 if the most recent agent run completed without error.", lastOK)
	writeGauge(b, "pkm_agent_last_run_wall_time_seconds",
		"Wall-clock time in seconds for the most recent agent run.", lastWall)
	writeGauge(b, "pkm_agent_last_run_tokens",
		"Token count from the most recent agent run.", tokens)
	writeGauge(b, "pkm_agent_runs_7d_total",
		"Number of agent runs recorded in the last 7 days.", runs7d)
	writeGauge(b, "pkm_agent_errors_7d_total",
		"Number of failed/errored agent runs in the last 7 days.", errors7d)
	writeGauge(b, "pkm_agent_errors_3run_total",
		"Number of failed/errored runs among the last 3 runs per agent.", errors3run)
}

// collectLatestAgentRuns reads the most recent run per agent from
// agent_metrics.db (highest rowid per agent_name).
func collectLatestAgentRuns(db *sql.DB) (lastTS, lastOK, lastWall, tokens []sample) {
	rows, err := db.Query(`
		SELECT agent_name, run_date, completed, wall_time_s,
		       prompt_tokens, completion_tokens, thinking_tokens,
		       cache_read_tokens, cache_write_tokens
		FROM agent_runs
		WHERE rowid IN (SELECT MAX(rowid) FROM agent_runs GROUP BY agent_name)
	`)
	if err != nil {
		return nil, nil, nil, nil
	}
	defer rows.Close()

	tokenKinds := []string{"prompt", "completion", "thinking", "cache_read", "cache_write"}

	for rows.Next() {
		var (
			agent     string
			runDate   sql.NullString
			completed any // BOOLEAN column: the driver may return bool or int64
			wallTime  sql.NullFloat64
			counts    [5]sql.NullFloat64
		)

		err := rows.Scan(&agent, &runDate, &completed, &wallTime,
			&counts[0], &counts[1], &counts[2], &counts[3], &counts[4])
		if err != nil {
			continue
		}

		agentLabel := [2]string{"agent", agent}

		if ts, ok := parseTimestamp(runDate.String); ok {
			lastTS = append(lastTS, sample{[][2]string{agentLabel}, ts})
		}

		lastOK = append(lastOK, sample{[][2]string{agentLabel}, boolToFloat(truthy(completed))})
		lastWall = append(lastWall, sample{[][2]string{agentLabel}, wallTime.Float64})

		for i, kind := range tokenKinds {
			labels := [][2]string{agentLabel, {"kind", kind}}
			tokens = append(tokens, sample{labels, counts[i].Float64})
		}
	}

	return lastTS, lastOK, lastWall, tokens
}

// collectAgentErrors3Run counts failures among the last 3 runs per agent.
func collectAgentErrors3Run(db *sql.DB) []sample {
	rows, err := db.Query(`
		SELECT agent_name,
		       SUM(CASE WHEN completed = 0 OR error_message IS NOT NULL THEN 1 ELSE 0 END)
		FROM (
		    SELECT agent_name, completed, error_message,
		           ROW_NUMBER() OVER (PARTITION BY agent_name ORDER BY rowid DESC) AS rn
		    FROM agent_runs
		)
		WHERE rn <= 3
		GROUP BY agent_name
	`)
	if err != nil {
		return nil
	}
	defer rows.Close()

	var samples []sample

	for rows.Next() {
		var (
			agent  string
			errCnt sql.NullFloat64
		)

		if err := rows.Scan(&agent, &errCnt); err != nil {
			continue
		}

		samples = append(samples, sample{[][2]string{{"agent", agent}}, errCnt.Float64})
	}

	return samples
}

// collectAgentAggregates reads 7-day run/error counts per agent.
func collectAgentAggregates(db *sql.DB) (runs7d, errors7d []sample) {
	rows, err := db.Query(`
		SELECT agent_name,
		       COUNT(*) AS runs_7d,
		       SUM(CASE WHEN completed = 0 OR error_message IS NOT NULL THEN 1 ELSE 0 END) AS errors_7d
		FROM agent_runs
		WHERE run_date >= date('now', '-7 days')
		GROUP BY agent_name
	`)
	if err != nil {
		return nil, nil
	}
	defer rows.Close()

	for rows.Next() {
		var (
			agent        string
			runs, errCnt sql.NullFloat64
		)

		if err := rows.Scan(&agent, &runs, &errCnt); err != nil {
			continue
		}

		agentLabel := [][2]string{{"agent", agent}}
		runs7d = append(runs7d, sample{agentLabel, runs.Float64})
		errors7d = append(errors7d, sample{agentLabel, errCnt.Float64})
	}

	return runs7d, errors7d
}

func (s *Server) writeFreshnessMetrics(b *strings.Builder) {
	samples := s.collectFreshness(s.cfg.VectorDBPath, "vectors",
		"SELECT source_name, MAX(updated_at) FROM documents GROUP BY source_name")
	samples = append(samples, s.collectArchiveFreshness()...)
	samples = append(samples, s.collectFreshness(s.cfg.SlackDBPath, "slack",
		"SELECT 'slack', MAX(created_at) FROM slack_messages")...)

	writeGauge(b, "pkm_source_newest_timestamp",
		"Unix timestamp of the newest record for each data source.", samples)
}

// collectFreshness runs a (source, timestamp) query against a database and
// converts the rows into pkm_source_newest_timestamp samples.
func (s *Server) collectFreshness(dbPath, dbLabel, query string) []sample {
	db, err := s.db(dbPath)
	if err != nil {
		return nil
	}

	rows, err := db.Query(query)
	if err != nil {
		return nil
	}
	defer rows.Close()

	var samples []sample

	for rows.Next() {
		var source, tsStr sql.NullString

		if err := rows.Scan(&source, &tsStr); err != nil {
			continue
		}

		if ts, ok := parseTimestamp(tsStr.String); ok {
			labels := [][2]string{{"source", source.String}, {"db", dbLabel}}
			samples = append(samples, sample{labels, ts})
		}
	}

	return samples
}

// collectArchiveFreshness reads per-source sync state from archive.db,
// falling back to the newest archived message when sync_state is empty.
func (s *Server) collectArchiveFreshness() []sample {
	samples := s.collectFreshness(s.cfg.ArchiveDBPath, "archive",
		"SELECT source_name, last_sync_time FROM sync_state")
	if len(samples) > 0 {
		return samples
	}

	return s.collectFreshness(s.cfg.ArchiveDBPath, "archive",
		"SELECT 'all', MAX(date_archived) FROM messages")
}

// writeGauge emits one gauge metric family: HELP/TYPE header plus samples.
// Families with no samples are skipped entirely, matching prometheus_client.
func writeGauge(b *strings.Builder, name, help string, samples []sample) {
	if len(samples) == 0 {
		return
	}

	fmt.Fprintf(b, "# HELP %s %s\n", name, help)
	fmt.Fprintf(b, "# TYPE %s gauge\n", name)

	for _, smp := range samples {
		b.WriteString(name)

		if len(smp.labels) > 0 {
			parts := make([]string, len(smp.labels))
			for i, kv := range smp.labels {
				parts[i] = kv[0] + `="` + escapeLabelValue(kv[1]) + `"`
			}

			b.WriteString("{" + strings.Join(parts, ",") + "}")
		}

		b.WriteString(" " + strconv.FormatFloat(smp.value, 'g', -1, 64) + "\n")
	}
}

// escapeLabelValue escapes a label value per the Prometheus text format.
func escapeLabelValue(v string) string {
	r := strings.NewReplacer(`\`, `\\`, `"`, `\"`, "\n", `\n`)

	return r.Replace(v)
}

// truthy interprets a scanned SQLite value as a boolean. Columns declared
// BOOLEAN come back as bool from the driver; INTEGER columns come back int64.
func truthy(v any) bool {
	switch val := v.(type) {
	case bool:
		return val
	case int64:
		return val != 0
	default:
		return false
	}
}

// boolToFloat converts a bool to a 1/0 gauge value.
func boolToFloat(v bool) float64 {
	if v {
		return 1.0
	}

	return 0.0
}

// timestampLayouts are the formats accepted for timestamps found in status
// files and databases, matching the Python exporter's parser.
var timestampLayouts = []string{
	"2006-01-02T15:04:05Z",
	"2006-01-02T15:04:05",
	"2006-01-02 15:04:05",
	"2006-01-02",
	time.RFC3339,
}

// parseTimestamp converts a timestamp string to Unix seconds. Naive
// timestamps are interpreted as UTC.
func parseTimestamp(raw string) (float64, bool) {
	if raw == "" {
		return 0, false
	}

	for _, layout := range timestampLayouts {
		if t, err := time.ParseInLocation(layout, raw, time.UTC); err == nil {
			return float64(t.Unix()), true
		}
	}

	return 0, false
}
