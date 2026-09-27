package dynamicflow

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type fakeHost struct {
	invalidTypedOnce bool
	mu               sync.Mutex
	calls            map[string]int
	history          map[string][]string
	entered          chan struct{}
	hold             bool
	parallel         chan string
	release          chan struct{}
}

func (f *fakeHost) Prepare(context.Context, string) (json.RawMessage, error) {
	return json.RawMessage(`{}`), nil
}
func (f *fakeHost) Actor(_ context.Context, _ string, _ json.RawMessage, p Persona, id string) (string, error) {
	if id != "" {
		return id, nil
	}
	return p.Name, nil
}
func (f *fakeHost) Ask(ctx context.Context, id, prompt string, typed bool, _ json.RawMessage) (string, error) {
	f.mu.Lock()
	f.calls[prompt]++
	f.history[id] = append(f.history[id], prompt)
	hold := f.hold && prompt == "second"
	invalidTyped := typed && f.invalidTypedOnce
	if invalidTyped {
		f.invalidTypedOnce = false
	}
	f.mu.Unlock()
	if f.parallel != nil {
		f.parallel <- id
		select {
		case <-f.release:
		case <-ctx.Done():
			return "", ctx.Err()
		}
	}
	if hold {
		select {
		case f.entered <- struct{}{}:
		default:
		}
		<-ctx.Done()
		return "", ctx.Err()
	}
	if invalidTyped {
		return `{"ok":"wrong-type"}`, nil
	}
	if typed {
		return `{"ok":true,"items":["A","B"]}`, nil
	}
	return "OUT:" + prompt, nil
}
func newManager(t *testing.T) (*Manager, *fakeHost, string, string) {
	t.Helper()
	runtimePath, err := filepath.Abs("../../workflow-runtime/output/workflow-runtime.mjs")
	require.NoError(t, err)
	if _, err = os.Stat(runtimePath); err != nil {
		t.Skip("build workflow-runtime before integration tests")
	}
	host := &fakeHost{calls: map[string]int{}, history: map[string][]string{}, entered: make(chan struct{}, 1)}
	dir := t.TempDir()
	m, err := New(dir, runtimePath, host)
	require.NoError(t, err)
	t.Cleanup(m.Close)
	return m, host, dir, runtimePath
}
func waitRun(t *testing.T, m *Manager, id string) *Run {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	run, err := m.Wait(ctx, id)
	require.NoError(t, err)
	return run
}
func TestDynamicParallelTypedBranchAndActorContext(t *testing.T) {
	m, host, _, _ := newManager(t)
	run, err := m.Start(context.Background(), "parallel", `interface Plan {ok:boolean;items:string[]} const a=agent("author"); const b=agent("reviewer"); const plan=await a.ask<Plan>("plan"); if(!plan.ok) throw new Error("bad plan"); const pieces=await Promise.all(plan.items.map(item=>b.ask(item))); return await a.ask(pieces.join("|"));`, "")
	require.NoError(t, err)
	done := waitRun(t, m, run.ID)
	require.Equal(t, "completed", done.Status, done.Error)
	require.Contains(t, string(done.Result), "OUT:A|OUT:B")
	host.mu.Lock()
	defer host.mu.Unlock()
	require.Equal(t, []string{"A", "B"}, host.history["reviewer"])
	require.Len(t, host.history["author"], 2)
}
func TestDynamicCancelPersistAndResumeSkipsCompletedAsk(t *testing.T) {
	m, host, dir, path := newManager(t)
	host.hold = true
	run, err := m.Start(context.Background(), "resume", `const a=agent("author"); await a.ask("first"); return await a.ask("second");`, "")
	require.NoError(t, err)
	select {
	case <-host.entered:
	case <-time.After(60 * time.Second):
		t.Fatal("second ask not started")
	}
	require.NoError(t, m.Cancel(run.ID))
	require.Equal(t, "cancelled", waitRun(t, m, run.ID).Status)
	m.Close()
	host.mu.Lock()
	host.hold = false
	host.mu.Unlock()
	restored, err := New(dir, path, host)
	require.NoError(t, err)
	defer restored.Close()
	_, err = restored.Resume(context.Background(), run.ID)
	require.NoError(t, err)
	done := waitRun(t, restored, run.ID)
	require.Equal(t, "completed", done.Status, done.Error)
	host.mu.Lock()
	defer host.mu.Unlock()
	require.Equal(t, 1, host.calls["first"])
	require.Equal(t, 2, host.calls["second"])
}
func TestDynamicInvalidScriptNeverCallsHost(t *testing.T) {
	m, host, _, _ := newManager(t)
	for _, script := range []string{`return process.env;`, `const a=agent("x");return a.ask<number>("number"); invalid;`} {
		run, err := m.Start(context.Background(), "invalid", script, "")
		require.NoError(t, err)
		done := waitRun(t, m, run.ID)
		require.Equal(t, "failed", done.Status)
		require.NotEmpty(t, done.Error)
	}
	require.Empty(t, host.calls)
	_, err := m.Get("../../etc/passwd")
	require.True(t, errors.Is(err, os.ErrNotExist))
	_, err = m.Start(context.Background(), "large", strings.Repeat("x", 65<<10), "")
	require.Error(t, err)
}
func TestDynamicBusyScriptCancellation(t *testing.T) {
	m, _, _, _ := newManager(t)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	run, err := m.Start(ctx, "busy", `while(true) {}`, "")
	require.NoError(t, err)
	require.Equal(t, "cancelled", waitRun(t, m, run.ID).Status)
}

func TestIndependentActorsRunConcurrently(t *testing.T) {
	m, host, _, _ := newManager(t)
	host.parallel = make(chan string, 2)
	host.release = make(chan struct{})
	run, err := m.Start(context.Background(), "parallel actors", `const a=agent("alpha"); const b=agent("beta"); return await Promise.all([a.ask("one"),b.ask("two")]);`, "")
	require.NoError(t, err)
	entered := map[string]bool{}
	for len(entered) < 2 {
		select {
		case id := <-host.parallel:
			entered[id] = true
		case <-time.After(60 * time.Second):
			t.Fatal("actors did not overlap")
		}
	}
	close(host.release)
	require.Equal(t, "completed", waitRun(t, m, run.ID).Status)
}

func TestTypedAskRepairsInvalidResultInSameActor(t *testing.T) {
	m, host, _, _ := newManager(t)
	host.invalidTypedOnce = true
	run, err := m.Start(context.Background(), "typed repair", `interface Plan {ok:boolean;items:string[]} const a=agent("author"); const p=await a.ask<Plan>("plan"); return p.items;`, "")
	require.NoError(t, err)
	done := waitRun(t, m, run.ID)
	require.Equal(t, "completed", done.Status, done.Error)
	host.mu.Lock()
	defer host.mu.Unlock()
	require.Len(t, host.history["author"], 2)
	require.Contains(t, host.history["author"][1], "Validation errors")
}

func TestIncompleteRunMetadataDoesNotBlockStartup(t *testing.T) {
	dir := t.TempDir()
	for _, id := range []string{"dw-000000000000000000000000", "dw-111111111111111111111111"} {
		require.NoError(t, os.Mkdir(filepath.Join(dir, id), 0700))
	}
	require.NoError(t, os.WriteFile(filepath.Join(dir, "dw-111111111111111111111111", "run.json"), []byte("incomplete"), 0600))
	m, err := New(dir, "missing-runtime", nil)
	require.NoError(t, err)
	defer m.Close()
	require.Empty(t, m.List())
}
