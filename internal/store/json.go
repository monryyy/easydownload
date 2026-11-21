// Package store persists download state as one JSON file per download.
package store

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"

	"EasyDownload/internal/model"
)

// Store reads and writes download state files.
type Store struct {
	dir string
}

// New creates the state directory and returns a Store.
func New(dir string) (*Store, error) {
	if dir == "" {
		return nil, errors.New("state directory is empty")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	return &Store{dir: dir}, nil
}

func (s *Store) path(id string) string {
	return filepath.Join(s.dir, id+".json")
}

// Save writes a download atomically.
func (s *Store) Save(d *model.Download) error {
	data, err := json.MarshalIndent(d, "", "  ")
	if err != nil {
		return err
	}
	// A unique temp name avoids collisions between the progress ticker and
	// status changes saving the same download at the same time.
	tmp, err := os.CreateTemp(s.dir, d.ID+"-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, s.path(d.ID))
}

// LoadAll reads every valid state file in the directory.
func (s *Store) LoadAll() ([]*model.Download, error) {
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		return nil, err
	}
	var out []*model.Download
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		data, err := os.ReadFile(filepath.Join(s.dir, entry.Name()))
		if err != nil {
			continue
		}
		var d model.Download
		if err := json.Unmarshal(data, &d); err != nil || d.ID == "" {
			continue
		}
		out = append(out, &d)
	}
	return out, nil
}

// Delete removes a download's state file.
func (s *Store) Delete(id string) error {
	err := os.Remove(s.path(id))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}
