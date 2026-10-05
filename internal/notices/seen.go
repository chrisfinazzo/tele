package notices

import (
	"database/sql"
	"encoding/json"
	"os"
	"strconv"
	"sync"
	"time"
)

// Seen records which notices have already been shown.
//
// This deliberately does not live on store.Store: that interface is already
// wide, and seen-state is app-level preference data rather than account state.
type Seen interface {
	IsSeen(id string) bool
	MarkSeen(id string)
}

// keyPrefix namespaces notice rows inside the shared metadata table.
const keyPrefix = "notice:"

type sqliteSeen struct{ db *sql.DB }

// NewSQLiteSeen stores seen-state in the existing metadata key/value table.
func NewSQLiteSeen(db *sql.DB) Seen { return &sqliteSeen{db: db} }

func (s *sqliteSeen) IsSeen(id string) bool {
	var v string
	err := s.db.QueryRow(`SELECT value FROM metadata WHERE key = ?`, keyPrefix+id).Scan(&v)
	return err == nil
}

// MarkSeen is best effort: failing to record a notice means showing it once
// more, which is strictly better than failing startup over it.
func (s *sqliteSeen) MarkSeen(id string) {
	_, _ = s.db.Exec(
		`INSERT OR REPLACE INTO metadata (key, value) VALUES (?, ?)`,
		keyPrefix+id, strconv.FormatInt(time.Now().Unix(), 10),
	)
}

// fileSeen keeps seen-state in a file of its own in the state directory. A
// notice is about the machine - where the data lives, how many instances may
// run - rather than about the account, so its seen-state stays out of the
// account's database, which goes with every log out (#297).
type fileSeen struct {
	path string
	mu   sync.Mutex
	ids  map[string]int64
}

// NewFileSeen reads seen-state from path, and reports whether the file was
// there. A file that cannot be read sees nothing: a notice shown once more is
// strictly better than failing startup over it.
func NewFileSeen(path string) (Seen, bool) {
	s := &fileSeen{path: path, ids: map[string]int64{}}
	raw, err := os.ReadFile(path)
	if err != nil {
		return s, false
	}
	_ = json.Unmarshal(raw, &s.ids)
	if s.ids == nil {
		s.ids = map[string]int64{}
	}
	return s, true
}

func (s *fileSeen) IsSeen(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, ok := s.ids[id]
	return ok
}

// MarkSeen rewrites the file whole. Best effort, as for the database.
func (s *fileSeen) MarkSeen(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ids[id] = time.Now().Unix()
	raw, err := json.Marshal(s.ids)
	if err != nil {
		return
	}
	_ = os.WriteFile(s.path, raw, 0o600)
}

// CarryOver copies into to what from has seen of ids. It is how seen-state an
// earlier build kept in the account's database reaches the file, once.
func CarryOver(to, from Seen, ids []string) {
	for _, id := range ids {
		if from.IsSeen(id) {
			to.MarkSeen(id)
		}
	}
}

type memorySeen struct{ ids map[string]bool }

// NewMemorySeen is the non-persistent implementation used by tests.
func NewMemorySeen() Seen { return &memorySeen{ids: map[string]bool{}} }

func (m *memorySeen) IsSeen(id string) bool { return m.ids[id] }
func (m *memorySeen) MarkSeen(id string)    { m.ids[id] = true }
