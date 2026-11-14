package main

import "github.com/spock2300/vmake/internal/storage"

// commandSession owns the ambient command inputs that used to be read from
// package globals by openProject/commandStorageLocks: set overrides, lock
// flags, the config selector and the storage lifecycle.
type commandSession struct {
	setFlags       []string
	locked         bool
	offline        bool
	configSelector string
	exclusive      bool
	storage        *storage.Session
}

var activeCommandSession *commandSession

func newCommandSession() *commandSession {
	return &commandSession{
		setFlags:       ephemeralSetFlags,
		locked:         lockedFlag,
		offline:        offlineFlag,
		configSelector: configFlag,
	}
}

func currentCommandSession() *commandSession {
	if activeCommandSession == nil {
		activeCommandSession = newCommandSession()
	}
	return activeCommandSession
}

func (s *commandSession) storageLocks() *storage.Session {
	if s.storage != nil {
		return s.storage
	}
	session, err := storage.Acquire(findProjectDir(), getCacheDir(), s.exclusive)
	fatalErr(err)
	s.storage = session
	return s.storage
}

func (s *commandSession) close() error {
	if s == nil || s.storage == nil {
		return nil
	}
	err := s.storage.Close()
	s.storage = nil
	return err
}

func commandStorageLocks() *storage.Session {
	return currentCommandSession().storageLocks()
}
