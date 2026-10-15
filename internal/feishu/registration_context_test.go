package feishu

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestRegistrationRequestCancelsInFlight(t *testing.T) {
	entered := make(chan struct{})
	released := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.ParseForm()
		close(entered)
		<-r.Context().Done()
		close(released)
	}))
	defer srv.Close()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, err := postRegistrationContext(ctx, srv.URL, map[string]string{"action": "begin"})
		done <- err
	}()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("HTTP request not started")
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("HTTP cancellation hung")
	}
	select {
	case <-released:
	case <-time.After(time.Second):
		t.Fatal("server request remained alive")
	}
}

func TestRegistrationWaitCancelsAndDoesNotPollAgain(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	var count atomic.Int32
	done := make(chan error, 1)
	go func() {
		_, err := pollRegistration(ctx, "test-only", 600, 600, "feishu", func(string) { cancel() },
			func(context.Context, string, string) (*PollResult, string, error) {
				count.Add(1)
				return nil, "feishu", nil
			})
		done <- err
	}()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("poll interval ignored cancellation")
	}
	if count.Load() != 1 {
		t.Fatal("cancelled registration kept polling")
	}
}

func TestRegistrationDeadlineBoundsRequests(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Millisecond)
	defer cancel()
	_, err := pollRegistration(ctx, "test-only", 600, 600, "feishu", nil,
		func(ctx context.Context, _, _ string) (*PollResult, string, error) {
			<-ctx.Done()
			return nil, "feishu", ctx.Err()
		})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal(err)
	}
}

func TestRegistrationDenialStopsImmediately(t *testing.T) {
	_, err := pollRegistration(t.Context(), "test-only", 600, 600, "feishu", nil,
		func(context.Context, string, string) (*PollResult, string, error) {
			return nil, "feishu", &RegistrationError{ErrorCode: "access_denied"}
		})
	var denied *RegistrationError
	if !errors.As(err, &denied) || denied.ErrorCode != "access_denied" {
		t.Fatal(err)
	}
}
