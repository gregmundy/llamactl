package cli

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/gregmundy/llamactl/internal/download"
	"github.com/gregmundy/llamactl/internal/hardware"
	"github.com/gregmundy/llamactl/internal/hf"
	"github.com/gregmundy/llamactl/internal/launchd"
	"github.com/gregmundy/llamactl/internal/models"
)

// fakeHFClient is a controllable HFClient for tests.
type fakeHFClient struct {
	SearchHits map[string][]hf.SearchHit
	Repos      map[string]hf.Repo
	Bytes      map[string][]byte // key = repoID + "/" + file
	FetchCalls int
	// RepoInfoDelay is an optional sleep applied to every RepoInfo call.
	// Used to verify parallelism in `fit` — N hits × delay should wall-time
	// at roughly delay when the loop is concurrent, vs N×delay when serial.
	RepoInfoDelay time.Duration
}

func (f *fakeHFClient) Search(ctx context.Context, q string) ([]hf.SearchHit, error) {
	return f.SearchHits[q], nil
}
func (f *fakeHFClient) SearchRefresh(ctx context.Context, q string) ([]hf.SearchHit, error) {
	return f.SearchHits[q], nil
}
func (f *fakeHFClient) RepoInfo(ctx context.Context, repoID string) (hf.Repo, error) {
	if f.RepoInfoDelay > 0 {
		select {
		case <-time.After(f.RepoInfoDelay):
		case <-ctx.Done():
			return hf.Repo{}, ctx.Err()
		}
	}
	r, ok := f.Repos[repoID]
	if !ok {
		return hf.Repo{}, errors.New("404")
	}
	return r, nil
}
func (f *fakeHFClient) FetchRange(ctx context.Context, repoID, file string, off, end int64, w io.Writer) error {
	f.FetchCalls++
	b, ok := f.Bytes[repoID+"/"+file]
	if !ok {
		return errors.New("404")
	}
	if end == 0 {
		end = int64(len(b))
	}
	_, err := w.Write(b[off:end])
	return err
}

// fakeDownloader writes the requested body directly (via fakeHFClient), so we
// can assert "Downloader.Get was called" without exercising the real flock
// machinery. The real Downloader is covered via httptest in T16.
//
// If the destination already exists with matching SHA, mimic the real
// Downloader by setting req.WasAlreadyPresent and NOT recording the call as
// a network fetch — so tests can distinguish "deduped" from "downloaded".
type fakeDownloader struct {
	HFClient *fakeHFClient
	Calls    []download.Request
}

func (f *fakeDownloader) Get(ctx context.Context, req *download.Request) error {
	// Mimic real dedupe: if dest exists with matching SHA, signal and return.
	if existing, _ := sha256OfFile(req.DestPath); existing != "" && existing == req.ExpectedSHA256 {
		req.WasAlreadyPresent = true
		return nil
	}
	f.Calls = append(f.Calls, *req)
	if f.HFClient == nil {
		return nil
	}
	body, ok := f.HFClient.Bytes[req.RepoID+"/"+req.File]
	if !ok {
		return errors.New("fakeDownloader: no body for " + req.RepoID + "/" + req.File)
	}
	if err := os.MkdirAll(filepath.Dir(req.DestPath), 0o755); err != nil {
		return err
	}
	return os.WriteFile(req.DestPath, body, 0o644)
}

// sha256OfFile is a small helper used by fakeDownloader to detect when the
// destination is already present with the expected hash — mirroring the
// real Downloader.Get dedupe behavior.
func sha256OfFile(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// fakeHardwareDetector returns a pinned Info.
type fakeHardwareDetector struct{ Info hardware.Info }

func (f fakeHardwareDetector) Detect(ctx context.Context) (hardware.Info, error) {
	return f.Info, nil
}

// fakeNow gives deterministic AddedAt timestamps.
func fakeNow() time.Time { return time.Date(2026, 5, 10, 12, 0, 0, 0, time.UTC) }

// fakeModelStore is an in-memory ModelStore.
type fakeModelStore struct {
	M map[string]models.Metadata
}

func newFakeModelStore() *fakeModelStore { return &fakeModelStore{M: map[string]models.Metadata{}} }

func (s *fakeModelStore) List(_ context.Context) ([]models.Metadata, error) {
	out := make([]models.Metadata, 0, len(s.M))
	for _, m := range s.M {
		out = append(out, m)
	}
	return out, nil
}
func (s *fakeModelStore) Get(_ context.Context, id string) (models.Metadata, error) {
	m, ok := s.M[id]
	if !ok {
		return models.Metadata{}, models.ErrNotFound
	}
	return m, nil
}
func (s *fakeModelStore) Put(_ context.Context, m models.Metadata) error {
	s.M[m.ID] = m
	return nil
}
func (s *fakeModelStore) Delete(_ context.Context, id string) error {
	if _, ok := s.M[id]; !ok {
		return models.ErrNotFound
	}
	delete(s.M, id)
	return nil
}

// --- Phase 3 fakes ---

type fakeLaunchdService struct {
	Loaded     []string                       // plist paths passed to Load
	Booted     []string                       // labels passed to Bootout
	Services   map[string]launchd.ServiceInfo // by label
	ListResult []launchd.ServiceInfo
	LoadErr    error
	BootoutErr error
}

func (f *fakeLaunchdService) Load(_ context.Context, plistPath string) error {
	f.Loaded = append(f.Loaded, plistPath)
	return f.LoadErr
}
func (f *fakeLaunchdService) Bootout(_ context.Context, label string) error {
	f.Booted = append(f.Booted, label)
	return f.BootoutErr
}
func (f *fakeLaunchdService) Print(_ context.Context, label string) (launchd.ServiceInfo, error) {
	if f.Services == nil {
		return launchd.ServiceInfo{Label: label}, nil
	}
	info, ok := f.Services[label]
	if !ok {
		return launchd.ServiceInfo{Label: label}, nil
	}
	return info, nil
}
func (f *fakeLaunchdService) List(_ context.Context) ([]launchd.ServiceInfo, error) {
	return f.ListResult, nil
}

type fakePortAllocator struct {
	Allocated []int
	Skipped   [][]int     // skip slice captured per call (for assertions)
	Returns   map[int]int // preferred → returned
}

func (f *fakePortAllocator) Free(preferred int, skip []int) (int, error) {
	out := preferred
	if v, ok := f.Returns[preferred]; ok {
		out = v
	}
	f.Allocated = append(f.Allocated, out)
	// Copy skip so later mutations by the caller can't change our record.
	cp := append([]int(nil), skip...)
	f.Skipped = append(f.Skipped, cp)
	return out, nil
}

type fakeProcInspector struct {
	RSSByPID    map[int]int64
	UptimeByPID map[int]time.Duration
}

func (f *fakeProcInspector) RSS(pid int) (int64, error) {
	if v, ok := f.RSSByPID[pid]; ok {
		return v, nil
	}
	return 0, nil
}
func (f *fakeProcInspector) Uptime(pid int) (time.Duration, error) {
	if v, ok := f.UptimeByPID[pid]; ok {
		return v, nil
	}
	return 0, nil
}

type fakeTokRateReader struct {
	RateByPath map[string]float64
}

func (f *fakeTokRateReader) Rate(logPath string, _ time.Duration, _ time.Time) (float64, error) {
	return f.RateByPath[logPath], nil
}
