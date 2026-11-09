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

// Version 2 records trust per repository identity (kind + name), not per commit.
// Files written by older versions are treated as an empty store.
const Version = 2

type Record struct {
	Kind      string    `json:"kind"`
	Name      string    `json:"name"`
	URL       string    `json:"url,omitempty"`
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
	lock, err := flock.AcquireShared(store.lockPath())
	if err != nil {
		return nil, err
	}
	defer lock.Release()
	if _, err := store.load(); err != nil {
		return nil, err
	}
	return store, nil
}

func (s *Store) Verify(kind, name, url string) (bool, error) {
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
	record, ok := data.Records[keyOf(kind, name)]
	if !ok {
		return false, nil
	}
	if url != "" && record.URL != url {
		return false, nil
	}
	return true, nil
}

func (s *Store) Trust(kind, name, url string) error {
	if err := validateKey(kind, name); err != nil {
		return err
	}
	return s.update(func(data *file) {
		data.Records[keyOf(kind, name)] = Record{
			Kind: kind, Name: name, URL: url, TrustedAt: time.Now().UTC(),
		}
	})
}

func (s *Store) Revoke(kind, name string) error {
	if err := validateKey(kind, name); err != nil {
		return err
	}
	return s.update(func(data *file) {
		delete(data.Records, keyOf(kind, name))
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
	var header struct {
		Version int `json:"version"`
	}
	if err := json.Unmarshal(raw, &header); err != nil {
		return nil, fmt.Errorf("trust store %s is corrupt: %w", s.path, err)
	}
	if header.Version != Version {
		return data, nil
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
	if data.Records == nil {
		data.Records = map[string]Record{}
	}
	for key, record := range data.Records {
		if record.Kind == "" || record.Name == "" {
			return nil, fmt.Errorf("trust store %s has an incomplete record %q", s.path, key)
		}
		if key != keyOf(record.Kind, record.Name) {
			return nil, fmt.Errorf("trust store %s has a mismatched record %q", s.path, key)
		}
	}
	return data, nil
}

func (s *Store) save(data *file) error {
	if data.Records == nil {
		data.Records = map[string]Record{}
	}
	data.Version = Version
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

func keyOf(kind, name string) string {
	return kind + ":" + name
}
