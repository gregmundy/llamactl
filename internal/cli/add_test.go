package cli

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gregmundy/llamactl/internal/gguftest"
	"github.com/gregmundy/llamactl/internal/hardware"
	"github.com/gregmundy/llamactl/internal/hf"
	"github.com/gregmundy/llamactl/internal/models"
)

func makeDeps(t *testing.T) (*Deps, *fakeHFClient, *fakeDownloader, *fakeModelStore, string) {
	t.Helper()
	shared := t.TempDir()
	configDir := t.TempDir()
	body := []byte("fake gguf bytes for testing")
	sum := sha256.Sum256(body)
	shaHex := hex.EncodeToString(sum[:])

	hfc := &fakeHFClient{
		Repos: map[string]hf.Repo{
			"Qwen/Qwen2.5-7B-Instruct-GGUF": {
				ID: "Qwen/Qwen2.5-7B-Instruct-GGUF",
				Siblings: []hf.File{
					{RFilename: "qwen2.5-7b-instruct-q4_k_m.gguf", LFS: &hf.LFSInfo{SHA256: shaHex, Size: int64(len(body))}},
				},
			},
		},
		Bytes: map[string][]byte{
			"Qwen/Qwen2.5-7B-Instruct-GGUF/qwen2.5-7b-instruct-q4_k_m.gguf": body,
		},
	}
	dl := &fakeDownloader{HFClient: hfc}
	store := newFakeModelStore()

	d := &Deps{
		HardwareDetector: fakeHardwareDetector{Info: hardware.Info{RAMBytes: 16 * (1 << 30)}},
		HardwareJSONPath: filepath.Join(configDir, "hardware.json"),
		HFClient:         hfc,
		Downloader:       dl,
		QuantSelector:    SelectorAdapter{},
		ModelStore:       store,
		FS:               OSFileSystem{},
		ModelsConfigDir:  filepath.Join(configDir, "models"),
		SharedModelsDir:  shared,
		Now:              fakeNow,
	}
	return d, hfc, dl, store, shared
}

// runRoot signature is (t, deps, args...) -> (stdout, stderr, err). It
// overwrites deps.Stdout/Stderr internally, so tests don't pre-set them.

func TestAddHappyPath(t *testing.T) {
	d, _, dl, store, shared := makeDeps(t)
	if _, _, err := runRoot(t, d, "add", "qwen2.5-7b-instruct"); err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(dl.Calls) != 1 {
		t.Errorf("Downloader.Get call count = %d, want 1", len(dl.Calls))
	}
	if _, ok := store.M["qwen2.5-7b-instruct"]; !ok {
		t.Errorf("Metadata not persisted")
	}
	want := filepath.Join(shared, "qwen2.5-7b-instruct", "Q4_K_M.gguf")
	if dl.Calls[0].DestPath != want {
		t.Errorf("DestPath = %q, want %q", dl.Calls[0].DestPath, want)
	}
}

func TestAddDedupesIfFileAlreadyPresent(t *testing.T) {
	d, hfc, dl, _, shared := makeDeps(t)
	body := hfc.Bytes["Qwen/Qwen2.5-7B-Instruct-GGUF/qwen2.5-7b-instruct-q4_k_m.gguf"]
	dest := filepath.Join(shared, "qwen2.5-7b-instruct", "Q4_K_M.gguf")
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dest, body, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := runRoot(t, d, "add", "qwen2.5-7b-instruct"); err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(dl.Calls) != 0 {
		t.Errorf("Downloader should not be called when SHA matches; got %d calls", len(dl.Calls))
	}
}

// Dedupe path must print "already present" and NOT also print "installed".
// Before the fix, finishAdd printed both lines on the dedupe branch, which
// misled users into thinking a re-download had occurred.
func TestAddDedupeDoesNotPrintInstalled(t *testing.T) {
	d, hfc, _, _, shared := makeDeps(t)
	body := hfc.Bytes["Qwen/Qwen2.5-7B-Instruct-GGUF/qwen2.5-7b-instruct-q4_k_m.gguf"]
	dest := filepath.Join(shared, "qwen2.5-7b-instruct", "Q4_K_M.gguf")
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dest, body, 0o644); err != nil {
		t.Fatal(err)
	}
	stdout, _, err := runRoot(t, d, "add", "qwen2.5-7b-instruct")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if !strings.Contains(stdout, "already present") {
		t.Errorf("stdout should contain 'already present'; got: %s", stdout)
	}
	if strings.Contains(stdout, "installed ") {
		t.Errorf("stdout should NOT contain 'installed ' on dedupe path; got: %s", stdout)
	}
}

func TestAddUnknownModelErrors(t *testing.T) {
	d, _, _, _, _ := makeDeps(t)
	_, _, err := runRoot(t, d, "add", "nope")
	if err == nil || !strings.Contains(err.Error(), "available") {
		t.Fatalf("err = %v", err)
	}
}

