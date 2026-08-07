package credentials

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

const credentialsFile = "credentials.json"

// FileStore keeps credentials in a 0600 JSON file. Every write replaces the
// file atomically, so a crash or a full disk leaves the previous version
// intact rather than a truncated one.
type FileStore struct {
	dir string
}

func NewFileStore(dir string) *FileStore {
	return &FileStore{dir: dir}
}

func (s *FileStore) Name() string { return KindFile }

func (s *FileStore) path() string { return filepath.Join(s.dir, credentialsFile) }

type fileContents struct {
	Contexts map[string]*Credential `json:"contexts"`
}

func (s *FileStore) load() (*fileContents, error) {
	data, err := os.ReadFile(s.path())
	if err != nil {
		if os.IsNotExist(err) {
			return &fileContents{Contexts: map[string]*Credential{}}, nil
		}
		return nil, fmt.Errorf("read %s: %w", s.path(), err)
	}

	var out fileContents
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, fmt.Errorf("parse %s: %w", s.path(), err)
	}
	if out.Contexts == nil {
		out.Contexts = map[string]*Credential{}
	}
	return &out, nil
}

func (s *FileStore) Get(context string) (*Credential, error) {
	contents, err := s.load()
	if err != nil {
		return nil, err
	}
	return contents.Contexts[context].clone(), nil
}

// Set replaces one context's credential. It re-reads the file first so that
// two processes refreshing different contexts do not clobber each other; the
// caller must hold WithLock for that read-modify-write to be safe.
func (s *FileStore) Set(context string, cred *Credential) error {
	if context == "" {
		return fmt.Errorf("context name is required")
	}
	contents, err := s.load()
	if err != nil {
		return err
	}
	if cred.IsZero() {
		delete(contents.Contexts, context)
	} else {
		contents.Contexts[context] = cred.clone()
	}
	return s.save(contents)
}

func (s *FileStore) Delete(context string) error {
	contents, err := s.load()
	if err != nil {
		return err
	}
	if _, ok := contents.Contexts[context]; !ok {
		return nil
	}
	delete(contents.Contexts, context)
	return s.save(contents)
}

func (s *FileStore) save(contents *fileContents) error {
	if err := os.MkdirAll(s.dir, 0700); err != nil {
		return fmt.Errorf("create credential directory %s: %w", s.dir, err)
	}
	data, err := json.MarshalIndent(contents, "", "  ")
	if err != nil {
		return err
	}
	return WriteAtomic(s.path(), data)
}

// Writable reports whether save would succeed, by round-tripping a probe file
// through the same directory. It never touches stored credentials.
func (s *FileStore) Writable() error {
	if err := os.MkdirAll(s.dir, 0700); err != nil {
		return fmt.Errorf("create credential directory %s: %w", s.dir, err)
	}
	probe, err := os.CreateTemp(s.dir, ".writable-*.probe")
	if err != nil {
		return fmt.Errorf("credential directory %s is not writable: %w", s.dir, err)
	}
	name := probe.Name()
	defer func() { _ = os.Remove(name) }()
	if err := probe.Close(); err != nil {
		return fmt.Errorf("credential directory %s is not writable: %w", s.dir, err)
	}
	return nil
}

// WriteAtomic replaces path with data via a same-directory temp file and a
// rename, fsyncing both the file and the directory so the replacement survives
// a crash. Rename is only atomic within a filesystem, hence the same directory.
// The result is always 0600 — every file this package writes holds secrets.
func WriteAtomic(path string, data []byte) error {
	dir := filepath.Dir(path)
	f, err := os.CreateTemp(dir, "."+filepath.Base(path)+"-*.tmp")
	if err != nil {
		return fmt.Errorf("create temp file in %s: %w", dir, err)
	}
	tmp := f.Name()
	defer func() { _ = os.Remove(tmp) }()

	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		return fmt.Errorf("write %s: %w", tmp, err)
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		return fmt.Errorf("sync %s: %w", tmp, err)
	}
	if err := f.Chmod(0600); err != nil {
		_ = f.Close()
		return fmt.Errorf("chmod %s: %w", tmp, err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("close %s: %w", tmp, err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("rename %s to %s: %w", tmp, path, err)
	}
	return fsyncDir(dir)
}

func fsyncDir(dir string) error {
	if runtime.GOOS == "windows" {
		return nil
	}
	f, err := os.Open(dir)
	if err != nil {
		return fmt.Errorf("open %s: %w", dir, err)
	}
	defer f.Close()
	if err := f.Sync(); err != nil {
		return fmt.Errorf("sync %s: %w", dir, err)
	}
	return nil
}
