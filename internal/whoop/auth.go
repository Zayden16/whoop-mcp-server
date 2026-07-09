package whoop

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

const (
	AuthURL  = "https://api.prod.whoop.com/oauth/oauth2/auth"
	TokenURL = "https://api.prod.whoop.com/oauth/oauth2/token"
)

// Scopes requested during authorization. "offline" is required to receive a
// refresh token.
var Scopes = []string{
	"read:cycles",
	"read:recovery",
	"read:sleep",
	"read:workout",
	"read:profile",
	"read:body_measurement",
	"offline",
}

// Token is the persisted OAuth token state.
type Token struct {
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token"`
	ExpiresAt    time.Time `json:"expires_at"`
}

func (t *Token) expired() bool {
	// Refresh a minute early to avoid using a token that dies mid-request.
	return time.Now().After(t.ExpiresAt.Add(-time.Minute))
}

type tokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int    `json:"expires_in"`
	TokenType    string `json:"token_type"`
	Scope        string `json:"scope"`
}

// TokenPath returns the on-disk location of the cached token.
func TokenPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "whoop-mcp", "token.json"), nil
}

// TokenStore loads and saves tokens, serializing refreshes.
type TokenStore struct {
	mu   sync.Mutex
	path string
	tok  *Token
}

func NewTokenStore() (*TokenStore, error) {
	path, err := TokenPath()
	if err != nil {
		return nil, err
	}
	return &TokenStore{path: path}, nil
}

func (s *TokenStore) load() (*Token, error) {
	if s.tok != nil {
		return s.tok, nil
	}
	data, err := os.ReadFile(s.path)
	if err != nil {
		return nil, err
	}
	var t Token
	if err := json.Unmarshal(data, &t); err != nil {
		return nil, err
	}
	s.tok = &t
	return s.tok, nil
}

func (s *TokenStore) save(t *Token) error {
	s.tok = t
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(t, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.path, data, 0o600)
}

// AccessToken returns a valid access token, refreshing it if expired.
func (s *TokenStore) AccessToken(ctx context.Context, clientID, clientSecret string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	tok, err := s.load()
	if err != nil {
		return "", fmt.Errorf("no cached token (run `whoop-mcp-server auth` first): %w", err)
	}
	if !tok.expired() {
		return tok.AccessToken, nil
	}
	if tok.RefreshToken == "" {
		return "", fmt.Errorf("access token expired and no refresh token available; run `whoop-mcp-server auth` again")
	}

	form := url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {tok.RefreshToken},
		"client_id":     {clientID},
		"client_secret": {clientSecret},
		"scope":         {"offline"},
	}
	fresh, err := requestToken(ctx, form)
	if err != nil {
		return "", fmt.Errorf("token refresh failed: %w", err)
	}
	if err := s.save(fresh); err != nil {
		return "", fmt.Errorf("saving refreshed token: %w", err)
	}
	return fresh.AccessToken, nil
}

func requestToken(ctx context.Context, form url.Values) (*Token, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, TokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var tr tokenResponse
	if resp.StatusCode != http.StatusOK {
		var body map[string]any
		_ = json.NewDecoder(resp.Body).Decode(&body)
		return nil, fmt.Errorf("token endpoint returned %s: %v", resp.Status, body)
	}
	if err := json.NewDecoder(resp.Body).Decode(&tr); err != nil {
		return nil, err
	}
	return &Token{
		AccessToken:  tr.AccessToken,
		RefreshToken: tr.RefreshToken,
		ExpiresAt:    time.Now().Add(time.Duration(tr.ExpiresIn) * time.Second),
	}, nil
}

// Authorize runs the browser-based OAuth authorization-code flow with a
// localhost callback server and persists the resulting token.
func Authorize(ctx context.Context, clientID, clientSecret string, port int) error {
	state, err := randomState()
	if err != nil {
		return err
	}
	redirectURI := fmt.Sprintf("http://localhost:%d/callback", port)

	authURL := AuthURL + "?" + url.Values{
		"response_type": {"code"},
		"client_id":     {clientID},
		"redirect_uri":  {redirectURI},
		"scope":         {strings.Join(Scopes, " ")},
		"state":         {state},
	}.Encode()

	codeCh := make(chan string, 1)
	errCh := make(chan error, 1)

	mux := http.NewServeMux()
	mux.HandleFunc("/callback", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if q.Get("state") != state {
			http.Error(w, "state mismatch", http.StatusBadRequest)
			errCh <- fmt.Errorf("oauth state mismatch")
			return
		}
		if e := q.Get("error"); e != "" {
			http.Error(w, "authorization failed: "+e, http.StatusBadRequest)
			errCh <- fmt.Errorf("authorization failed: %s (%s)", e, q.Get("error_description"))
			return
		}
		fmt.Fprintln(w, "Whoop authorization complete. You can close this tab.")
		codeCh <- q.Get("code")
	})

	srv := &http.Server{Addr: fmt.Sprintf("localhost:%d", port), Handler: mux}
	go func() {
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			errCh <- err
		}
	}()
	defer srv.Shutdown(context.Background())

	fmt.Fprintf(os.Stderr, "Opening browser for Whoop authorization...\nIf it does not open, visit:\n%s\n", authURL)
	openBrowser(authURL)

	var code string
	select {
	case code = <-codeCh:
	case err := <-errCh:
		return err
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(5 * time.Minute):
		return fmt.Errorf("timed out waiting for authorization callback")
	}

	form := url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {code},
		"client_id":     {clientID},
		"client_secret": {clientSecret},
		"redirect_uri":  {redirectURI},
	}
	tok, err := requestToken(ctx, form)
	if err != nil {
		return err
	}

	store, err := NewTokenStore()
	if err != nil {
		return err
	}
	if err := store.save(tok); err != nil {
		return err
	}
	path, _ := TokenPath()
	fmt.Fprintf(os.Stderr, "Token saved to %s\n", path)
	return nil
}

func randomState() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func openBrowser(u string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", u)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", u)
	default:
		cmd = exec.Command("xdg-open", u)
	}
	_ = cmd.Start()
}
