package jsonstore

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
)

type user struct {
	Name string   `json:"name"`
	Age  int      `json:"age"`
	Tags []string `json:"tags,omitempty"`
}

func newTestStore(t *testing.T) *Store {
	t.Helper()
	s, err := New(t.TempDir())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return s
}

func TestSaveLoadRoundTrip(t *testing.T) {
	s := newTestStore(t)
	u := user{Name: "Ada", Age: 30}

	v, err := s.Save("users/ada", u)
	if err != nil {
		t.Fatalf("Save: %v", err)
	}
	if v != 1 {
		t.Fatalf("expected version 1, got %d", v)
	}

	var got user
	if err := s.Load("users/ada", &got); err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !reflect.DeepEqual(u, got) {
		t.Fatalf("roundtrip mismatch: got %+v want %+v", got, u)
	}

	// The key should show up as a folder on disk.
	if _, err := os.Stat(filepath.Join(s.Root, "users", "ada", "meta.json")); err != nil {
		t.Fatalf("expected key folder with meta.json: %v", err)
	}
}

func TestVersionsSnapshotsAndDiff(t *testing.T) {
	s := newTestStore(t)
	s.SnapshotInterval = 3

	versions := []user{
		{Name: "Ada", Age: 30},
		{Name: "Ada", Age: 31},
		{Name: "Ada", Age: 31, Tags: []string{"admin"}},
		{Name: "Ada", Age: 32, Tags: []string{"admin"}},
		{Name: "Ada", Age: 32, Tags: []string{"admin", "vip"}},
	}
	for i, u := range versions {
		v, err := s.SaveWithMessage("users/ada", u, "update")
		if err != nil {
			t.Fatalf("save %d: %v", i, err)
		}
		if v != i+1 {
			t.Fatalf("expected version %d, got %d", i+1, v)
		}
	}

	hist, err := s.History("users/ada")
	if err != nil {
		t.Fatalf("History: %v", err)
	}
	if len(hist) != len(versions) {
		t.Fatalf("expected %d versions, got %d", len(versions), len(hist))
	}

	// With SnapshotInterval=3: v1 is always a snapshot, and the next
	// snapshot lands once we're >=3 versions past the last one (v4).
	wantSnap := map[int]bool{1: true, 4: true}
	for _, vm := range hist {
		if vm.Snapshot != wantSnap[vm.Version] {
			t.Errorf("version %d: snapshot=%v, want %v", vm.Version, vm.Snapshot, wantSnap[vm.Version])
		}
		if vm.Message != "update" {
			t.Errorf("version %d: message=%q, want %q", vm.Version, vm.Message, "update")
		}
	}

	// Every historical version must reconstruct exactly, via snapshot+diffs.
	for i, want := range versions {
		raw, err := s.LoadVersion("users/ada", i+1)
		if err != nil {
			t.Fatalf("LoadVersion %d: %v", i+1, err)
		}
		var got user
		if err := json.Unmarshal(raw, &got); err != nil {
			t.Fatalf("unmarshal version %d: %v", i+1, err)
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("version %d mismatch: got %+v want %+v", i+1, got, want)
		}
	}

	diff, err := s.Diff("users/ada", 1, 2)
	if err != nil {
		t.Fatalf("Diff: %v", err)
	}
	if !strings.Contains(diff, `-  "age": 30`) {
		t.Errorf("diff missing removed age line:\n%s", diff)
	}
	if !strings.Contains(diff, `+  "age": 31`) {
		t.Errorf("diff missing added age line:\n%s", diff)
	}

	// The .diff files on disk should be much smaller than a full snapshot
	// would be, proving we're actually storing deltas, not copies.
	diffPath := filepath.Join(s.Root, "users", "ada", "versions", "2.diff")
	diffBytes, err := os.ReadFile(diffPath)
	if err != nil {
		t.Fatalf("reading 2.diff: %v", err)
	}
	snapPath := filepath.Join(s.Root, "users", "ada", "versions", "1.snap")
	snapBytes, err := os.ReadFile(snapPath)
	if err != nil {
		t.Fatalf("reading 1.snap: %v", err)
	}
	t.Logf("snapshot=%d bytes, diff=%d bytes", len(snapBytes), len(diffBytes))
}

