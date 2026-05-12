package download

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gregmundy/llamactl/internal/hf"
)

func mkBody(n int) []byte {
	b := make([]byte, n)
	for i := range b {
		b[i] = byte(i % 251)
	}
	return b
}

func sha256hex(b []byte) string {
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

// fakeRanger satisfies the Ranger interface for tests without a real HTTP server.
type fakeRanger struct {
	body    []byte
	noRange bool
}

func (f *fakeRanger) FetchRange(ctx context.Context, repo, file string, offset, end int64, w io.Writer) error {
	if offset > 0 && f.noRange {
		return hf.ErrRangeNotSupported
	}
	if end == 0 || end > int64(len(f.body)) {
		end = int64(len(f.body))
	}
	_, err := w.Write(f.body[offset:end])
	return err
}

func TestDownloadHappyPath(t *testing.T) {
	dir := t.TempDir()
	body := mkBody(1024)
	dl := &Downloader{Ranger: &fakeRanger{body: body}}
	req := Request{
		RepoID:         "x/y",
		File:           "f.gguf",
		DestPath:       filepath.Join(dir, "f.gguf"),
		ExpectedSHA256: sha256hex(body),
		TotalSize:      int64(len(body)),
	}
	if err := dl.Get(context.Background(), &req); err != nil {
		t.Fatalf("Get: %v", err)
	}
	got, err := os.ReadFile(req.DestPath)
	if err != nil || string(got) != string(body) {
		t.Errorf("dest mismatch (err=%v)", err)
	}
	if _, err := os.Stat(req.DestPath + ".partial"); !errors.Is(err, os.ErrNotExist) {
		t.Errorf(".partial should be cleaned up; got err=%v", err)
	}
}

// When DestPath already exists with matching SHA-256, Get must short-circuit
// and signal the caller via Request.WasAlreadyPresent — no network call.
func TestDownloadSignalsAlreadyPresent(t *testing.T) {
	dir := t.TempDir()
	body := mkBody(512)
	dest := filepath.Join(dir, "f.gguf")
	if err := os.WriteFile(dest, body, 0o644); err != nil {
		t.Fatal(err)
	}
	// Ranger that fails if called — proves we hit the fast path.
	ranger := &fakeRanger{body: nil}
	dl := &Downloader{Ranger: ranger}
	req := Request{
		RepoID: "x/y", File: "f.gguf",
		DestPath:       dest,
		ExpectedSHA256: sha256hex(body),
		TotalSize:      int64(len(body)),
	}
	if err := dl.Get(context.Background(), &req); err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !req.WasAlreadyPresent {
		t.Errorf("WasAlreadyPresent = false, want true on dedupe fast path")
	}
}

func TestDownloadResumeFromPartial(t *testing.T) {
	dir := t.TempDir()
	body := mkBody(1024)
	partial := filepath.Join(dir, "f.gguf.partial")
	if err := os.WriteFile(partial, body[:512], 0o644); err != nil {
		t.Fatal(err)
	}
	dl := &Downloader{Ranger: &fakeRanger{body: body}}
	req := Request{
		RepoID: "x/y", File: "f.gguf",
		DestPath:       filepath.Join(dir, "f.gguf"),
		ExpectedSHA256: sha256hex(body),
		TotalSize:      int64(len(body)),
	}
	if err := dl.Get(context.Background(), &req); err != nil {
		t.Fatalf("Get: %v", err)
	}
	got, _ := os.ReadFile(req.DestPath)
	if string(got) != string(body) {
		t.Errorf("resume produced wrong final content")
	}
}

func TestDownloadSHAMismatchUnlinksPartial(t *testing.T) {
	dir := t.TempDir()
	body := mkBody(64)
	dl := &Downloader{Ranger: &fakeRanger{body: body}}
	req := Request{
		RepoID: "x/y", File: "f.gguf",
		DestPath:       filepath.Join(dir, "f.gguf"),
		ExpectedSHA256: strings.Repeat("0", 64), // wrong
		TotalSize:      int64(len(body)),
	}
	err := dl.Get(context.Background(), &req)
	if err == nil {
		t.Fatal("expected SHA mismatch error")
	}
	if _, err := os.Stat(req.DestPath + ".partial"); !errors.Is(err, os.ErrNotExist) {
		t.Errorf(".partial should be unlinked after SHA mismatch")
	}
	if _, err := os.Stat(req.DestPath); err == nil {
		t.Errorf("dest should not exist after SHA mismatch")
	}
}

func TestDownloadNoRangeRestartsFromZero(t *testing.T) {
	dir := t.TempDir()
	body := mkBody(256)
	partial := filepath.Join(dir, "f.gguf.partial")
	_ = os.WriteFile(partial, body[:128], 0o644) // garbage from a previous run

	dl := &Downloader{Ranger: &fakeRanger{body: body, noRange: true}}
	req := Request{
		RepoID: "x/y", File: "f.gguf",
		DestPath:       filepath.Join(dir, "f.gguf"),
		ExpectedSHA256: sha256hex(body),
		TotalSize:      int64(len(body)),
	}
	if err := dl.Get(context.Background(), &req); err != nil {
		t.Fatalf("Get: %v", err)
	}
	got, _ := os.ReadFile(req.DestPath)
	if string(got) != string(body) {
		t.Errorf("after no-range restart, content mismatch")
	}
}

func TestDownloadFlockSerializesTwoCallers(t *testing.T) {
	dir := t.TempDir()
	body := mkBody(2048)
	gate := make(chan struct{})
	slow := &slowRanger{body: body, gate: gate}
	dl := &Downloader{Ranger: slow}
	req := Request{
		RepoID: "x/y", File: "f.gguf",
		DestPath:       filepath.Join(dir, "f.gguf"),
		ExpectedSHA256: sha256hex(body),
		TotalSize:      int64(len(body)),
	}

	var wg sync.WaitGroup
	wg.Add(2)
	errs := make([]error, 2)
	go func() { defer wg.Done(); errs[0] = dl.Get(context.Background(), &req) }()
	time.Sleep(50 * time.Millisecond)
	go func() { defer wg.Done(); errs[1] = dl.Get(context.Background(), &req) }()
	time.Sleep(50 * time.Millisecond)
	close(gate)
	wg.Wait()
	if errs[0] != nil {
		t.Errorf("caller 0: %v", errs[0])
	}
	if errs[1] != nil {
		t.Errorf("caller 1: %v", errs[1])
	}
	if got, _ := os.ReadFile(req.DestPath); string(got) != string(body) {
		t.Errorf("final file content mismatch")
	}
}

// TestDownloadFlockLogsOnContention asserts that the second caller — the one
// that has to wait on the EX flock — emits a single-line note to its Stderr
// identifying the repo, instead of blocking silently.
func TestDownloadFlockLogsOnContention(t *testing.T) {
	dir := t.TempDir()
	body := mkBody(2048)
	gate := make(chan struct{})
	slow := &slowRanger{body: body, gate: gate}

	var buf2 bytes.Buffer
	dl1 := &Downloader{Ranger: slow}
	dl2 := &Downloader{Ranger: slow, Stderr: &buf2}

	dest := filepath.Join(dir, "f.gguf")
	mkReq := func() Request {
		return Request{
			RepoID: "owner/repo", File: "f.gguf",
			DestPath:       dest,
			ExpectedSHA256: sha256hex(body),
			TotalSize:      int64(len(body)),
		}
	}

	var wg sync.WaitGroup
	wg.Add(2)
	errs := make([]error, 2)
	r0 := mkReq()
	go func() { defer wg.Done(); errs[0] = dl1.Get(context.Background(), &r0) }()
	// Give caller 0 time to acquire the flock and block on the gate.
	time.Sleep(50 * time.Millisecond)
	r1 := mkReq()
	go func() { defer wg.Done(); errs[1] = dl2.Get(context.Background(), &r1) }()
	// Give caller 1 time to hit the contended flock path.
	time.Sleep(50 * time.Millisecond)
	close(gate)
	wg.Wait()

	if errs[0] != nil {
		t.Errorf("caller 0: %v", errs[0])
	}
	if errs[1] != nil {
		t.Errorf("caller 1: %v", errs[1])
	}
	out := buf2.String()
	if !strings.Contains(out, "waiting") {
		t.Errorf("caller 1 Stderr did not mention waiting; got %q", out)
	}
	if !strings.Contains(out, "owner/repo") {
		t.Errorf("caller 1 Stderr did not mention RepoID; got %q", out)
	}
}

type slowRanger struct {
	body []byte
	gate chan struct{}
}

func (s *slowRanger) FetchRange(ctx context.Context, repo, file string, offset, end int64, w io.Writer) error {
	<-s.gate
	if end == 0 {
		end = int64(len(s.body))
	}
	_, err := w.Write(s.body[offset:end])
	return err
}

func TestErrInProgressIsWrapped(t *testing.T) {
	base := fmt.Errorf("%w: foo pending", ErrInProgress)
	if !errors.Is(base, ErrInProgress) {
		t.Fatal("errors.Is failed on wrapped sentinel")
	}
}

func TestDownloadOverHTTPTest(t *testing.T) {
	body := mkBody(2048)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		w.Write(body)
	}))
	t.Cleanup(ts.Close)
	c := hf.NewClient(ts.URL, hf.NewCache(t.TempDir()), nil)
	dl := &Downloader{Ranger: c}
	dir := t.TempDir()
	req := Request{
		RepoID: "x/y", File: "f.gguf",
		DestPath:       filepath.Join(dir, "f.gguf"),
		ExpectedSHA256: sha256hex(body),
		TotalSize:      int64(len(body)),
	}
	if err := dl.Get(context.Background(), &req); err != nil {
		t.Fatalf("Get: %v", err)
	}
	fi, err := os.Stat(req.DestPath)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Size() != int64(len(body)) {
		t.Errorf("got size %d, want %d", fi.Size(), len(body))
	}
}
