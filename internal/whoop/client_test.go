package whoop

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// testClient returns a Client pointed at the given server with a valid
// cached token so no refresh is attempted.
func testClient(t *testing.T, serverURL string) *Client {
	t.Helper()
	store := &TokenStore{
		path: filepath.Join(t.TempDir(), "token.json"),
		tok: &Token{
			AccessToken: "test-token",
			ExpiresAt:   time.Now().Add(time.Hour),
		},
	}
	return &Client{
		store:   store,
		http:    &http.Client{Timeout: 5 * time.Second},
		baseURL: serverURL,
	}
}

func TestGetSendsBearerToken(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer test-token" {
			t.Errorf("Authorization = %q, want %q", got, "Bearer test-token")
		}
		fmt.Fprint(w, `{"ok":true}`)
	}))
	defer srv.Close()

	raw, err := testClient(t, srv.URL).Get(context.Background(), "/v2/user/profile/basic", nil)
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != `{"ok":true}` {
		t.Errorf("body = %s", raw)
	}
}

func TestGetPaginatedFollowsNextToken(t *testing.T) {
	var tokens []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		next := r.URL.Query().Get("nextToken")
		tokens = append(tokens, next)
		switch next {
		case "":
			fmt.Fprint(w, `{"records":[{"id":1},{"id":2}],"next_token":"page2"}`)
		case "page2":
			fmt.Fprint(w, `{"records":[{"id":3}],"next_token":""}`)
		default:
			t.Errorf("unexpected nextToken %q", next)
		}
	}))
	defer srv.Close()

	records, err := testClient(t, srv.URL).GetPaginated(context.Background(), "/v2/cycle", "", "", 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 3 {
		t.Fatalf("got %d records, want 3", len(records))
	}
	if len(tokens) != 2 || tokens[1] != "page2" {
		t.Errorf("pagination tokens = %v, want [\"\" \"page2\"]", tokens)
	}
}

func TestGetPaginatedRespectsMaxRecords(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"records":[{"id":1},{"id":2},{"id":3}],"next_token":"more"}`)
	}))
	defer srv.Close()

	records, err := testClient(t, srv.URL).GetPaginated(context.Background(), "/v2/cycle", "", "", 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 2 {
		t.Fatalf("got %d records, want 2 (capped)", len(records))
	}
}

func TestGetRetriesOn429(t *testing.T) {
	var calls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls == 1 {
			w.Header().Set("Retry-After", "0")
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		fmt.Fprint(w, `{"ok":true}`)
	}))
	defer srv.Close()

	raw, err := testClient(t, srv.URL).Get(context.Background(), "/v2/cycle", nil)
	if err != nil {
		t.Fatal(err)
	}
	if calls != 2 {
		t.Errorf("calls = %d, want 2", calls)
	}
	if string(raw) != `{"ok":true}` {
		t.Errorf("body = %s", raw)
	}
}

func TestGetGivesUpAfterSecond429(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "0")
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer srv.Close()

	_, err := testClient(t, srv.URL).Get(context.Background(), "/v2/cycle", nil)
	if err == nil {
		t.Fatal("expected error after repeated 429")
	}
}

func TestGetErrorIncludesBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		fmt.Fprint(w, `{"error":"invalid token"}`)
	}))
	defer srv.Close()

	_, err := testClient(t, srv.URL).Get(context.Background(), "/v2/cycle", nil)
	if err == nil {
		t.Fatal("expected error")
	}
	if want := "invalid token"; !strings.Contains(err.Error(), want) {
		t.Errorf("error %q does not contain %q", err, want)
	}
}

func TestGetPassesQueryParams(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("limit"); got != "25" {
			t.Errorf("limit = %q, want 25", got)
		}
		if got := r.URL.Query().Get("start"); got != "2026-07-01T00:00:00Z" {
			t.Errorf("start = %q", got)
		}
		fmt.Fprint(w, `{}`)
	}))
	defer srv.Close()

	q := url.Values{"limit": {"25"}, "start": {"2026-07-01T00:00:00Z"}}
	if _, err := testClient(t, srv.URL).Get(context.Background(), "/v2/cycle", q); err != nil {
		t.Fatal(err)
	}
}

func TestRetryAfter(t *testing.T) {
	tests := []struct {
		header string
		want   time.Duration
	}{
		{"", time.Second},
		{"3", 3 * time.Second},
		{"60", 10 * time.Second}, // capped
		{"garbage", time.Second},
		{"-1", time.Second},
	}
	for _, tt := range tests {
		if got := retryAfter(tt.header); got != tt.want {
			t.Errorf("retryAfter(%q) = %v, want %v", tt.header, got, tt.want)
		}
	}
}

func TestTokenExpiry(t *testing.T) {
	fresh := &Token{ExpiresAt: time.Now().Add(time.Hour)}
	if fresh.expired() {
		t.Error("fresh token reported expired")
	}
	// Within the 1-minute early-refresh window counts as expired.
	closeToExpiry := &Token{ExpiresAt: time.Now().Add(30 * time.Second)}
	if !closeToExpiry.expired() {
		t.Error("token inside early-refresh window not reported expired")
	}
	stale := &Token{ExpiresAt: time.Now().Add(-time.Hour)}
	if !stale.expired() {
		t.Error("stale token not reported expired")
	}
}

func TestTokenStoreRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", "token.json")
	store := &TokenStore{path: path}
	want := &Token{
		AccessToken:  "at",
		RefreshToken: "rt",
		ExpiresAt:    time.Now().Add(time.Hour).Truncate(time.Second),
	}
	if err := store.save(want); err != nil {
		t.Fatal(err)
	}

	reloaded := &TokenStore{path: path}
	got, err := reloaded.load()
	if err != nil {
		t.Fatal(err)
	}
	if got.AccessToken != want.AccessToken || got.RefreshToken != want.RefreshToken {
		t.Errorf("got %+v, want %+v", got, want)
	}
}

func TestPageDecodeError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `not json`)
	}))
	defer srv.Close()

	_, err := testClient(t, srv.URL).GetPaginated(context.Background(), "/v2/cycle", "", "", 10)
	if err == nil {
		t.Fatal("expected decode error")
	}
}
