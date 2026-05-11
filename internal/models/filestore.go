package models

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// ErrNotFound is returned by FileStore.Get/Delete when no metadata file
// exists for the given id.
var ErrNotFound = errors.New("model metadata not found")

// FileStore is the on-disk implementation of cli.ModelStore. Each model id
// maps to one JSON file under dir.
type FileStore struct {
	dir string
}

// NewFileStore returns a FileStore writing to dir. The directory is created
// lazily on first Put.
func NewFileStore(dir string) *FileStore {
	return &FileStore{dir: dir}
}

func (s *FileStore) path(id string) string {
	return filepath.Join(s.dir, id+".json")
}

// List returns all stored Metadata sorted by ID.
func (s *FileStore) List(_ context.Context) ([]Metadata, error) {
	entries, err := os.ReadDir(s.dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read models dir %s: %w", s.dir, err)
	}
	var out []Metadata
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		id := strings.TrimSuffix(e.Name(), ".json")
		m, err := s.Get(context.Background(), id)
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

// Get returns the Metadata for id or ErrNotFound.
func (s *FileStore) Get(_ context.Context, id string) (Metadata, error) {
	data, err := os.ReadFile(s.path(id))
	if errors.Is(err, fs.ErrNotExist) {
		return Metadata{}, ErrNotFound
	}
	if err != nil {
		return Metadata{}, fmt.Errorf("read metadata %s: %w", id, err)
	}
	var m Metadata
	if err := json.Unmarshal(data, &m); err != nil {
		return Metadata{}, fmt.Errorf("decode metadata %s: %w", id, err)
	}
	return m, nil
}

// Put writes the metadata atomically (write to tmp + rename).
func (s *FileStore) Put(_ context.Context, m Metadata) error {
	if err := os.MkdirAll(s.dir, 0o755); err != nil {
		return fmt.Errorf("mkdir %s: %w", s.dir, err)
	}
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return fmt.Errorf("encode metadata %s: %w", m.ID, err)
	}
	final := s.path(m.ID)
	tmp := final + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("write tmp %s: %w", tmp, err)
	}
	if err := os.Rename(tmp, final); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("rename %s -> %s: %w", tmp, final, err)
	}
	return nil
}

// Delete removes the metadata file. Missing -> ErrNotFound.
func (s *FileStore) Delete(_ context.Context, id string) error {
	err := os.Remove(s.path(id))
	if errors.Is(err, fs.ErrNotExist) {
		return ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("remove metadata %s: %w", id, err)
	}
	return nil
}
