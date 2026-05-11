package cli

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/gregmundy/llamactl/internal/download"
	"github.com/gregmundy/llamactl/internal/hardware"
	"github.com/gregmundy/llamactl/internal/hf"
	"github.com/gregmundy/llamactl/internal/models"
)

// fakeHFClient is a controllable HFClient for tests.
type fakeHFClient struct {
	SearchHits map[string][]hf.SearchHit
	Repos      map[string]hf.Repo
	Bytes      map[string][]byte // key = repoID + "/" + file
	FetchCalls int
}

func (f *fakeHFClient) Search(ctx context.Context, q string) ([]hf.SearchHit, error) {
	return f.SearchHits[q], nil
}
func (f *fakeHFClient) SearchRefresh(ctx context.Context, q string) ([]hf.SearchHit, error) {
	return f.SearchHits[q], nil
}
func (f *fakeHFClient) RepoInfo(ctx context.Context, repoID string) (hf.Repo, error) {
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
type fakeDownloader struct {
	HFClient *fakeHFClient
	Calls    []download.Request
}

func (f *fakeDownloader) Get(ctx context.Context, req download.Request) error {
	f.Calls = append(f.Calls, req)
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
