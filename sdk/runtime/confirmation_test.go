package runtime

import (
	"context"
	"testing"

	"github.com/hwj123hwj/easyagent/sdk/agent"
	"github.com/hwj123hwj/easyagent/sdk/ai/providers"
	"github.com/hwj123hwj/easyagent/sdk/config"
	"github.com/hwj123hwj/easyagent/sdk/extensions"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type confirmationTestApplication struct {
	buildToolsCalls int
}

func (a *confirmationTestApplication) BuildTools(ToolBuildOptions) []agent.Tool {
	a.buildToolsCalls++
	return nil
}

func (*confirmationTestApplication) BuildPrompt(PromptBuildOptions, string, string) string {
	return "test prompt"
}

func (*confirmationTestApplication) NewSessionExt() SessionExt { return nil }

func TestSetConfirmFuncRebuildsExistingAgent(t *testing.T) {
	application := &confirmationTestApplication{}
	session := &AgentSession{
		cfg:         config.Default(),
		extRegistry: extensions.NewRegistry(),
		deps:        Dependencies{Registry: providers.NewRegistry()},
		application: application,
		agent:       agent.New(agent.Options{}),
	}
	oldAgent := session.agent

	session.SetConfirmFunc(func(context.Context, agent.ConfirmationRequest) agent.ConfirmDecision {
		return agent.ConfirmDecision{Approved: true}
	})

	require.NotSame(t, oldAgent, session.agent)
	assert.Equal(t, 1, application.buildToolsCalls)
	assert.True(t, session.ConfirmEnabled(), "installing a confirmation handler enables confirmation by default")
}

func TestWrapConfirmPreservesFullAccessAcrossAgentRebuilds(t *testing.T) {
	session := &AgentSession{}
	confirmCalls := 0
	session.confirmFunc = func(context.Context, agent.ConfirmationRequest) agent.ConfirmDecision {
		confirmCalls++
		return agent.ConfirmDecision{Approved: false}
	}
	session.confirmEnabled.Store(false)

	for range 2 {
		decision := session.wrapConfirm(session.confirmFunc)(context.Background(), agent.ConfirmationRequest{})
		assert.True(t, decision.Approved, "full access must bypass confirmation")
	}
	assert.Zero(t, confirmCalls, "full access must not invoke the dialog callback")
	assert.False(t, session.ConfirmEnabled())

	session.SetConfirmEnabled(true)
	decision := session.wrapConfirm(session.confirmFunc)(context.Background(), agent.ConfirmationRequest{})
	assert.False(t, decision.Approved, "turning confirmation back on must call the handler")
	assert.Equal(t, 1, confirmCalls)
}

func TestExplicitAccessModeRetainsMCPApprovalRule(t *testing.T) {
	session := &AgentSession{}
	calls := 0
	confirm := func(context.Context, agent.ConfirmationRequest) agent.ConfirmDecision {
		calls++
		return agent.ConfirmDecision{Approved: false}
	}
	require.NoError(t, session.TrySetAccessMode(confirm, false))
	require.Equal(t, "full", session.AccessMode())
	require.True(t, session.wrapConfirm(confirm)(context.Background(), agent.ConfirmationRequest{}).Approved)
	require.False(t, session.wrapConfirm(confirm)(context.Background(), agent.ConfirmationRequest{RequiresApproval: true}).Approved)
	require.Equal(t, 1, calls)
	require.NoError(t, session.TrySetConfirmFunc(confirm))
	require.Equal(t, "full", session.AccessMode())
	require.NoError(t, session.TrySetAccessMode(confirm, true))
	require.False(t, session.wrapConfirm(confirm)(context.Background(), agent.ConfirmationRequest{}).Approved)
	require.Equal(t, 2, calls)
}

func TestContextSnapshotRejectsActiveMutation(t *testing.T) {
	for _, field := range []string{"running", "mutating"} {
		t.Run(field, func(t *testing.T) {
			sess := &AgentSession{agent: agent.New(agent.Options{System: "rules"})}
			if field == "running" {
				sess.running = true
			} else {
				sess.mutating = true
			}
			_, err := sess.ContextSnapshot(context.Background())
			require.ErrorIs(t, err, agent.ErrAgentBusy)
			sess.running = false
			sess.mutating = false
			snapshot, err := sess.ContextSnapshot(context.Background())
			require.NoError(t, err)
			require.Equal(t, "rules", snapshot.System)
		})
	}
}

func TestMandatoryConfirmationSurvivesGlobalAutoApprove(t *testing.T) {
	session := &AgentSession{cfg: config.Config{AutoApprove: true}}
	calls := 0
	confirm := func(context.Context, agent.ConfirmationRequest) agent.ConfirmDecision {
		calls++
		return agent.ConfirmDecision{Approved: false}
	}
	decision := session.wrapConfirm(confirm)(context.Background(), agent.ConfirmationRequest{RequiresApproval: true, ForceConfirmation: true})
	require.False(t, decision.Approved)
	require.Equal(t, 1, calls)
	require.True(t, session.wrapConfirm(confirm)(context.Background(), agent.ConfirmationRequest{RequiresApproval: true}).Approved, "existing explicit server trust remains compatible")
}
