package cli

import (
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/gregmundy/llamactl/internal/launchd"
	"github.com/gregmundy/llamactl/internal/models"
	"github.com/spf13/cobra"
)

func TestCompleteInstalledModelsListsStoreEntries(t *testing.T) {
	store := newFakeModelStore()
	_ = store.Put(context.Background(), models.Metadata{ID: "qwen2.5-3b-instruct"})
	_ = store.Put(context.Background(), models.Metadata{ID: "gemma-4-e4b-it"})
	d := &Deps{ModelStore: store}

	got, dir := completeInstalledModels(d)(&cobra.Command{}, nil, "")
	if dir != cobra.ShellCompDirectiveNoFileComp {
		t.Errorf("directive = %v, want NoFileComp", dir)
	}
	want := []string{"gemma-4-e4b-it", "qwen2.5-3b-instruct"} // alphabetical
	if !slices.Equal(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

// TestCompleteInstalledModelsExcludesPositional pins serve --draft's
// behavior: drafting a model with itself makes no sense and SpeculativePair
// would refuse it; the completion shouldn't even suggest it.
func TestCompleteInstalledModelsExcludesPositional(t *testing.T) {
	store := newFakeModelStore()
	_ = store.Put(context.Background(), models.Metadata{ID: "qwen2.5-3b-instruct"})
	_ = store.Put(context.Background(), models.Metadata{ID: "qwen2.5-0.5b-instruct"})
	d := &Deps{ModelStore: store}

	got, _ := completeInstalledModels(d, "qwen2.5-3b-instruct")(&cobra.Command{}, nil, "")
	for _, id := range got {
		if id == "qwen2.5-3b-instruct" {
			t.Errorf("completion should not suggest the excluded id; got %v", got)
		}
	}
	if !slices.Contains(got, "qwen2.5-0.5b-instruct") {
		t.Errorf("completion should include other ids; got %v", got)
	}
}

func TestCompleteRunningServiceNames(t *testing.T) {
	ld := &fakeLaunchdService{
		ListResult: []launchd.ServiceInfo{
			{Label: "com.llamactl.qwen-rag"},
			{Label: "com.llamactl.qwen-utils"},
			{Label: "com.llamactl.gemma-chat"},
		},
	}
	d := &Deps{LaunchdService: ld}

	got, _ := completeRunningServiceNames(d)(&cobra.Command{}, nil, "")
	want := []string{"gemma-chat", "qwen-rag", "qwen-utils"} // alphabetical, prefix stripped
	if !slices.Equal(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

// TestCompletePreferredOrModelPathSkipsHFPath: once the user has typed
// a slash they're in HF-path mode; we deliberately stop suggesting so
// they can finish typing without preferred-id noise.
func TestCompletePreferredOrModelPathSkipsHFPath(t *testing.T) {
	got, _ := completePreferredOrModelPath()(&cobra.Command{}, nil, "Qwen/")
	if len(got) != 0 {
		t.Errorf("HF-path partial should suppress preferred-id suggestions; got %v", got)
	}
}

// TestCompletePreferredOrModelPathListsPreferredIDs: when no slash yet,
// every PreferredIDs entry is offered. cobra's framework filters by the
// toComplete prefix on the shell side, so we always return the full set.
func TestCompletePreferredOrModelPathListsPreferredIDs(t *testing.T) {
	got, _ := completePreferredOrModelPath()(&cobra.Command{}, nil, "qwen")
	if !slices.Contains(got, "qwen2.5-3b-instruct") {
		t.Errorf("expected qwen2.5-3b-instruct in suggestions; got %v", got)
	}
	if len(got) != len(models.PreferredIDs) {
		t.Errorf("got %d suggestions, want %d (all PreferredIDs)", len(got), len(models.PreferredIDs))
	}
}

func TestCompleteRecipeNames(t *testing.T) {
	got, _ := completeRecipeNames(&cobra.Command{}, nil, "")
	want := []string{"agent", "chat", "code", "long-context", "low-memory"} // alphabetical
	if !slices.Equal(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestCompleteConfigKeys(t *testing.T) {
	got, _ := completeConfigKeys(&cobra.Command{}, nil, "")
	for _, k := range []string{"api_key", "default_port", "hf_token", "llama_server_path", "log_level", "models_dir"} {
		if !slices.Contains(got, k) {
			t.Errorf("missing key %q in completion list %v", k, got)
		}
	}
}

// TestServeRecipeFlagCompletionRegistered confirms cobra knows about the
// recipe completer when serve runs through its constructor. Regression
// guard against accidentally dropping the RegisterFlagCompletionFunc
// call during a future refactor.
func TestServeRecipeFlagCompletionRegistered(t *testing.T) {
	d := &Deps{}
	cmd := newServeCmd(d)
	// cobra stores flag completions in an internal map; we invoke
	// completion via the framework's GetFlagCompletionFunc.
	fn, found := cmd.GetFlagCompletionFunc("recipe")
	if !found {
		t.Fatal("--recipe has no completion func registered")
	}
	got, _ := fn(cmd, nil, "")
	if !slices.Contains(got, "agent") {
		t.Errorf("--recipe completion missing 'agent'; got %v", got)
	}
}

// TestStopCompletionStripsPrefix confirms the integration of
// completeRunningServiceNames into newStopCmd: the suggestions are the
// raw run names users type, not the full launchd labels.
func TestStopCompletionStripsPrefix(t *testing.T) {
	ld := &fakeLaunchdService{
		ListResult: []launchd.ServiceInfo{
			{Label: "com.llamactl.my-run"},
		},
	}
	d := &Deps{LaunchdService: ld}
	cmd := newStopCmd(d)
	got, _ := cmd.ValidArgsFunction(cmd, nil, "")
	if !slices.Contains(got, "my-run") {
		t.Errorf("stop completion should suggest 'my-run' (prefix stripped); got %v", got)
	}
	for _, s := range got {
		if strings.HasPrefix(s, "com.llamactl.") {
			t.Errorf("completion %q still has com.llamactl. prefix", s)
		}
	}
}