func TestAddQuantOverride(t *testing.T) {
	d, hfc, dl, _, _ := makeDeps(t)
	body := []byte("alt bytes")
	sum := sha256.Sum256(body)
	hfc.Repos["Qwen/Qwen2.5-7B-Instruct-GGUF"] = hf.Repo{
		ID: "Qwen/Qwen2.5-7B-Instruct-GGUF",
		Siblings: []hf.File{
			{RFilename: "qwen2.5-7b-instruct-q4_k_m.gguf", LFS: &hf.LFSInfo{SHA256: "0", Size: 1}},
			{RFilename: "qwen2.5-7b-instruct-q5_k_m.gguf", LFS: &hf.LFSInfo{SHA256: hex.EncodeToString(sum[:]), Size: int64(len(body))}},
		},
	}
	hfc.Bytes["Qwen/Qwen2.5-7B-Instruct-GGUF/qwen2.5-7b-instruct-q5_k_m.gguf"] = body
	if _, _, err := runRoot(t, d, "add", "qwen2.5-7b-instruct", "--quant", "Q5_K_M"); err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(dl.Calls) != 1 || !strings.Contains(dl.Calls[0].File, "q5_k_m") {
		t.Errorf("expected Q5_K_M file; got calls=%+v", dl.Calls)
	}
}

func TestAddNoQuantFitsErrors(t *testing.T) {
	d, _, _, _, _ := makeDeps(t)
	d.HardwareDetector = fakeHardwareDetector{Info: hardware.Info{RAMBytes: 8 * (1 << 30)}}
	_, _, err := runRoot(t, d, "add", "llama3.3-70b")
	if err == nil || !strings.Contains(strings.ToLower(err.Error()), "quant") {
		t.Fatalf("err = %v", err)
	}
}

func TestAddBootstrapsHardwareJSON(t *testing.T) {
	d, _, _, _, _ := makeDeps(t)
	if _, err := os.Stat(d.HardwareJSONPath); err == nil {
		t.Fatal("precondition: hardware.json should not exist yet")
	}
	if _, _, err := runRoot(t, d, "add", "qwen2.5-7b-instruct"); err != nil {
		t.Fatalf("err: %v", err)
	}
	if _, err := os.Stat(d.HardwareJSONPath); err != nil {
		t.Errorf("hardware.json should be auto-written; err=%v", err)
	}
}

var _ = models.Q4_K_M // touch the package

// mustGGUFBody builds a synthetic GGUF body for HF-path tests, delegating
// to the shared internal/gguftest package.
func mustGGUFBody(t *testing.T, arch string, paramsCount uint64) []byte {
	t.Helper()
	var kvs []gguftest.KV
	if arch != "" {
		kvs = append(kvs, gguftest.KV{Key: "general.architecture", Type: gguftest.TypeString, Value: arch})
	}
	if paramsCount > 0 {
		kvs = append(kvs, gguftest.KV{Key: "general.parameter_count", Type: gguftest.TypeU64, Value: paramsCount})
	}
	return gguftest.Build(t, 3, kvs...)
}

func makeHFPathDeps(t *testing.T, body []byte) (*Deps, *fakeHFClient, *fakeDownloader, *fakeModelStore, string) {
	t.Helper()
	shared := t.TempDir()
	configDir := t.TempDir()
	sum := sha256.Sum256(body)
	shaHex := hex.EncodeToString(sum[:])

	hfc := &fakeHFClient{
		Repos: map[string]hf.Repo{
			"Qwen/Qwen3-8B-Instruct-GGUF": {
				ID: "Qwen/Qwen3-8B-Instruct-GGUF",
				Siblings: []hf.File{
					{RFilename: "qwen3-8b-instruct-q4_k_m.gguf", LFS: &hf.LFSInfo{SHA256: shaHex, Size: int64(len(body))}},
					{RFilename: "qwen3-8b-instruct-q5_k_m.gguf", LFS: &hf.LFSInfo{SHA256: "00", Size: 999}},
				},
			},
		},
		Bytes: map[string][]byte{
			"Qwen/Qwen3-8B-Instruct-GGUF/qwen3-8b-instruct-q4_k_m.gguf": body,
		},
	}
	dl := &fakeDownloader{HFClient: hfc}
	store := newFakeModelStore()
	d := &Deps{
		HardwareDetector: fakeHardwareDetector{Info: hardware.Info{RAMBytes: 16 * (1 << 30)}},
		HardwareJSONPath: filepath.Join(configDir, "hardware.json"),
		HFClient:         hfc,
		Downloader:       dl,
		QuantSelector:    SelectorAdapter{},
		ModelStore:       store,
		FS:               OSFileSystem{},
		ModelsConfigDir:  filepath.Join(configDir, "models"),
		SharedModelsDir:  shared,
		Now:              fakeNow,
	}
	return d, hfc, dl, store, shared
}