func TestNoChangeReturnsErrNoChange(t *testing.T) {
	s := newTestStore(t)
	u := user{Name: "Ada", Age: 30}

	if _, err := s.Save("users/ada", u); err != nil {
		t.Fatalf("Save: %v", err)
	}
	v, err := s.Save("users/ada", u)
	if !errors.Is(err, ErrNoChange) {
		t.Fatalf("expected ErrNoChange, got %v", err)
	}
	if v != 1 {
		t.Fatalf("expected version to stay at 1, got %d", v)
	}

	hist, err := s.History("users/ada")
	if err != nil {
		t.Fatalf("History: %v", err)
	}
	if len(hist) != 1 {
		t.Fatalf("expected no new version to be recorded, got %d versions", len(hist))
	}
}

func TestKeysAndDelete(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.Save("a", user{Name: "A"}); err != nil {
		t.Fatalf("Save a: %v", err)
	}
	if _, err := s.Save("nested/b", user{Name: "B"}); err != nil {
		t.Fatalf("Save nested/b: %v", err)
	}

	keys, err := s.Keys()
	if err != nil {
		t.Fatalf("Keys: %v", err)
	}
	want := []string{"a", "nested/b"}
	if !reflect.DeepEqual(keys, want) {
		t.Fatalf("Keys = %v, want %v", keys, want)
	}

	if !s.Exists("a") {
		t.Error("Exists(a) = false, want true")
	}
	if s.Exists("nope") {
		t.Error("Exists(nope) = true, want false")
	}

	if err := s.Delete("a"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := s.LoadRaw("a"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound after delete, got %v", err)
	}
	if _, err := os.Stat(filepath.Join(s.Root, "a")); !os.IsNotExist(err) {
		t.Fatal("expected key folder to be removed from disk")
	}
}

func TestInvalidKeysRejected(t *testing.T) {
	s := newTestStore(t)
	for _, key := range []string{"", "..", "../escape", "a/../../b", "/absolute"} {
		if _, err := s.Save(key, user{Name: "x"}); err == nil {
			t.Errorf("Save(%q) succeeded, want error", key)
		}
	}
}

func TestConcurrentSavesToSameKey(t *testing.T) {
	s := newTestStore(t)
	const n = 20

	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			if _, err := s.Save("counter", map[string]int{"n": i}); err != nil {
				t.Errorf("save %d: %v", i, err)
			}
		}(i)
	}
	wg.Wait()

	hist, err := s.History("counter")
	if err != nil {
		t.Fatalf("History: %v", err)
	}
	if len(hist) != n {
		t.Fatalf("expected %d versions from %d concurrent saves, got %d", n, n, len(hist))
	}
	for i, vm := range hist {
		if vm.Version != i+1 {
			t.Fatalf("version numbers not sequential: %+v", hist)
		}
	}
	if _, err := s.LoadVersion("counter", hist[len(hist)-1].Version); err != nil {
		t.Fatalf("LoadVersion latest: %v", err)
	}
	if _, err := s.LoadVersion("counter", 1); err != nil {
		t.Fatalf("LoadVersion 1: %v", err)
	}
}

func TestLoadVersionAndDiffUnknownVersion(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.Save("k", user{Name: "x"}); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if _, err := s.LoadVersion("k", 99); !errors.Is(err, ErrNotFound) {
		t.Fatalf("LoadVersion(99) err = %v, want ErrNotFound", err)
	}
	if _, err := s.Diff("k", 1, 99); err == nil {
		t.Fatal("Diff against unknown version should error")
	}
}

func TestUnknownKeyErrors(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.LoadRaw("nope"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("LoadRaw err = %v, want ErrNotFound", err)
	}
	if _, err := s.History("nope"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("History err = %v, want ErrNotFound", err)
	}
}

