package config

import (
	"slices"
	"sync"
)

// Settings owns the settings file within one process: it reads the file once, writes every change
// whole, and announces each change to whoever waits on Changes. Every part takes its settings from it.
type Settings struct {
	path    string
	mu      sync.Mutex
	cfg     Config
	changed chan struct{} // closed and replaced on every change
}

// Open reads the settings at path, once.
func Open(path string) (*Settings, error) {
	c, err := Load(path)
	if err != nil {
		return nil, err
	}
	return New(path, c), nil
}

// New owns the settings at path starting from c, without reading the file: for one that could not be read.
func New(path string, c Config) *Settings {
	return &Settings{path: path, cfg: c, changed: make(chan struct{})}
}

// Path is the settings file.
func (s *Settings) Path() string { return s.path }

// Get is the settings now.
func (s *Settings) Get() Config {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.cfg
}

// Changes is closed at the next change.
func (s *Settings) Changes() <-chan struct{} {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.changed
}

// Update applies change to a copy of the settings, saves it, and announces it; when change or the save
// fails, nothing changes.
func (s *Settings) Update(change func(*Config) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	next := s.cfg
	next.Off = slices.Clone(s.cfg.Off)
	next.Folders = slices.Clone(s.cfg.Folders)
	if err := change(&next); err != nil {
		return err
	}
	if err := Save(s.path, next); err != nil {
		return err
	}
	s.cfg = next
	close(s.changed)
	s.changed = make(chan struct{})
	return nil
}
