package worker

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"video-generator/services/api/internal/model"
)

type Client struct {
	baseURL string
	http    *http.Client
	upload  *http.Client
}

func New(baseURL string) *Client {
	return &Client{
		baseURL: strings.TrimRight(baseURL, "/"),
		http:    &http.Client{Timeout: 15 * time.Second},
		upload:  &http.Client{Timeout: 10 * time.Minute},
	}
}

func (c *Client) Create(ctx context.Context, request model.GenerationRequest) (model.WorkerJob, error) {
	return c.do(ctx, http.MethodPost, "/v1/jobs", request)
}

func (c *Client) Get(ctx context.Context, id string) (model.WorkerJob, error) {
	return c.do(ctx, http.MethodGet, "/v1/jobs/"+id, nil)
}

func (c *Client) Cancel(ctx context.Context, id string) (model.WorkerJob, error) {
	return c.do(ctx, http.MethodPost, "/v1/jobs/"+id+"/cancel", struct{}{})
}

func (c *Client) UploadInput(ctx context.Context, model, contentType string, contentLength int64, body io.Reader) (string, error) {
	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		c.baseURL+"/v1/assets/input?model="+url.QueryEscape(model),
		body,
	)
	if err != nil {
		return "", err
	}
	req.ContentLength = contentLength
	req.Header.Set("Content-Type", contentType)
	resp, err := c.upload.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		message, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return "", fmt.Errorf("worker returned %d: %s", resp.StatusCode, strings.TrimSpace(string(message)))
	}
	var result struct {
		Asset struct {
			AssetID string `json:"assetId"`
		} `json:"asset"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", err
	}
	if result.Asset.AssetID == "" {
		return "", errors.New("worker response did not include assetId")
	}
	return result.Asset.AssetID, nil
}

func (c *Client) do(ctx context.Context, method, path string, body any) (model.WorkerJob, error) {
	var reader io.Reader
	if body != nil {
		payload, err := json.Marshal(body)
		if err != nil {
			return model.WorkerJob{}, err
		}
		reader = bytes.NewReader(payload)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, reader)
	if err != nil {
		return model.WorkerJob{}, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return model.WorkerJob{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		message, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return model.WorkerJob{}, fmt.Errorf("worker returned %d: %s", resp.StatusCode, strings.TrimSpace(string(message)))
	}
	var job model.WorkerJob
	if err := json.NewDecoder(resp.Body).Decode(&job); err != nil {
		return model.WorkerJob{}, err
	}
	return job, nil
}
