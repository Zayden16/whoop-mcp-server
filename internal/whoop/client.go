package whoop

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

const BaseURL = "https://api.prod.whoop.com/developer"

// Client is a minimal Whoop API v2 client with automatic token refresh.
type Client struct {
	clientID     string
	clientSecret string
	store        *TokenStore
	http         *http.Client
}

func NewClient(clientID, clientSecret string) (*Client, error) {
	store, err := NewTokenStore()
	if err != nil {
		return nil, err
	}
	return &Client{
		clientID:     clientID,
		clientSecret: clientSecret,
		store:        store,
		http:         &http.Client{Timeout: 30 * time.Second},
	}, nil
}

// Get performs an authenticated GET against the given API path (e.g.
// "/v2/cycle") with optional query params and returns the raw JSON response.
func (c *Client) Get(ctx context.Context, path string, query url.Values) (json.RawMessage, error) {
	token, err := c.store.AccessToken(ctx, c.clientID, c.clientSecret)
	if err != nil {
		return nil, err
	}

	u := BaseURL + path
	if len(query) > 0 {
		u += "?" + query.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("whoop API %s returned %s: %s", path, resp.Status, truncate(string(body), 500))
	}
	return json.RawMessage(body), nil
}

// GetPaginated follows nextToken pagination, collecting all records between
// start and end (RFC3339 timestamps, both optional), capped at maxRecords.
func (c *Client) GetPaginated(ctx context.Context, path, start, end string, maxRecords int) ([]json.RawMessage, error) {
	var records []json.RawMessage
	nextToken := ""
	for {
		q := url.Values{"limit": {"25"}}
		if start != "" {
			q.Set("start", start)
		}
		if end != "" {
			q.Set("end", end)
		}
		if nextToken != "" {
			q.Set("nextToken", nextToken)
		}

		raw, err := c.Get(ctx, path, q)
		if err != nil {
			return nil, err
		}
		var page struct {
			Records   []json.RawMessage `json:"records"`
			NextToken string            `json:"next_token"`
		}
		if err := json.Unmarshal(raw, &page); err != nil {
			return nil, fmt.Errorf("decoding %s response: %w", path, err)
		}
		records = append(records, page.Records...)
		if page.NextToken == "" || len(records) >= maxRecords {
			break
		}
		nextToken = page.NextToken
	}
	if len(records) > maxRecords {
		records = records[:maxRecords]
	}
	return records, nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
