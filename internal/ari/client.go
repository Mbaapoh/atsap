// Package ari implements a minimal client for the Asterisk REST Interface
// (ARI): REST calls for call/channel/bridge control, plus a WebSocket
// event stream for the Stasis application.
package ari

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// Client is a REST client bound to one ARI base URL and credential pair.
type Client struct {
	baseURL  string
	username string
	password string
	appName  string
	http     *http.Client
}

// New creates an ARI REST client. baseURL is the ARI root, e.g.
// "http://asterisk:8088/ari".
func New(baseURL, username, password, appName string) *Client {
	return &Client{
		baseURL:  strings.TrimRight(baseURL, "/"),
		username: username,
		password: password,
		appName:  appName,
		http:     &http.Client{},
	}
}

// AppName returns the Stasis application name this client's dialplan
// entries should route into, and that the event WebSocket subscribes to.
func (c *Client) AppName() string {
	return c.appName
}

// AnswerChannel answers an inbound channel that is in the Stasis app.
func (c *Client) AnswerChannel(ctx context.Context, channelID string) error {
	_, err := c.do(ctx, http.MethodPost, fmt.Sprintf("/channels/%s/answer", channelID), nil)
	return err
}

// HangupChannel terminates a channel, optionally with a reason
// (e.g. "normal", "busy", "congestion").
func (c *Client) HangupChannel(ctx context.Context, channelID, reason string) error {
	q := url.Values{}
	if reason != "" {
		q.Set("reason", reason)
	}
	_, err := c.do(ctx, http.MethodDelete, fmt.Sprintf("/channels/%s?%s", channelID, q.Encode()), nil)
	return err
}

// Play starts media playback (e.g. "sound:hello-world") on a channel and
// returns the playback ID.
func (c *Client) Play(ctx context.Context, channelID, media string) (string, error) {
	q := url.Values{}
	q.Set("media", media)
	body, err := c.do(ctx, http.MethodPost, fmt.Sprintf("/channels/%s/play?%s", channelID, q.Encode()), nil)
	if err != nil {
		return "", err
	}
	var resp struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return "", fmt.Errorf("ari: decode play response: %w", err)
	}
	return resp.ID, nil
}

// Originate places an outbound call and puts it into this client's Stasis
// app once answered. endpoint is e.g. "PJSIP/1000".
func (c *Client) Originate(ctx context.Context, endpoint, callerID string) (string, error) {
	q := url.Values{}
	q.Set("endpoint", endpoint)
	q.Set("app", c.appName)
	if callerID != "" {
		q.Set("callerId", callerID)
	}
	body, err := c.do(ctx, http.MethodPost, "/channels?"+q.Encode(), nil)
	if err != nil {
		return "", err
	}
	var resp struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return "", fmt.Errorf("ari: decode originate response: %w", err)
	}
	return resp.ID, nil
}

// do performs an authenticated REST call against the ARI base URL and
// returns the raw response body. A non-2xx status is returned as an error
// including the response body for diagnostics.
func (c *Client) do(ctx context.Context, method, path string, payload io.Reader) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, payload)
	if err != nil {
		return nil, fmt.Errorf("ari: build request: %w", err)
	}
	req.SetBasicAuth(c.username, c.password)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("ari: request %s %s: %w", method, path, err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("ari: read response: %w", err)
	}
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("ari: %s %s returned %d: %s", method, path, resp.StatusCode, bytes.TrimSpace(body))
	}
	return body, nil
}
