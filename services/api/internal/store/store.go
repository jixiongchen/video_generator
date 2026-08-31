package store

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"sync"

	"video-generator/services/api/internal/model"
)

var ErrNotFound = errors.New("generation not found")

type Store struct {
	mu    sync.RWMutex
	path  string
	items map[string]model.Generation
}

func New(dataDir string) (*Store, error) {
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return nil, err
	}
	s := &Store{
		path:  filepath.Join(dataDir, "generations.json"),
		items: make(map[string]model.Generation),
	}
	if err := s.load(); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *Store) load() error {
	b, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	var rows []model.Generation
	if err := json.Unmarshal(b, &rows); err != nil {
		return err
	}
	for _, row := range rows {
		s.items[row.ID] = row
	}
	return nil
}

func (s *Store) List() []model.Generation {
	s.mu.RLock()
	defer s.mu.RUnlock()
	rows := make([]model.Generation, 0, len(s.items))
	for _, item := range s.items {
		rows = append(rows, item)
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].CreatedAt.After(rows[j].CreatedAt) })
	return rows
}

func (s *Store) Get(id string) (model.Generation, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	item, ok := s.items[id]
	if !ok {
		return model.Generation{}, ErrNotFound
	}
	return item, nil
}

func (s *Store) Put(item model.Generation) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.items[item.ID] = item
	return s.flushLocked()
}

func (s *Store) flushLocked() error {
	rows := make([]model.Generation, 0, len(s.items))
	for _, item := range s.items {
		rows = append(rows, item)
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].CreatedAt.Before(rows[j].CreatedAt) })
	b, err := json.MarshalIndent(rows, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}
