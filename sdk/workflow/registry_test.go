package workflow

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGateCanResolveAsSoonAsWaitingIsPublished(t *testing.T) {
	r := NewRegistry(&Engine{})
	r.runs["test"] = &liveRun{gates: make(map[string]chan bool)}
	approved, err := r.WaitApprovalReady(context.Background(), "test", "gate", func() {
		require.NoError(t, r.Reject("test", "gate"))
	})
	require.NoError(t, err)
	require.False(t, approved)
	require.Empty(t, r.runs["test"].gates)
}
