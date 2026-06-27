package calculator

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// store.go persists the Scoring Generator as one document per app type
// (generator-radarr.json / generator-sonarr.json). A document holds the shared
// title pool plus the named sets (orderings/selections of the pool), mirroring
// how the Scoring Sandbox keeps its results + score sets together. Writes are
// atomic (temp + rename). Calculation itself is stateless (the API computes
// from a payload), so nothing here loads on the hot path.

// Store loads and saves Scoring Generator documents.
type Store struct {
	dir string
	mu  sync.Mutex
}

// NewStore returns a store rooted at dir (created on first save).
func NewStore(dir string) *Store { return &Store{dir: dir} }

func validApp(app string) bool { return app == "radarr" || app == "sonarr" }

func (s *Store) docPath(app string) (string, error) {
	if !validApp(app) {
		return "", fmt.Errorf("invalid app type")
	}
	return filepath.Join(s.dir, "generator-"+app+".json"), nil
}

// LoadDoc reads the document for an app type. A missing file yields an empty
// document (not an error) so first use just starts blank.
func (s *Store) LoadDoc(app string) (*GeneratorDoc, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, err := s.docPath(app)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(p)
	if err != nil {
		if os.IsNotExist(err) {
			return &GeneratorDoc{AppType: app, Pool: []PoolTitle{}, Sets: []GeneratorSet{}}, nil
		}
		return nil, err
	}
	var doc GeneratorDoc
	if err := json.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("corrupt generator document: %w", err)
	}
	doc.AppType = app
	if doc.Pool == nil {
		doc.Pool = []PoolTitle{}
	}
	if doc.Sets == nil {
		doc.Sets = []GeneratorSet{}
	}
	return &doc, nil
}

// SaveDoc writes the document for an app type atomically.
func (s *Store) SaveDoc(app string, doc *GeneratorDoc) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, err := s.docPath(app)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(s.dir, 0o755); err != nil {
		return err
	}
	doc.AppType = app
	data, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return err
	}
	tmp := p + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, p)
}
