package truststore

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/spock2300/vmake/internal/flock"
	"github.com/spock2300/vmake/internal/jsonio"
)

const Version = 1

type Record struct {
	Kind      string    `json:"kind"`
	Name      string    `json:"name"`
	URL       string    `json:"url,omitempty"`
	Commit    string    `json:"commit"`
	TrustedAt time.Time `json:"trustedAt"`
}

type file struct {
	Version int               `json:"version"`
	Records map[string]Record `json:"records"`
}

type Store struct {
	path string
}

func Open(path string) (*Store, error) {
	store := &Store{path: path}
	if _, err := store.load(); err != nil {
		return nil, err
	}
	return store, nil
}

func (s *Store) Verify(kind, name, url, commit string) (bool, error) {
	if err := validateKey(kind, name); err != nil {
		return false, err
	}
	lock, err := flock.AcquireShared(s.lockPath())
	if err != nil {
		return false, err
	}
	defer lock.Release()
	data, err := s.load()
	if err != nil {
		return false, err
	}
	record, ok := data.Records[keyOf(kind, name, commit)]
	if !ok || record.Commit != commit {
		return false, nil
	}
	if url != "" && record.URL != "" && record.URL != url {
		return false, nil
	}
	return true, nil
}

func (s *Store) Trust(kind, name, url, commit string) error {
	if err := validateKey(kind, name); err != nil {
		return err
	}
	if err := validateCommit(commit); err != nil {
		return fmt.Errorf("cannot trust %s %q: %w", kind, name, err)
	}
	return s.update(func(data *file) {
		data.Records[keyOf(kind, name, commit)] = Record{
			Kind: kind, Name: name, URL: url, Commit: commit, TrustedAt: time.Now().UTC(),
		}
	})
}

func (s *Store) Revoke(kind, name string) error {
	if err := validateKey(kind, name); err != nil {
		return err
	}
	return s.update(func(data *file) {
		for key, record := range data.Records {
			if record.Kind == kind && record.Name == name {
				delete(data.Records, key)
			}
		}
	})
}

func (s *Store) update(mutate func(*file)) error {
	lock, err := flock.Acquire(s.lockPath())
	if err != nil {
		return err
	}
	defer lock.Release()
	data, err := s.load()
	if err != nil {
		return err
	}
	mutate(data)
	return s.save(data)
}

func (s *Store) load() (*file, error) {
	data := &file{Version: Version, Records: map[string]Record{}}
	raw, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return data, nil
		}
		return nil, fmt.Errorf("read trust store %s: %w", s.path, err)
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(data); err != nil {
		return nil, fmt.Errorf("trust store %s is corrupt: %w", s.path, err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return nil, fmt.Errorf("trust store %s has trailing data: %w", s.path, err)
	}
	if data.Version != Version {
		return nil, fmt.Errorf("trust store %s has unsupported version %d (want %d)", s.path, data.Version, Version)
	}
	if data.Records == nil {
		data.Records = map[string]Record{}
	}
	for key, record := range data.Records {
		if record.Kind == "" || record.Name == "" || record.Commit == "" {
			return nil, fmt.Errorf("trust store %s has an incomplete record %q", s.path, key)
		}
		if key != keyOf(record.Kind, record.Name, record.Commit) {
			return nil, fmt.Errorf("trust store %s has a mismatched record %q", s.path, key)
		}
	}
	return data, nil
}

func (s *Store) save(data *file) error {
	if data.Records == nil {
		data.Records = map[string]Record{}
	}
	return jsonio.Save(s.path, data)
}

func (s *Store) lockPath() string {
	return s.path + ".lock"
}

func validateKey(kind, name string) error {
	if kind == "" || name == "" {
		return fmt.Errorf("trust kind and name are required")
	}
	if strings.ContainsAny(kind, ":@") || strings.ContainsAny(name, ":@") {
		return fmt.Errorf("trust kind and name must not contain ':' or '@'")
	}
	return nil
}

func validateCommit(commit string) error {
	if commit == "" {
		return fmt.Errorf("a commit is required")
	}
	if strings.ContainsAny(commit, ":@") {
		return fmt.Errorf("invalid commit %q", commit)
	}
	return nil
}

func keyOf(kind, name, commit string) string {
	return kind + ":" + name + "@" + commit
}
