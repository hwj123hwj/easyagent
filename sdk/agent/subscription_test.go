package agent

import (
	"context"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestSubscriptionsRemainIdentifiableAfterOtherListenersRemoved(t *testing.T) {
	a := &Agent{}
	first, second, third := 0, 0, 0
	offFirst := a.Subscribe(func(context.Context, AgentEvent) { first++ })
	offSecond := a.Subscribe(func(context.Context, AgentEvent) { second++ })
	offThird := a.Subscribe(func(context.Context, AgentEvent) { third++ })
	offFirst()
	offSecond()
	a.emit(context.Background(), EventAgentStart{})
	require.Zero(t, first)
	require.Zero(t, second)
	require.Equal(t, 1, third)
	offSecond()
	offThird()
	a.emit(context.Background(), EventAgentStart{})
	require.Equal(t, 1, third)
}
