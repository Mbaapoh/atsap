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
	"strconv"
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

// OriginateRequest carries what's needed to place one outbound leg.
// Endpoint is e.g. "PJSIP/1000". ChannelID and Variables implement the
// D-19 correlation strategy: supplying our own channel ID and a custom
// variable at origination time makes correlating a later ARI event back
// to the right Participant exact rather than a guess under concurrency
// (docs/hld/01-architecture.md §2.1).
type OriginateRequest struct {
	Endpoint  string
	CallerID  string
	ChannelID string
	Variables map[string]string
	// TimeoutSeconds bounds how long Asterisk alerts before giving up
	// (PRD §11.1 Presenting). Zero means Asterisk's own default.
	TimeoutSeconds int
}

// Originate places an outbound call and puts it into this client's
// Stasis app once answered.
func (c *Client) Originate(ctx context.Context, req OriginateRequest) (string, error) {
	q := url.Values{}
	q.Set("endpoint", req.Endpoint)
	q.Set("app", c.appName)
	if req.CallerID != "" {
		q.Set("callerId", req.CallerID)
	}
	if req.ChannelID != "" {
		q.Set("channelId", req.ChannelID)
	}
	if req.TimeoutSeconds > 0 {
		q.Set("timeout", strconv.Itoa(req.TimeoutSeconds))
	}

	var payload io.Reader
	if len(req.Variables) > 0 {
		encoded, err := json.Marshal(struct {
			Variables map[string]string `json:"variables"`
		}{Variables: req.Variables})
		if err != nil {
			return "", fmt.Errorf("ari: encode originate variables: %w", err)
		}
		payload = bytes.NewReader(encoded)
	}

	body, err := c.do(ctx, http.MethodPost, "/channels?"+q.Encode(), payload)
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

// CreateBridge creates a mixing bridge of the given type (e.g. "mixing")
// and returns its ID.
func (c *Client) CreateBridge(ctx context.Context, bridgeType string) (string, error) {
	q := url.Values{}
	if bridgeType != "" {
		q.Set("type", bridgeType)
	}
	body, err := c.do(ctx, http.MethodPost, "/bridges?"+q.Encode(), nil)
	if err != nil {
		return "", err
	}
	var resp struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return "", fmt.Errorf("ari: decode create bridge response: %w", err)
	}
	return resp.ID, nil
}

// AddChannelToBridge adds channelID to bridgeID.
func (c *Client) AddChannelToBridge(ctx context.Context, bridgeID, channelID string) error {
	q := url.Values{}
	q.Set("channel", channelID)
	_, err := c.do(ctx, http.MethodPost, fmt.Sprintf("/bridges/%s/addChannel?%s", bridgeID, q.Encode()), nil)
	return err
}

// DestroyBridge tears down bridgeID.
func (c *Client) DestroyBridge(ctx context.Context, bridgeID string) error {
	_, err := c.do(ctx, http.MethodDelete, fmt.Sprintf("/bridges/%s", bridgeID), nil)
	return err
}

// ListChannels returns the IDs of every channel currently known to
// Asterisk, used by callers that need to find a channel by its
// correlation variable rather than by an ID they already hold.
func (c *Client) ListChannels(ctx context.Context) ([]string, error) {
	body, err := c.do(ctx, http.MethodGet, "/channels", nil)
	if err != nil {
		return nil, err
	}
	var resp []struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("ari: decode list channels response: %w", err)
	}
	ids := make([]string, len(resp))
	for i, ch := range resp {
		ids[i] = ch.ID
	}
	return ids, nil
}

// GetChannelVariable reads a channel variable, used to re-derive
// correlation after a brief ARI WebSocket reconnect
// (docs/hld/01-architecture.md §2.1).
func (c *Client) GetChannelVariable(ctx context.Context, channelID, name string) (string, error) {
	q := url.Values{}
	q.Set("variable", name)
	body, err := c.do(ctx, http.MethodGet, fmt.Sprintf("/channels/%s/variable?%s", channelID, q.Encode()), nil)
	if err != nil {
		return "", err
	}
	var resp struct {
		Value string `json:"value"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return "", fmt.Errorf("ari: decode get channel variable response: %w", err)
	}
	return resp.Value, nil
}

// SetChannelVariable sets a channel variable, used to make a fresh
// inbound channel self-describing for correlation
// (docs/hld/01-architecture.md §2.1).
func (c *Client) SetChannelVariable(ctx context.Context, channelID, name, value string) error {
	q := url.Values{}
	q.Set("variable", name)
	q.Set("value", value)
	_, err := c.do(ctx, http.MethodPost, fmt.Sprintf("/channels/%s/variable?%s", channelID, q.Encode()), nil)
	return err
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
