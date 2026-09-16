// Package jsonstore stores JSON documents on disk, one folder per key.
//
// It works like a tiny, file-based git for JSON: every Save call records a
// new "version" of a key. Instead of writing a full copy of the document
// every time (wasteful for large, slowly-changing documents), only the
// first version and periodic "snapshot" versions store the complete JSON.
// Every version in between stores a compact unified-diff patch against the
// previous version — the same idea as git storing deltas between blobs.
//
// Reconstructing any historical version means starting at the nearest
// snapshot and replaying patches forward, exactly like checking out an old
// commit. The latest version's full content is also cached inline in
// meta.json so normal reads stay O(1) regardless of how much history
// exists.
//
// On disk, a store rooted at "data" with keys "users/ada" and "config"
// looks like:
//
//	data/
//	  users/
//	    ada/
//	      meta.json        version list + timestamps + messages + latest content
//	      versions/
//	        1.snap          full snapshot (always version 1)
//	        2.diff          patch: v1 -> v2
//	        3.diff          patch: v2 -> v3
//	        ...
//	  config/
//	    meta.json
//	    versions/
//	      1.snap
//
// meta.json is written via a temp-file-then-rename, and is the *only* file
// that records which versions officially exist and what the latest content
// is — both committed together in that one atomic write. A crash can leave
// an unreferenced version file behind (harmless; the next Save for that key
// overwrites it), but it can never leave meta.json pointing at a version
// whose content wasn't fully written, and it can never leave the "latest"
// content out of sync with the version list, because they live in the same
// file and are replaced together in a single rename.
package jsonstore

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// ErrNotFound is returned when a requested key or version does not exist.
var ErrNotFound = errors.New("jsonstore: not found")

// ErrNoChange is returned by Save/SaveWithMessage when the new value is
// identical (after canonicalization) to the current latest version, so
// nothing new was written. The version number returned alongside it is the
// current (unchanged) latest version.
var ErrNoChange = errors.New("jsonstore: no change")

const (
	metaFileName = "meta.json"
	versionsDir  = "versions"
)

// Store is a JSON document store rooted at a directory on disk.
//
// A Store's zero value is not usable directly for the Root field being
// empty; use New to construct one. It is safe to share a single *Store
// across goroutines, and safe to share the same Root directory across
// separate processes: writes are protected by a per-key lock file.
type Store struct {
	// Root is the directory that contains one subdirectory per key.
	Root string

	// SnapshotInterval controls how many versions may pass between full
	// snapshots. Smaller values mean faster reconstruction of old
	// versions but more disk usage; larger values save disk space but
	// make LoadVersion replay more patches. Defaults to 20 if <= 0.
	SnapshotInterval int

	// Context is the number of unchanged lines kept around each change
	// in a stored diff, the same idea as the "-U" flag to `diff`.
	// Defaults to 3 if <= 0.
	Context int
}

// New creates the root directory (if needed) and returns a Store using
// default settings. Set SnapshotInterval/Context on the returned Store
// before saving anything if you want to change the defaults.
func New(root string) (*Store, error) {
	if root == "" {
		return nil, errors.New("jsonstore: root must not be empty")
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		return nil, fmt.Errorf("jsonstore: creating root: %w", err)
	}
	return &Store{Root: root}, nil
}

func (s *Store) snapshotInterval() int {
	if s.SnapshotInterval <= 0 {
		return 20
	}
	return s.SnapshotInterval
}

func (s *Store) context() int {
	if s.Context <= 0 {
		return 3
	}
	return s.Context
}

// keyDir maps a key to its folder under Root, rejecting anything that
// would escape Root (like "..").
func (s *Store) keyDir(key string) (string, error) {
	if key == "" {
		return "", errors.New("jsonstore: key must not be empty")
	}
	clean := filepath.Clean(filepath.FromSlash(key))
	if filepath.IsAbs(clean) {
		return "", fmt.Errorf("jsonstore: invalid key %q: must be relative", key)
	}
	for _, part := range strings.Split(clean, string(filepath.Separator)) {
		if part == ".." || part == "." {
			return "", fmt.Errorf("jsonstore: invalid key %q", key)
		}
	}
	return filepath.Join(s.Root, clean), nil
}

// VersionMeta describes one saved version of a key.
type VersionMeta struct {
	Version  int       `json:"version"`
	Time     time.Time `json:"time"`
	Snapshot bool      `json:"snapshot"` // true if stored as a full copy rather than a diff
	Message  string    `json:"message,omitempty"`
}

type meta struct {
	Key      string        `json:"key"`
	Versions []VersionMeta `json:"versions"`
	// Latest holds the current HEAD content, committed atomically with
	// Versions. It's stored as an escaped JSON string rather than nested
	// raw JSON on purpose: embedding it as a raw sub-object would let
	// json.MarshalIndent silently reindent it to match its nesting
	// depth, corrupting the exact byte content the diff engine relies
	// on. A string field round-trips through Marshal/Unmarshal exactly.
	Latest string `json:"latest,omitempty"`
}

