# jsonstore

A small, dependency-free Go package that stores JSON documents on disk —
one folder per key — and keeps a git-like version history where only
**diffs** are written between periodic full snapshots, instead of a full
copy on every save.

```
data/
  users/
    ada/
      meta.json        # version list + latest content, committed together
      versions/
        1.snap          # full snapshot (always version 1)
        2.diff          # patch: v1 -> v2
        3.diff          # patch: v2 -> v3
        ...
```

Reconstructing any historical version starts at the nearest snapshot and
replays patches forward — the same idea as checking out an old git commit
from a snapshot + delta chain.

## Install

```
go get jsonstore   # or just copy the .go files into your module
```

(This was built and tested locally; there's no real published module path,
so `go get` won't resolve — copy the files into your own module instead.)

## Usage

```go
store, _ := jsonstore.New("./data")

type Profile struct {
    Name string   `json:"name"`
    Bio  string   `json:"bio"`
    Tags []string `json:"tags"`
}

p := Profile{Name: "Ada", Bio: "Mathematician", Tags: []string{"engineer"}}
store.Save("profiles/ada", p)             // -> version 1 (snapshot)

p.Bio = "Mathematician and writer"
store.Save("profiles/ada", p)             // -> version 2 (diff)

// git log
hist, _ := store.History("profiles/ada")

// git show v1
var old Profile
store.LoadVersionInto("profiles/ada", 1, &old)

// git diff v1 v2
diff, _ := store.Diff("profiles/ada", 1, 2)

// latest
var latest Profile
store.Load("profiles/ada", &latest)
```

See `example/main.go` for a full runnable walkthrough.

## API

| Method | Like... |
|---|---|
| `Save(key, v)` / `SaveWithMessage(key, v, msg)` | `git add && git commit` |
| `Load(key, &out)` / `LoadRaw(key)` | reading the working tree |
| `LoadVersion(key, n)` / `LoadVersionInto(key, n, &out)` | `git show <rev>:file` |
| `History(key)` | `git log` |
| `Diff(key, v1, v2)` | `git diff v1 v2` |
| `Keys()` | listing everything stored |
| `Exists(key)` / `Delete(key)` | — |

`Save` returns `(version int, err error)`. If the new value is identical to
what's already stored, it returns the current version and `ErrNoChange`
(check with `errors.Is`) instead of writing a new one — no-op commits are
skipped, same as git telling you "nothing to commit."

Keys can be nested paths, e.g. `"users/ada"` or `"orders/2026/09/123"` —
each becomes a nested folder. `..` and absolute paths are rejected.

## Configuration

```go
store.SnapshotInterval = 20 // default: write a full snapshot every 20 versions
store.Context = 3           // default: lines of context kept around each change
```

Lower `SnapshotInterval` = faster reconstruction of old versions, more disk
use. Higher = the opposite. `Context` behaves like `diff -U<n>`.

## How the diffing works

JSON is canonicalized to pretty-printed, deterministic text (stable key
order, no float round-off — large integers and precise decimals are
preserved via `json.Number`) and then diffed **line by line** using Myers'
shortest-edit-script algorithm — the same algorithm behind `diff` and
`git diff`. Only changed lines plus a little surrounding context are
written to the `.diff` file; long unchanged stretches are skipped entirely
and represented purely by the line numbers in the patch header
(`@@ -a,b +c,d @@`), which is what keeps the patches small.

For a 38 KB catalog document where one item's price changed, the resulting
diff is 150 bytes. Concretely:

```
@@ -1256,7 +1256,7 @@
     {
       "id": 250,
       "name": "widget-250",
-      "price": 350
+      "price": 9999
     },
     {
       "id": 251,
```

**Caveat:** this is a *line*-based diff, not a JSON-aware one. If a
document has a very long value on a single line (e.g. one giant string
field with no internal newlines) sitting right next to whatever changed,
that whole line gets pulled in as "context" and the diff won't be small.
This only matters for documents with huge single-line fields directly
adjacent to frequently-changed fields; typical pretty-printed JSON (objects
and arrays spread across many lines) compacts well, as shown above.

## Concurrency

A `*Store` is safe to share across goroutines. Writes to the same key are
also safe across **separate processes**: each save takes an OS-level
exclusive lock file (`.lock`, created with `O_EXCL`) inside the key's
folder while it writes, the same mechanism git uses for `index.lock`. If
the holding process dies (crash, kill, panic past its deferred cleanup),
the next process to try that key checks whether the recorded PID is still
alive and breaks the lock immediately if not — no need to wait out a
timeout. (Falls back to a 30-second age check if liveness can't be
determined on the current platform.)

## Crash safety

A crash mid-save never corrupts a version that was already committed. Two
things make that true:

- Every file is written via a temp-file-then-rename, so a reader (or the
  next `Save` call) only ever sees the old file or the fully-new one —
  never a half-written one.
- `meta.json` is the single source of truth for both "which versions
  exist" and "what the latest content is," and it's updated in one atomic
  rename. There's no window where the version list and the latest content
  can disagree, because they're the same file.

A crash can leave one harmless byproduct: an orphaned `N.diff`/`N.snap`
file for the version that was in progress. It isn't referenced by
`meta.json`, so it's invisible to every read path, and the next `Save`
for that key simply overwrites it. `TestCrashDuringSaveDoesNotCorruptHistory`
in the test suite verifies this for real — it forks an actual subprocess,
kills it (`os.Exit`, skipping deferred cleanup) at the exact instant
between writing a version file and committing `meta.json`, and checks that
the parent process sees fully intact history and can keep writing
immediately afterward.

## Running tests

```
go test -race ./...
```
