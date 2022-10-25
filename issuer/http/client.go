package http

import (
	"bytes"
	"context"
	"io"
	"net/http"

	"github.com/go-chi/chi/middleware"
	"github.com/pkg/errors"
)

// Client represents default http client that can be used to send requests to third party services
type Client struct {
	base http.Client
}

// NewClient returns new instance of custom client
func NewClient(c http.Client) *Client {
	return &Client{
		base: c,
	}
}

// Post send posts request to url with additional headers
func (c *Client) Post(ctx context.Context, url string, req []byte) ([]byte, error) {
	reqBody := bytes.NewBuffer(req)

	request, err := http.NewRequest(http.MethodPost, url, reqBody)
	if err != nil {
		return nil, err
	}

	addRequestIDToHeader(ctx, request)

	return executeRequest(c, request)
}

// Get send request to url with requestID headers
func (c *Client) Get(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url,
		http.NoBody)
	if err != nil {
		return nil, err
	}

	addRequestIDToHeader(ctx, req)

	return executeRequest(c, req)
}

// addRequestIDToHeader adds headers to request
func addRequestIDToHeader(ctx context.Context, r *http.Request) {
	requestID := middleware.GetReqID(ctx)

	r.Header.Add("Content-Type", "application/json")
	r.Header.Add(middleware.RequestIDHeader, requestID)
}

// executeRequest contains utils logic of request execution
func executeRequest(c *Client, r *http.Request) ([]byte, error) {
	resp, err := c.base.Do(r)
	if err != nil {
		return nil, err
	}

	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, errors.Errorf("http request failed with status %v, error: %v", resp.StatusCode, string(body))
	}

	return body, nil
}