func readMeta(dir string) (*meta, error) {
	b, err := os.ReadFile(filepath.Join(dir, metaFileName))
	if errors.Is(err, os.ErrNotExist) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	var m meta
	if err := json.Unmarshal(b, &m); err != nil {
		return nil, fmt.Errorf("jsonstore: corrupt %s: %w", metaFileName, err)
	}
	return &m, nil
}

func writeMeta(dir string, m *meta) error {
	b, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	return writeFileAtomic(filepath.Join(dir, metaFileName), b)
}

// writeFileAtomic writes data to path via a temp file + rename, so readers
// never observe a partially-written file.
func writeFileAtomic(path string, data []byte) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".tmp-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return err
	}
	if err := os.Rename(tmpName, path); err != nil {
		os.Remove(tmpName)
		return err
	}
	return nil
}

// canonicalize turns v into deterministic, pretty-printed JSON text ending
// in a single newline, so that identical documents always diff as "no
// change" and diffs are stable line-by-line.
func canonicalize(v interface{}) ([]byte, error) {
	switch t := v.(type) {
	case json.RawMessage:
		return reindent(t)
	case []byte:
		return reindent(t)
	default:
		b, err := json.MarshalIndent(v, "", "  ")
		if err != nil {
			return nil, err
		}
		return append(b, '\n'), nil
	}
}

// reindent re-marshals raw JSON bytes through a decoder that preserves
// number formatting (via json.Number) so large integers or exact decimals
// aren't corrupted by a float64 round-trip.
func reindent(raw []byte) ([]byte, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var v interface{}
	if err := dec.Decode(&v); err != nil {
		return nil, fmt.Errorf("jsonstore: invalid JSON: %w", err)
	}
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(b, '\n'), nil
}

// afterVersionFileWritten is called during saveRaw right after the version
// file (snapshot or diff) is written but before meta.json is committed.
// It exists purely so tests can simulate a crash at that exact point; it
// is a no-op in normal use and has no effect on behavior or performance.
var afterVersionFileWritten = func() {}

// Save marshals v as JSON and stores it as a new version of key, creating
// the key's folder on first use. It returns the new version number.
//
// If v canonicalizes to exactly the same JSON as the current latest
// version, Save writes nothing and returns the current version number
// together with ErrNoChange (check with errors.Is).
func (s *Store) Save(key string, v interface{}) (int, error) {
	return s.SaveWithMessage(key, v, "")
}

// SaveWithMessage is like Save but attaches a free-form message to the
// version (visible in History), similar to a git commit message.
func (s *Store) SaveWithMessage(key string, v interface{}, message string) (int, error) {
	content, err := canonicalize(v)
	if err != nil {
		return 0, err
	}
	return s.saveRaw(key, content, message)
}

func (s *Store) saveRaw(key string, content []byte, message string) (int, error) {
	dir, err := s.keyDir(key)
	if err != nil {
		return 0, err
	}
	unlock, err := lockDir(dir)
	if err != nil {
		return 0, err
	}
	defer unlock()

	if err := os.MkdirAll(filepath.Join(dir, versionsDir), 0o755); err != nil {
		return 0, err
	}

	m, err := readMeta(dir)
	if errors.Is(err, ErrNotFound) {
		m = &meta{Key: key}
	} else if err != nil {
		return 0, err
	}

	var prevContent []byte
	if len(m.Versions) > 0 {
		prevContent = []byte(m.Latest)
		if bytes.Equal(prevContent, content) {
			return m.Versions[len(m.Versions)-1].Version, ErrNoChange
		}
	}

	nextVersion := len(m.Versions) + 1
	lastSnapshotVersion := 0
	for i := len(m.Versions) - 1; i >= 0; i-- {
		if m.Versions[i].Snapshot {
			lastSnapshotVersion = m.Versions[i].Version
			break
		}
	}
	isSnapshot := nextVersion == 1 || nextVersion-lastSnapshotVersion >= s.snapshotInterval()

	var fileName string
	var fileContent []byte
	if isSnapshot {
		fileName = fmt.Sprintf("%d.snap", nextVersion)
		fileContent = content
	} else {
		fileName = fmt.Sprintf("%d.diff", nextVersion)
		fileContent = []byte(unifiedDiff(string(prevContent), string(content), s.context()))
	}
	if err := writeFileAtomic(filepath.Join(dir, versionsDir, fileName), fileContent); err != nil {
		return 0, err
	}
	afterVersionFileWritten() // test hook only; a no-op in normal use

	// Everything below is staged in memory and committed with a single
	// atomic rename of meta.json. That's the crash-safety guarantee: a
	// reader (or the next Save call) only ever sees the version list and
	// the latest content in lockstep, never one updated without the
	// other. If the process dies before this point, meta.json is
	// untouched and the version file written just above becomes a
	// harmless orphan that the next Save for this key will overwrite.
	m.Versions = append(m.Versions, VersionMeta{
		Version:  nextVersion,
		Time:     time.Now().UTC(),
		Snapshot: isSnapshot,
		Message:  message,
	})
	m.Latest = string(content)
	if err := writeMeta(dir, m); err != nil {
		return 0, err
	}
	return nextVersion, nil
}

