package main

import (
	"database/sql"
	"encoding/json"
	"errors"
	"log"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"
)

const dbSchema = `
CREATE TABLE IF NOT EXISTS tasks (
  id TEXT PRIMARY KEY,
  kind TEXT NOT NULL,
  status TEXT NOT NULL DEFAULT 'pending',
  input_json TEXT NOT NULL DEFAULT '',
  output_json TEXT NOT NULL DEFAULT '',
  error TEXT NOT NULL DEFAULT '',
  progress_done INTEGER NOT NULL DEFAULT 0,
  progress_total INTEGER NOT NULL DEFAULT 0,
  progress_msg TEXT NOT NULL DEFAULT '',
  created_at DATETIME NOT NULL,
  updated_at DATETIME NOT NULL
);

CREATE TABLE IF NOT EXISTS state (
  key TEXT PRIMARY KEY,
  value TEXT NOT NULL,
  updated_at DATETIME NOT NULL
);
`

type task struct {
	ID            string    `json:"id"`
	Kind          string    `json:"kind"`
	Status        string    `json:"status"`
	InputJSON     string    `json:"inputJson,omitempty"`
	OutputJSON    string    `json:"outputJson,omitempty"`
	Error         string    `json:"error,omitempty"`
	ProgressDone  int       `json:"progressDone"`
	ProgressTotal int       `json:"progressTotal"`
	ProgressMsg   string    `json:"progressMsg,omitempty"`
	CreatedAt     time.Time `json:"createdAt"`
	UpdatedAt     time.Time `json:"updatedAt"`
}

type enhanceStyleInput struct {
	APIKey, TextModel, Title, Style, Workspace, ReferencePath string
}

func (s *server) openWorkspaceDB(workspace string) (*sql.DB, error) {
	if v, ok := s.dbs.Load(workspace); ok {
		return v.(*sql.DB), nil
	}
	path := filepath.Join(s.workspaceDir(workspace), "tasks.db")
	db, err := sql.Open("sqlite", path+"?_journal_mode=WAL&_busy_timeout=5000")
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(dbSchema); err != nil {
		_ = db.Close()
		return nil, err
	}
	actual, loaded := s.dbs.LoadOrStore(workspace, db)
	if loaded {
		_ = db.Close()
		return actual.(*sql.DB), nil
	}
	return db, nil
}

func newTaskID() string {
	return newWorkspaceID("")
}

func createTask(db *sql.DB, kind, taskID, inputJSON string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := db.Exec(`INSERT INTO tasks (id, kind, status, input_json, created_at, updated_at)
		VALUES (?, ?, 'pending', ?, ?, ?)`,
		taskID, kind, inputJSON, now, now)
	return err
}

func startTask(db *sql.DB, taskID string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := db.Exec(`UPDATE tasks SET status='running', updated_at=? WHERE id=?`, now, taskID)
	return err
}

func updateTaskProgress(db *sql.DB, taskID string, done, total int, msg string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := db.Exec(`UPDATE tasks SET progress_done=?, progress_total=?, progress_msg=?, updated_at=? WHERE id=?`,
		done, total, msg, now, taskID)
	return err
}

func completeTask(db *sql.DB, taskID, outputJSON string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := db.Exec(`UPDATE tasks SET status='done', output_json=?, updated_at=? WHERE id=?`,
		outputJSON, now, taskID)
	return err
}

func failTask(db *sql.DB, taskID, errMsg string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := db.Exec(`UPDATE tasks SET status='failed', error=?, updated_at=? WHERE id=?`,
		errMsg, now, taskID)
	return err
}

func getTask(db *sql.DB, taskID string) (task, error) {
	row := db.QueryRow(`SELECT id, kind, status, input_json, output_json, error,
		progress_done, progress_total, progress_msg, created_at, updated_at
		FROM tasks WHERE id=?`, taskID)
	return scanTask(row)
}

func listTasks(db *sql.DB) ([]task, error) {
	rows, err := db.Query(`SELECT id, kind, status, input_json, output_json, error,
		progress_done, progress_total, progress_msg, created_at, updated_at
		FROM tasks ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var tasks []task
	for rows.Next() {
		t, err := scanTask(rows)
		if err != nil {
			return nil, err
		}
		tasks = append(tasks, t)
	}
	return tasks, rows.Err()
}

type taskScanner interface {
	Scan(dest ...any) error
}

func scanTask(s taskScanner) (task, error) {
	var t task
	var createdAt, updatedAt string
	err := s.Scan(
		&t.ID, &t.Kind, &t.Status,
		&t.InputJSON, &t.OutputJSON, &t.Error,
		&t.ProgressDone, &t.ProgressTotal, &t.ProgressMsg,
		&createdAt, &updatedAt,
	)
	if err != nil {
		return t, err
	}
	t.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
	t.UpdatedAt, _ = time.Parse(time.RFC3339, updatedAt)
	return t, nil
}

func setState(db *sql.DB, key, value string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := db.Exec(`INSERT INTO state(key, value, updated_at) VALUES(?, ?, ?)
		ON CONFLICT(key) DO UPDATE SET value=excluded.value, updated_at=excluded.updated_at`,
		key, value, now)
	return err
}

func getAllState(db *sql.DB) (map[string]string, error) {
	rows, err := db.Query(`SELECT key, value FROM state`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	m := map[string]string{}
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			return nil, err
		}
		m[k] = v
	}
	return m, rows.Err()
}

func markStaleTasksFailed(db *sql.DB) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := db.Exec(`UPDATE tasks SET status='failed', error='server restarted', updated_at=?
		WHERE status='running' OR status='pending'`, now)
	return err
}

func (s *server) startupCleanup() {
	entries, err := os.ReadDir(s.cfg.WorkDir)
	if err != nil {
		return
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		workspace := e.Name()
		dbPath := filepath.Join(s.cfg.WorkDir, workspace, "tasks.db")
		if _, err := os.Stat(dbPath); err != nil {
			continue
		}
		db, err := s.openWorkspaceDB(workspace)
		if err != nil {
			log.Printf("startup cleanup: open db for %s: %v", workspace, err)
			continue
		}
		if err := markStaleTasksFailed(db); err != nil {
			log.Printf("startup cleanup: mark failed for %s: %v", workspace, err)
		}
	}
}

func taskJSONOutput(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return `{"error":"marshal failed"}`
	}
	return string(b)
}

func taskLogErr(label string, err error) {
	if err != nil {
		log.Printf("%s: %v", label, err)
	}
}

func errIsNotFound(err error) bool {
	return errors.Is(err, sql.ErrNoRows)
}

func (s *server) taskProgress(db *sql.DB, taskID string, done, total int, msg string) {
	if err := updateTaskProgress(db, taskID, done, total, msg); err != nil {
		log.Printf("updateTaskProgress %s: %v", taskID, err)
	}
}
