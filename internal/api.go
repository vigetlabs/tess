package internal

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const defaultBaseURL = "https://api.latticehq.com/"

type Client struct {
	base   *url.URL
	http   *http.Client
	apiKey string
}

func NewClient(apiKey string) (*Client, error) {
	if strings.TrimSpace(apiKey) == "" {
		return nil, fmt.Errorf("api key is empty")
	}
	u, _ := url.Parse(defaultBaseURL)
	return &Client{
		base:   u,
		http:   &http.Client{Timeout: 15 * time.Second},
		apiKey: apiKey,
	}, nil
}

func (c *Client) resolve(pathOrURL string) (string, error) {
	if strings.HasPrefix(pathOrURL, "http://") || strings.HasPrefix(pathOrURL, "https://") {
		return pathOrURL, nil
	}
	if strings.HasPrefix(pathOrURL, "/") {
		// Treat as absolute path relative to base host
		u := *c.base
		u.Path = strings.TrimSuffix(c.base.Path, "/") + pathOrURL
		return u.String(), nil
	}
	// Relative path
	u := c.base.ResolveReference(&url.URL{Path: pathOrURL})
	return u.String(), nil
}

func (c *Client) newRequest(ctx context.Context, method, pathOrURL string, body io.Reader) (*http.Request, error) {
	full, err := c.resolve(pathOrURL)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, method, full, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("accept", "application/json")
	// Prefer a Bearer token; allow preformatted values in config.
	req.Header.Set("Authorization", c.authHeaderValue())
	return req, nil
}

func (c *Client) authHeaderValue() string {
	v := strings.TrimSpace(c.apiKey)
	if v == "" {
		return ""
	}
	lower := strings.ToLower(v)
	if strings.HasPrefix(lower, "bearer ") || strings.HasPrefix(lower, "basic ") || strings.HasPrefix(lower, "token ") || strings.HasPrefix(lower, "lattice ") {
		return v
	}
	return "Bearer " + v
}

func (c *Client) doJSON(req *http.Request, v any) error {
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<10))
		return fmt.Errorf("http %d: %s", resp.StatusCode, strings.TrimSpace(string(b)))
	}
	if v == nil {
		io.Copy(io.Discard, resp.Body)
		return nil
	}
	dec := json.NewDecoder(resp.Body)
	return dec.Decode(v)
}

// Types mapped to the subset of fields we need
type ListRef struct {
	Object string `json:"object"`
	URL    string `json:"url"`
}

type User struct {
	ID            string  `json:"id"`
	Name          string  `json:"name"`
	Email         string  `json:"email"`
	DirectReports ListRef `json:"directReports"`
}

type userListResponse struct {
	Object       string `json:"object"`
	HasMore      bool   `json:"hasMore"`
	EndingCursor any    `json:"endingCursor"`
	Data         []User `json:"data"`
}

// Review cycles
type ReviewCycle struct {
	ID        string  `json:"id"`
	Name      string  `json:"name"`
	Reviewees ListRef `json:"reviewees"`
}

type reviewCycleListResponse struct {
	Object       string        `json:"object"`
	HasMore      bool          `json:"hasMore"`
	EndingCursor any           `json:"endingCursor"`
	Data         []ReviewCycle `json:"data"`
}

// Reviewees
type UserRef struct {
	ID string `json:"id"`
}

type Reviewee struct {
	ID   string  `json:"id"`
	User UserRef `json:"user"`
}

type revieweeListResponse struct {
	Object       string     `json:"object"`
	HasMore      bool       `json:"hasMore"`
	EndingCursor any        `json:"endingCursor"`
	Data         []Reviewee `json:"data"`
}

func (c *Client) GetMe(ctx context.Context) (*User, error) {
	req, err := c.newRequest(ctx, http.MethodGet, "/v1/me", nil)
	if err != nil {
		return nil, err
	}
	var u User
	if err := c.doJSON(req, &u); err != nil {
		return nil, err
	}
	return &u, nil
}

func (c *Client) ListUsersByURL(ctx context.Context, listURL string) ([]User, error) {
	req, err := c.newRequest(ctx, http.MethodGet, listURL, nil)
	if err != nil {
		return nil, err
	}
	var lr userListResponse
	if err := c.doJSON(req, &lr); err != nil {
		return nil, err
	}
	return lr.Data, nil
}

func (c *Client) ListReviewCycles(ctx context.Context) ([]ReviewCycle, error) {
	req, err := c.newRequest(ctx, http.MethodGet, "/v1/reviewCycles", nil)
	if err != nil {
		return nil, err
	}
	var lr reviewCycleListResponse
	if err := c.doJSON(req, &lr); err != nil {
		return nil, err
	}
	return lr.Data, nil
}

func (c *Client) ListRevieweesByURL(ctx context.Context, listURL string) ([]Reviewee, error) {
	req, err := c.newRequest(ctx, http.MethodGet, listURL, nil)
	if err != nil {
		return nil, err
	}
	var lr revieweeListResponse
	if err := c.doJSON(req, &lr); err != nil {
		return nil, err
	}
	return lr.Data, nil
}
