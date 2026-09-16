package jsonstore

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"
)

const (
	lockFileName   = ".lock"
	lockStaleAfter = 30 * time.Second // fallback if we can't check the owning PID directly
	lockRetryDelay = 25 * time.Millisecond
	lockTimeout    = 5 * time.Second
)

// lockDir acquires an exclusive, cross-process lock on dir (creating dir if
// needed) and returns a function that releases it. It works the same way
// git's own lockfiles do (e.g. index.lock): create the lock file with
// O_EXCL so only one writer can succeed, and remove it when done.
//
// If a process dies (crashes, is killed, panics past its deferred cleanup)
// while holding the lock, the file is left behind. The next caller detects
// this by checking whether the PID recorded in the lock file is still
// alive and breaks the lock immediately if not — so a crash never leaves
// a key permanently stuck. If liveness can't be determined (e.g. on a
// platform where checking is unsupported), a lock older than
// lockStaleAfter is broken as a fallback.
func lockDir(dir string) (func(), error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	lockPath := filepath.Join(dir, lockFileName)
	deadline := time.Now().Add(lockTimeout)
	for {
		f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
		if err == nil {
			fmt.Fprintf(f, "%d\n", os.Getpid())
			f.Close()
			return func() { os.Remove(lockPath) }, nil
		}
		if !os.IsExist(err) {
			return nil, err
		}
		if lockIsStale(lockPath) {
			os.Remove(lockPath) // break a lock left by a dead or crashed process
			continue
		}
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("jsonstore: timed out waiting for lock on %s", dir)
		}
		time.Sleep(lockRetryDelay)
	}
}

func lockIsStale(lockPath string) bool {
	info, err := os.Stat(lockPath)
	if err != nil {
		return false // someone else may have just removed it; let the retry loop re-check
	}
	if pid, err := lockOwnerPID(lockPath); err == nil {
		if alive, known := processAlive(pid); known {
			return !alive
		}
	}
	return time.Since(info.ModTime()) > lockStaleAfter
}

func lockOwnerPID(lockPath string) (int, error) {
	b, err := os.ReadFile(lockPath)
	if err != nil {
		return 0, err
	}
	return strconv.Atoi(strings.TrimSpace(string(b)))
}

// processAlive reports whether pid is currently running. known is false
// when liveness couldn't be determined on this platform, in which case the
// caller should fall back to time-based staleness instead of trusting the
// (meaningless) alive value.
func processAlive(pid int) (alive bool, known bool) {
	if pid <= 0 {
		return false, true
	}
	proc, err := os.FindProcess(pid)
	if err != nil {
		return false, true
	}
	// Signal 0 is the standard POSIX trick: it performs the existence
	// check without actually delivering a signal. Windows doesn't
	// support arbitrary signals through os.Process.Signal, so treat
	// that as "can't tell" rather than misreporting.
	err = proc.Signal(syscall.Signal(0))
	if err == nil {
		return true, true
	}
	if errors.Is(err, os.ErrProcessDone) {
		return false, true
	}
	if runtime.GOOS == "windows" {
		return false, false
	}
	return false, true
}
