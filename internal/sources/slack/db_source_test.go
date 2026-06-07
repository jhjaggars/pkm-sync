package slack

import (
	"database/sql"
	"os"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

func makeTestSlackDB(t *testing.T) string {
	t.Helper()

	f, err := os.CreateTemp(t.TempDir(), "slack-*.db")
	if err != nil {
		t.Fatal(err)
	}

	f.Close()

	db, err := sql.Open("sqlite3", f.Name())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	_, err = db.Exec(`CREATE TABLE slack_messages (
		rowid INTEGER PRIMARY KEY AUTOINCREMENT,
		id TEXT UNIQUE NOT NULL,
		channel_id TEXT NOT NULL,
		channel_name TEXT NOT NULL,
		workspace TEXT NOT NULL,
		author TEXT NOT NULL,
		content TEXT NOT NULL,
		message_url TEXT NOT NULL,
		item_type TEXT NOT NULL,
		thread_ts TEXT NOT NULL DEFAULT '',
		is_thread_root INTEGER NOT NULL DEFAULT 0,
		reply_count INTEGER NOT NULL DEFAULT 0,
		created_at TEXT NOT NULL,
		synced_at TEXT NOT NULL
	)`)
	if err != nil {
		t.Fatal(err)
	}

	rows := []struct {
		id, channel, author, content, threadTs, createdAt string
		isRoot                                             int
	}{
		{"m1", "general", "alice", "hello world", "ts1", "2024-01-01T10:00:00Z", 1},
		{"m2", "general", "bob", "reply", "ts1", "2024-01-01T10:01:00Z", 0},
		{"m3", "general", "alice", "new thread", "ts3", "2024-06-01T09:00:00Z", 1},
	}

	for _, r := range rows {
		_, err := db.Exec(`INSERT INTO slack_messages
			(id, channel_id, channel_name, workspace, author, content, message_url,
			 item_type, thread_ts, is_thread_root, reply_count, created_at, synced_at)
			VALUES (?, ?, ?, 'test-ws', ?, ?, '', 'message', ?, ?, 0, ?, ?)`,
			r.id, r.channel, r.channel, r.author, r.content, r.threadTs, r.isRoot, r.createdAt, r.createdAt)
		if err != nil {
			t.Fatal(err)
		}
	}

	return f.Name()
}

func TestDBSource_Fetch(t *testing.T) {
	dbPath := makeTestSlackDB(t)

	src, err := NewDBSource(dbPath)
	if err != nil {
		t.Fatalf("NewDBSource: %v", err)
	}
	defer src.Close()

	// Fetch all items.
	items, err := src.Fetch(time.Time{}, 100)
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if len(items) != 3 {
		t.Errorf("expected 3 items, got %d", len(items))
	}

	// Fetch only items after June 2024.
	since := time.Date(2024, 6, 1, 0, 0, 0, 0, time.UTC)
	items, err = src.Fetch(since, 100)
	if err != nil {
		t.Fatalf("Fetch with since: %v", err)
	}
	if len(items) != 1 {
		t.Errorf("expected 1 item after since filter, got %d", len(items))
	}
	if items[0].GetID() != "m3" {
		t.Errorf("expected item m3, got %s", items[0].GetID())
	}

	// Verify metadata fields are populated.
	meta := items[0].GetMetadata()
	if meta["author"] != "alice" {
		t.Errorf("expected author alice, got %v", meta["author"])
	}
	if meta["thread_id"] != "ts3" {
		t.Errorf("expected thread_id ts3, got %v", meta["thread_id"])
	}
	if meta["is_thread_root"] != true {
		t.Errorf("expected is_thread_root true, got %v", meta["is_thread_root"])
	}
}

func TestDBSource_MissingDB(t *testing.T) {
	_, err := NewDBSource("/nonexistent/path/slack.db")
	if err == nil {
		t.Error("expected error for missing DB")
	}
}