// Load reads the latest version of key into out (a pointer), like
// json.Unmarshal.
func (s *Store) Load(key string, out interface{}) error {
	raw, err := s.LoadRaw(key)
	if err != nil {
		return err
	}
	return json.Unmarshal(raw, out)
}

// LoadRaw returns the latest raw JSON stored for key.
func (s *Store) LoadRaw(key string) ([]byte, error) {
	dir, err := s.keyDir(key)
	if err != nil {
		return nil, err
	}
	m, err := readMeta(dir)
	if err != nil {
		return nil, err
	}
	if len(m.Versions) == 0 || m.Latest == "" {
		return nil, ErrNotFound
	}
	return []byte(m.Latest), nil
}

// LoadVersion returns the raw JSON exactly as it was at the given version
// number, reconstructing it from the nearest snapshot plus any diffs
// needed to reach it.
func (s *Store) LoadVersion(key string, version int) ([]byte, error) {
	dir, err := s.keyDir(key)
	if err != nil {
		return nil, err
	}
	m, err := readMeta(dir)
	if err != nil {
		return nil, err
	}
	idx := -1
	for i, vm := range m.Versions {
		if vm.Version == version {
			idx = i
			break
		}
	}
	if idx == -1 {
		return nil, ErrNotFound
	}
	return s.reconstruct(dir, m, idx)
}

// LoadVersionInto is LoadVersion followed by json.Unmarshal into out.
func (s *Store) LoadVersionInto(key string, version int, out interface{}) error {
	raw, err := s.LoadVersion(key, version)
	if err != nil {
		return err
	}
	return json.Unmarshal(raw, out)
}

func (s *Store) reconstruct(dir string, m *meta, idx int) ([]byte, error) {
	snapIdx := idx
	for !m.Versions[snapIdx].Snapshot {
		snapIdx--
		if snapIdx < 0 {
			return nil, fmt.Errorf("jsonstore: no snapshot found for %q", m.Key)
		}
	}
	raw, err := os.ReadFile(filepath.Join(dir, versionsDir, fmt.Sprintf("%d.snap", m.Versions[snapIdx].Version)))
	if err != nil {
		return nil, err
	}
	content := string(raw)
	for i := snapIdx + 1; i <= idx; i++ {
		ver := m.Versions[i].Version
		patchBytes, err := os.ReadFile(filepath.Join(dir, versionsDir, fmt.Sprintf("%d.diff", ver)))
		if err != nil {
			return nil, err
		}
		content, err = applyUnifiedDiff(content, string(patchBytes))
		if err != nil {
			return nil, fmt.Errorf("jsonstore: applying diff for v%d: %w", ver, err)
		}
	}
	return []byte(content), nil
}

// History returns metadata for every saved version of key, oldest first.
func (s *Store) History(key string) ([]VersionMeta, error) {
	dir, err := s.keyDir(key)
	if err != nil {
		return nil, err
	}
	m, err := readMeta(dir)
	if err != nil {
		return nil, err
	}
	return m.Versions, nil
}

// Diff returns unified-diff text showing the change between two versions
// of key, similar to running `git diff v1 v2`.
func (s *Store) Diff(key string, v1, v2 int) (string, error) {
	a, err := s.LoadVersion(key, v1)
	if err != nil {
		return "", fmt.Errorf("jsonstore: loading v%d: %w", v1, err)
	}
	b, err := s.LoadVersion(key, v2)
	if err != nil {
		return "", fmt.Errorf("jsonstore: loading v%d: %w", v2, err)
	}
	return unifiedDiff(string(a), string(b), s.context()), nil
}

// Exists reports whether key has at least one saved version.
func (s *Store) Exists(key string) bool {
	dir, err := s.keyDir(key)
	if err != nil {
		return false
	}
	_, err = os.Stat(filepath.Join(dir, metaFileName))
	return err == nil
}

// Keys lists every key currently in the store, in lexical order,
// including nested keys like "users/ada".
func (s *Store) Keys() ([]string, error) {
	if _, err := os.Stat(s.Root); errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	var keys []string
	err := filepath.WalkDir(s.Root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || d.Name() != metaFileName {
			return nil
		}
		rel, err := filepath.Rel(s.Root, filepath.Dir(path))
		if err != nil {
			return err
		}
		keys = append(keys, filepath.ToSlash(rel))
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(keys)
	return keys, nil
}

// Delete removes a key and its entire history.
func (s *Store) Delete(key string) error {
	dir, err := s.keyDir(key)
	if err != nil {
		return err
	}
	return os.RemoveAll(dir)
}