func TestAddHFPath_RequiresQuant(t *testing.T) {
	body := mustGGUFBody(t, "qwen3", 8030000000)
	d, _, _, _, _ := makeHFPathDeps(t, body)
	_, _, err := runRoot(t, d, "add", "Qwen/Qwen3-8B-Instruct-GGUF")
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "requires --quant") {
		t.Errorf("err should mention 'requires --quant'; got: %v", err)
	}
	if !strings.Contains(err.Error(), "Q4_K_M") {
		t.Errorf("err should list available quants; got: %v", err)
	}
}

func TestAddHFPath_HappyPath(t *testing.T) {
	body := mustGGUFBody(t, "qwen3", 8030000000)
	d, _, dl, store, shared := makeHFPathDeps(t, body)
	_, _, err := runRoot(t, d, "add", "Qwen/Qwen3-8B-Instruct-GGUF", "--quant", "Q4_K_M")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(dl.Calls) != 1 {
		t.Errorf("Downloader.Get call count = %d, want 1", len(dl.Calls))
	}
	got, ok := store.M["qwen3-8b-instruct"]
	if !ok {
		t.Fatalf("metadata not persisted under derived id; have: %v", keys(store.M))
	}
	if got.ParamsB != 8 {
		t.Errorf("ParamsB = %d, want 8", got.ParamsB)
	}
	if string(got.Arch) != "qwen3" {
		t.Errorf("Arch = %q, want qwen3", got.Arch)
	}
	if got.Quant != models.Q4_K_M {
		t.Errorf("Quant = %q, want Q4_K_M", got.Quant)
	}
	want := filepath.Join(shared, "qwen3-8b-instruct", "Q4_K_M.gguf")
	if got.GGUFPath != want {
		t.Errorf("GGUFPath = %q, want %q", got.GGUFPath, want)
	}
}

func TestAddHFPath_DerivedIDStripsGGUFSuffix(t *testing.T) {
	body := mustGGUFBody(t, "qwen3", 8030000000)
	d, _, _, store, _ := makeHFPathDeps(t, body)
	_, _, err := runRoot(t, d, "add", "Qwen/Qwen3-8B-Instruct-GGUF", "--quant", "Q4_K_M")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if _, ok := store.M["qwen3-8b-instruct"]; !ok {
		t.Errorf("expected derived id 'qwen3-8b-instruct'; have: %v", keys(store.M))
	}
}

func TestAddHFPath_HeaderReadFailureWarnsAndProceeds(t *testing.T) {
	body := []byte("NOTAGGUFFILE...")
	d, _, _, store, _ := makeHFPathDeps(t, body)
	stdout, stderr, err := runRoot(t, d, "add", "Qwen/Qwen3-8B-Instruct-GGUF", "--quant", "Q4_K_M")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	got, ok := store.M["qwen3-8b-instruct"]
	if !ok {
		t.Fatalf("metadata not persisted")
	}
	if got.ParamsB != 0 || got.Arch != "" {
		t.Errorf("expected empty ParamsB/Arch on header failure, got %+v", got)
	}
	if !strings.Contains(stderr, "warning") {
		t.Errorf("stderr should contain 'warning'; got: %s", stderr)
	}
	_ = stdout
}

func TestAddHFPath_CollisionWithPreferredID(t *testing.T) {
	body := mustGGUFBody(t, "qwen2", 7615616512)
	sum := sha256.Sum256(body)
	shaHex := hex.EncodeToString(sum[:])

	shared := t.TempDir()
	configDir := t.TempDir()
	hfc := &fakeHFClient{
		Repos: map[string]hf.Repo{
			"Qwen/Qwen2.5-7B-Instruct-GGUF": {
				ID: "Qwen/Qwen2.5-7B-Instruct-GGUF",
				Siblings: []hf.File{
					{RFilename: "qwen2.5-7b-instruct-q4_k_m.gguf", LFS: &hf.LFSInfo{SHA256: shaHex, Size: int64(len(body))}},
				},
			},
		},
		Bytes: map[string][]byte{
			"Qwen/Qwen2.5-7B-Instruct-GGUF/qwen2.5-7b-instruct-q4_k_m.gguf": body,
		},
	}
	store := newFakeModelStore()
	d := &Deps{
		HardwareDetector: fakeHardwareDetector{Info: hardware.Info{RAMBytes: 16 * (1 << 30)}},
		HardwareJSONPath: filepath.Join(configDir, "hardware.json"),
		HFClient:         hfc,
		Downloader:       &fakeDownloader{HFClient: hfc},
		QuantSelector:    SelectorAdapter{},
		ModelStore:       store,
		FS:               OSFileSystem{},
		ModelsConfigDir:  filepath.Join(configDir, "models"),
		SharedModelsDir:  shared,
		Now:              fakeNow,
	}
	_, _, err := runRoot(t, d, "add", "Qwen/Qwen2.5-7B-Instruct-GGUF", "--quant", "Q4_K_M")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if _, ok := store.M["qwen2.5-7b-instruct"]; !ok {
		t.Errorf("expected metadata under 'qwen2.5-7b-instruct' (collision with preferred id)")
	}
}

// keys returns the map keys for friendlier error messages.
func keys(m map[string]models.Metadata) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
