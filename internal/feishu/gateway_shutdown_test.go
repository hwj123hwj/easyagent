package feishu

import (
	"context"
	"errors"
	"testing"
	"time"
)

type blockedGatewayClient struct{ stopped chan struct{} }

func (c *blockedGatewayClient) Start(context.Context) error { <-c.stopped; return nil }
func (c *blockedGatewayClient) Close()                      { close(c.stopped) }

func TestGatewayCancellationClosesClient(t *testing.T) {
	client := &blockedGatewayClient{stopped: make(chan struct{})}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- runGatewayClient(ctx, client) }()
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("gateway did not stop after cancellation")
	}
	select {
	case <-client.stopped:
	default:
		t.Fatal("client not closed")
	}
}

type failingGatewayClient struct{ closed bool }

func (*failingGatewayClient) Start(context.Context) error { return errors.New("connect failed") }
func (c *failingGatewayClient) Close()                    { c.closed = true }

func TestGatewayStartupFailureIsReturned(t *testing.T) {
	client := &failingGatewayClient{}
	if err := runGatewayClient(context.Background(), client); err == nil || !client.closed {
		t.Fatal("startup failure lost")
	}
}
