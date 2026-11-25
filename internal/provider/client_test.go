package provider

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/theocourtois/hetzner-ecoplus/internal/api"
)

func clientFor(t *testing.T, h http.HandlerFunc) *Client {
	t.Helper()
	ts := httptest.NewServer(h)
	t.Cleanup(ts.Close)
	return NewClient(ts.URL+"/", "tok", "9.9.9", time.Millisecond)
}

func TestDeleteNotFoundIsSuccess(t *testing.T) {
	c := clientFor(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"code":"not_found","message":"gone"}`))
	})
	if err := c.Delete(context.Background(), 1, "lease"); err != nil {
		t.Fatalf("delete of a missing server must succeed: %v", err)
	}
}

func TestDeleteOtherErrorsPropagate(t *testing.T) {
	c := clientFor(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusConflict)
		_, _ = w.Write([]byte(`{"code":"conflict","message":"no"}`))
	})
	if err := c.Delete(context.Background(), 1, "lease"); err == nil {
		t.Fatal("expected error")
	}
}

func TestHeadersSent(t *testing.T) {
	var got http.Header
	c := clientFor(t, func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Clone()
		_, _ = w.Write([]byte(`{"id":4,"lease":"l"}`))
	})
	srv, err := c.Get(context.Background(), 4, "the-lease")
	if err != nil || srv.ID != 4 {
		t.Fatalf("%+v %v", srv, err)
	}
	if got.Get("Authorization") != "Bearer tok" || got.Get("User-Agent") != "terraform-provider-ecoplus/9.9.9" ||
		got.Get(api.HeaderLease) != "the-lease" {
		t.Fatalf("headers: %v", got)
	}
}

func TestRetriesServiceUnavailableThenSucceeds(t *testing.T) {
	var calls atomic.Int32
	c := clientFor(t, func(w http.ResponseWriter, _ *http.Request) {
		if calls.Add(1) < 3 {
			w.Header().Set("Retry-After", "1")
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte(`{"code":"rate_limited","message":"slow down"}`))
			return
		}
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"id":9,"lease":"l"}`))
	})
	srv, err := c.Create(context.Background(), api.ServerRequest{Name: "a"})
	if err != nil || srv.ID != 9 || calls.Load() != 3 {
		t.Fatalf("%+v %v calls=%d", srv, err, calls.Load())
	}
}

func TestRetriesAreBounded(t *testing.T) {
	var calls atomic.Int32
	c := clientFor(t, func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte(`{"code":"not_ready","message":"later"}`))
	})
	if _, err := c.Get(context.Background(), 1, "l"); err == nil || !strings.Contains(err.Error(), "not_ready") {
		t.Fatalf("expected not_ready, got %v", err)
	}
	if calls.Load() != maxAttempts {
		t.Fatalf("expected %d attempts, got %d", maxAttempts, calls.Load())
	}
}

func TestRetryStopsWhenContextEnds(t *testing.T) {
	c := clientFor(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte(`{"code":"upstream_unavailable","message":"x"}`))
	})
	c.backoff = time.Hour
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if _, err := c.Get(ctx, 1, "l"); err == nil || !strings.Contains(err.Error(), "deadline") {
		t.Fatalf("expected deadline error, got %v", err)
	}
}

func TestNonRetryableErrorsReturnImmediately(t *testing.T) {
	var calls atomic.Int32
	c := clientFor(t, func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"code":"invalid_request","message":"bad"}`))
	})
	_, err := c.Patch(context.Background(), 1, "l", api.ServerPatch{})
	if err == nil || calls.Load() != 1 || IsNotFound(err) {
		t.Fatalf("err=%v calls=%d", err, calls.Load())
	}
}

func TestUnstructuredErrorBodies(t *testing.T) {
	for status, want := range map[int]api.Code{500: api.CodeInternal, 502: api.CodeUpstream, 504: api.CodeUpstream} {
		c := clientFor(t, func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(status)
			_, _ = w.Write([]byte("<html>oops</html>"))
		})
		c.backoff = time.Microsecond
		_, err := c.Get(context.Background(), 1, "l")
		var apiErr *api.Error
		if !errors.As(err, &apiErr) || apiErr.Code != want {
			t.Errorf("status %d: got %v", status, err)
		}
	}
}

func TestUnreachableControllerIsRetryableUpstream(t *testing.T) {
	c := NewClient("http://127.0.0.1:1", "t", "v", time.Microsecond)
	_, err := c.Get(context.Background(), 1, "l")
	var apiErr *api.Error
	if !errors.As(err, &apiErr) || apiErr.Code != api.CodeUpstream {
		t.Fatalf("got %v", err)
	}
}

func TestMalformedSuccessBody(t *testing.T) {
	c := clientFor(t, func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("{not json")) })
	if _, err := c.Get(context.Background(), 1, "l"); err == nil {
		t.Fatal("expected decode error")
	}
}
