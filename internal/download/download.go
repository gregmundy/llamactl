package download

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/gregmundy/llamactl/internal/hf"
	"golang.org/x/sys/unix"
)

// ErrInProgress signals that another caller is currently downloading the
// same dest path. Callers can errors.Is against it.
var ErrInProgress = errors.New("download in progress")

// Ranger is the network seam — satisfied by *hf.Client. Declared locally so
// internal/download does not depend on the broader cli.HFClient interface.
type Ranger interface {
	FetchRange(ctx context.Context, repoID, file string, offset, end int64, w io.Writer) error
}

// Request is one download job.
//
// WasAlreadyPresent is an out-parameter: Downloader.Get sets it to true
// when the dedupe fast-path fires (DestPath already exists with matching
// SHA-256). Callers consult this to differentiate "downloaded" from
// "already had it" in user-facing output without doing a second hash pass.
type Request struct {
	RepoID         string
	File           string    // .gguf filename on HF
	DestPath       string    // final on-disk path
	ExpectedSHA256 string    // hex
	TotalSize      int64     // for progress; 0 disables
	Progress       *Progress // optional

	WasAlreadyPresent bool // OUT: set by Get when dedupe fast-path fires
}

// Downloader orchestrates a single Get: lock -> resume -> stream -> verify -> rename.
//
// Stderr is the destination for human-facing one-line notes (e.g. flock
// contention). nil means os.Stderr. Tests inject a *bytes.Buffer.
type Downloader struct {
	Ranger Ranger
	Stderr io.Writer
}

// Get fetches DestPath from RepoID/File, resuming a .partial if present,
// verifying SHA256, and atomically renaming on success.
//
// If DestPath already exists and its on-disk SHA matches ExpectedSHA256,
// returns nil immediately and sets req.WasAlreadyPresent = true
// (dedupe fast path — PRD AC#7). Pointer receiver on req so callers
// can observe that signal.
func (d *Downloader) Get(ctx context.Context, req *Request) error {
	if existing, err := verifyExisting(req.DestPath, req.ExpectedSHA256); err == nil && existing {
		req.WasAlreadyPresent = true
		return nil
	}

	partial := req.DestPath + ".partial"
	if err := os.MkdirAll(filepath.Dir(req.DestPath), 0o755); err != nil {
		return fmt.Errorf("mkdir dest: %w", err)
	}
	f, err := os.OpenFile(partial, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return fmt.Errorf("open partial: %w", err)
	}
	closed := false
	defer func() {
		if !closed {
			f.Close()
		}
	}()

	stderr := d.Stderr
	if stderr == nil {
		stderr = os.Stderr
	}
	if err := unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		if errors.Is(err, unix.EWOULDBLOCK) {
			fmt.Fprintf(stderr, "another llamactl instance is downloading %s; waiting…\n", req.RepoID)
			if err := unix.Flock(int(f.Fd()), unix.LOCK_EX); err != nil {
				return fmt.Errorf("flock partial: %w", err)
			}
		} else {
			return fmt.Errorf("flock partial: %w", err)
		}
	}

	// Another process may have just finished while we waited on the lock.
	if existing, err := verifyExisting(req.DestPath, req.ExpectedSHA256); err == nil && existing {
		req.WasAlreadyPresent = true
		return nil
	}

	fi, err := f.Stat()
	if err != nil {
		return fmt.Errorf("stat partial: %w", err)
	}
	resumeOffset := fi.Size()

	h := sha256.New()
	if resumeOffset > 0 {
		if _, err := f.Seek(0, io.SeekStart); err != nil {
			return fmt.Errorf("seek partial: %w", err)
		}
		if _, err := io.CopyN(h, f, resumeOffset); err != nil {
			return fmt.Errorf("re-hash partial: %w", err)
		}
		if _, err := f.Seek(0, io.SeekEnd); err != nil {
			return fmt.Errorf("seek to end: %w", err)
		}
	}

	if req.Progress != nil {
		req.Progress.Initial = resumeOffset
	}

	writers := []io.Writer{f, h}
	if req.Progress != nil {
		writers = append(writers, req.Progress)
	}
	mw := io.MultiWriter(writers...)

	err = d.Ranger.FetchRange(ctx, req.RepoID, req.File, resumeOffset, 0, mw)
	if errors.Is(err, hf.ErrRangeNotSupported) {
		if err := f.Truncate(0); err != nil {
			return fmt.Errorf("truncate for restart: %w", err)
		}
		if _, err := f.Seek(0, io.SeekStart); err != nil {
			return err
		}
		h.Reset()
		if req.Progress != nil {
			req.Progress.Initial = 0
		}
		err = d.Ranger.FetchRange(ctx, req.RepoID, req.File, 0, 0, io.MultiWriter(f, h))
	}
	if err != nil {
		return fmt.Errorf("fetch range: %w", err)
	}

	if err := f.Sync(); err != nil {
		return fmt.Errorf("fsync partial: %w", err)
	}

	got := hex.EncodeToString(h.Sum(nil))
	if got != req.ExpectedSHA256 {
		_ = os.Remove(partial)
		return fmt.Errorf("sha256 mismatch: got %s, want %s", got, req.ExpectedSHA256)
	}

	// Rename while the flock is still held so that a concurrent waiter's
	// post-lock verifyExisting check sees DestPath before we release.
	if err := os.Rename(partial, req.DestPath); err != nil {
		return fmt.Errorf("rename %s -> %s: %w", partial, req.DestPath, err)
	}

	if err := f.Close(); err != nil {
		// fd was renamed away; treat close errors as non-fatal.
		_ = err
	}
	closed = true
	return nil
}

// verifyExisting returns (true, nil) if path exists and its sha256 == expected.
func verifyExisting(path, expectedHex string) (bool, error) {
	f, err := os.Open(path)
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return false, err
	}
	got := hex.EncodeToString(h.Sum(nil))
	return got == expectedHex, nil
}
