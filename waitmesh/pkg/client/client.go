package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/waitmesh/waitmesh/internal/domain"
	"github.com/waitmesh/waitmesh/internal/storage/sqlite"
)

type Client struct {
	base   string
	token  string
	http   *http.Client
}

type Option func(*Client)

func WithToken(token string) Option {
	return func(c *Client) { c.token = token }
}

func WithHTTPClient(h *http.Client) Option {
	return func(c *Client) { c.http = h }
}

func WithTimeout(d time.Duration) Option {
	return func(c *Client) {
		if c.http == nil {
			c.http = &http.Client{}
		}
		c.http.Timeout = d
	}
}

func New(baseURL string, opts ...Option) *Client {
	c := &Client{
		base: strings.TrimRight(baseURL, "/"),
		http: &http.Client{Timeout: 15 * time.Second},
	}
	for _, o := range opts {
		o(c)
	}
	return c
}

type APIError struct {
	Status int
	Body   string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("waitmesh api %d: %s", e.Status, e.Body)
}

func (c *Client) Publish(ctx context.Context, in domain.EventInput) (map[string]any, error) {
	var out map[string]any
	err := c.do(ctx, http.MethodPost, "/v1/events", in, &out)
	return out, err
}

func (c *Client) AddRule(ctx context.Context, in domain.RuleInput) (domain.Rule, error) {
	var out domain.Rule
	err := c.do(ctx, http.MethodPost, "/v1/rules", in, &out)
	return out, err
}

func (c *Client) ListRules(ctx context.Context) ([]domain.Rule, error) {
	var out struct {
		Rules []domain.Rule `json:"rules"`
	}
	err := c.do(ctx, http.MethodGet, "/v1/rules", nil, &out)
	return out.Rules, err
}

func (c *Client) EnableRule(ctx context.Context, id string) error {
	return c.do(ctx, http.MethodPatch, "/v1/rules/"+id, map[string]bool{"enabled": true}, nil)
}

func (c *Client) DisableRule(ctx context.Context, id string) error {
	return c.do(ctx, http.MethodPatch, "/v1/rules/"+id, map[string]bool{"enabled": false}, nil)
}

func (c *Client) ListEvents(ctx context.Context) ([]domain.Event, error) {
	var out struct {
		Events []domain.Event `json:"events"`
	}
	err := c.do(ctx, http.MethodGet, "/v1/events", nil, &out)
	return out.Events, err
}

func (c *Client) ListPending(ctx context.Context) ([]domain.PendingMatch, error) {
	var out struct {
		Pending []domain.PendingMatch `json:"pending"`
	}
	err := c.do(ctx, http.MethodGet, "/v1/pending", nil, &out)
	return out.Pending, err
}

func (c *Client) RegisterConsumer(ctx context.Context, consumerID string) error {
	return c.do(ctx, http.MethodPost, "/v1/consumers", map[string]string{
		"consumer_id": consumerID,
	}, nil)
}

func (c *Client) Poll(ctx context.Context, consumerID string, limit int) ([]sqlite.Delivery, error) {
	var out struct {
		Deliveries []sqlite.Delivery `json:"deliveries"`
	}
	err := c.do(ctx, http.MethodPost, "/v1/outputs/poll", map[string]any{
		"consumer_id": consumerID, "limit": limit, "lease_seconds": 30,
	}, &out)
	return out.Deliveries, err
}

func (c *Client) Ack(ctx context.Context, consumerID, outputID string) error {
	return c.do(ctx, http.MethodPost, "/v1/outputs/ack", map[string]string{
		"consumer_id": consumerID, "output_id": outputID,
	}, nil)
}

func (c *Client) Nack(ctx context.Context, consumerID, outputID string) error {
	return c.do(ctx, http.MethodPost, "/v1/outputs/nack", map[string]string{
		"consumer_id": consumerID, "output_id": outputID,
	}, nil)
}

func (c *Client) do(ctx context.Context, method, path string, body any, out any) error {
	var rdr io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rdr = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.base+path, rdr)
	if err != nil {
		return err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	res, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	data, _ := io.ReadAll(res.Body)
	if res.StatusCode >= 400 {
		return &APIError{Status: res.StatusCode, Body: string(data)}
	}
	if out == nil || res.StatusCode == http.StatusNoContent || len(data) == 0 {
		return nil
	}
	return json.Unmarshal(data, out)
}