// TestCrashDuringSaveDoesNotCorruptHistory forks a real subprocess that
// saves version 1 normally, then starts saving version 2 and is killed
// (os.Exit, which skips deferred cleanup — including the lock release —
// same as a real crash) right after the version-2 patch file is written
// but before meta.json is committed. The parent process then checks that:
//  1. version 1 is completely untouched and still reconstructs correctly,
//  2. the orphaned version-2 file left behind doesn't corrupt anything,
//  3. the stale lock left by the killed process doesn't wedge the key, and
//  4. the store is immediately usable again for new saves.
func TestCrashDuringSaveDoesNotCorruptHistory(t *testing.T) {
	if os.Getenv("JSONSTORE_CRASH_HELPER") == "1" {
		runCrashHelper()
		return // unreachable; runCrashHelper always calls os.Exit
	}

	dir := t.TempDir()
	cmd := exec.Command(os.Args[0], "-test.run=^TestCrashDuringSaveDoesNotCorruptHistory$")
	cmd.Env = append(os.Environ(),
		"JSONSTORE_CRASH_HELPER=1",
		"JSONSTORE_CRASH_DIR="+dir,
	)
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("expected the helper subprocess to crash (os.Exit(9)), but it exited cleanly.\noutput:\n%s", out)
	}
	t.Logf("helper subprocess output (expected to crash):\n%s", out)

	// Confirm the crashed process really did leave a version-2 patch file
	// orphaned on disk with no corresponding meta.json entry - that's the
	// exact situation we're testing recovery from, not an artifact of a
	// bug in the test itself.
	orphan := filepath.Join(dir, "doc", "versions", "2.diff")
	if _, statErr := os.Stat(orphan); statErr != nil {
		t.Fatalf("expected an orphaned 2.diff from the killed process, got: %v", statErr)
	}

	s, err := New(dir)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	hist, err := s.History("doc")
	if err != nil {
		t.Fatalf("History: %v", err)
	}
	if len(hist) != 1 {
		t.Fatalf("expected exactly 1 surviving version after the crash, got %d: %+v", len(hist), hist)
	}

	var got map[string]int
	if err := s.Load("doc", &got); err != nil {
		t.Fatalf("Load after crash: %v", err)
	}
	if got["n"] != 1 {
		t.Fatalf("Load after crash = %v, want {n:1} (v1 must be untouched)", got)
	}
	raw, err := s.LoadVersion("doc", 1)
	if err != nil {
		t.Fatalf("LoadVersion(1) after crash: %v", err)
	}
	var gotV1 map[string]int
	if err := json.Unmarshal(raw, &gotV1); err != nil || gotV1["n"] != 1 {
		t.Fatalf("LoadVersion(1) after crash = %v, err=%v, want {n:1}", gotV1, err)
	}

	// The killed process never ran its deferred unlock, so its stale lock
	// file should still be sitting there...
	if _, statErr := os.Stat(filepath.Join(dir, "doc", lockFileName)); statErr != nil {
		t.Fatalf("expected a stale lock file left by the killed process, got: %v", statErr)
	}
	// ...but a new save must still succeed promptly (PID-liveness check
	// breaking the stale lock immediately, not the 30s timeout fallback).
	v, err := s.Save("doc", map[string]int{"n": 2})
	if err != nil {
		t.Fatalf("Save after crash (recovering from stale lock): %v", err)
	}
	if v != 2 {
		t.Fatalf("expected the recovery save to land as version 2, got %d", v)
	}
	var got2 map[string]int
	if err := s.Load("doc", &got2); err != nil {
		t.Fatalf("Load after recovery save: %v", err)
	}
	if got2["n"] != 2 {
		t.Fatalf("Load after recovery save = %v, want {n:2}", got2)
	}
}

// runCrashHelper runs inside the re-exec'd subprocess. It never returns.
func runCrashHelper() {
	dir := os.Getenv("JSONSTORE_CRASH_DIR")
	s, err := New(dir)
	if err != nil {
		fmt.Fprintln(os.Stderr, "helper: New:", err)
		os.Exit(1)
	}
	if _, err := s.Save("doc", map[string]int{"n": 1}); err != nil {
		fmt.Fprintln(os.Stderr, "helper: save v1:", err)
		os.Exit(1)
	}
	// Simulate a real crash exactly between "version file written" and
	// "meta.json committed" - os.Exit skips all deferred cleanup, just
	// like a SIGKILL or a hard process crash would.
	afterVersionFileWritten = func() {
		fmt.Fprintln(os.Stderr, "helper: simulating crash mid-save")
		os.Exit(9)
	}
	_, err = s.Save("doc", map[string]int{"n": 2})
	// Should never get here.
	fmt.Fprintln(os.Stderr, "helper: crash hook did not fire, save returned:", err)
	os.Exit(1)
}
