package provider

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

// Client is the tenant-authenticated REST client used by Terraform resources.
type Client struct {
	Endpoint string
	APIKey   string
	HTTP     *http.Client
}

func NewClient(endpoint, apiKey string) *Client {
	return &Client{
		Endpoint: strings.TrimRight(endpoint, "/"),
		APIKey:   apiKey,
		HTTP:     http.DefaultClient,
	}
}

func (c *Client) do(ctx context.Context, method, path string, body any) ([]byte, int, error) {
	var reader io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return nil, 0, fmt.Errorf("marshal request: %w", err)
		}
		reader = bytes.NewReader(encoded)
	}

	request, err := http.NewRequestWithContext(ctx, method, c.Endpoint+path, reader)
	if err != nil {
		return nil, 0, fmt.Errorf("build request: %w", err)
	}
	request.Header.Set("X-Api-Key", c.APIKey)
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}

	httpClient := c.HTTP
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	response, err := httpClient.Do(request)
	if err != nil {
		return nil, 0, fmt.Errorf("%s %s: %w", method, path, err)
	}
	defer response.Body.Close()

	responseBody, err := io.ReadAll(response.Body)
	if err != nil {
		return nil, response.StatusCode, fmt.Errorf("read response: %w", err)
	}
	return responseBody, response.StatusCode, nil
}

func httpError(method, path string, status int, body []byte) error {
	snippet := strings.TrimSpace(string(body))
	if len(snippet) > 512 {
		snippet = snippet[:512]
	}
	return fmt.Errorf("%s %s: unexpected status %d: %s", method, path, status, snippet)
}

func is2xx(status int) bool { return status >= 200 && status < 300 }

func scopePath(kind, scopeID string) string {
	return "/v1/platform/scopes/" + url.PathEscape(kind) + "/" + url.PathEscape(scopeID)
}

// AWSAccountScope is the public AWS-native representation. It deliberately
// contains no platform database ID.
type AWSAccountScope struct {
	Kind         string   `json:"kind"`
	ScopeID      string   `json:"scope_id"`
	AccountID    string   `json:"account_id"`
	IAMRole      string   `json:"iam_role"`
	Regions      []string `json:"regions"`
	STSRegion    string   `json:"sts_region"`
	ReadOnly     bool     `json:"read_only"`
	ExternalID   string   `json:"external_id"`
	Status       string   `json:"status"`
	StatusDetail string   `json:"status_detail"`
	Connected    bool     `json:"connected"`
}

func decodeAWSAccountScope(operation string, body []byte) (AWSAccountScope, error) {
	var scope AWSAccountScope
	if err := json.Unmarshal(body, &scope); err != nil {
		return AWSAccountScope{}, fmt.Errorf("decode %s response: %w", operation, err)
	}
	if scope.AccountID == "" {
		scope.AccountID = scope.ScopeID
	}
	if scope.ScopeID == "" {
		scope.ScopeID = scope.AccountID
	}
	if scope.Regions == nil {
		scope.Regions = []string{}
	}
	return scope, nil
}

// UpsertAWSAccount creates or updates the authenticated tenant's AWS scope.
func (c *Client) UpsertAWSAccount(ctx context.Context, scope AWSAccountScope) (AWSAccountScope, error) {
	path := scopePath("aws", scope.AccountID)
	regions := scope.Regions
	if regions == nil {
		regions = []string{}
	}
	payload := map[string]any{
		"account_id":  scope.AccountID,
		"iam_role":    scope.IAMRole,
		"regions":     regions,
		"sts_region":  scope.STSRegion,
		"readonly":    scope.ReadOnly,
		"external_id": scope.ExternalID,
	}
	body, status, err := c.do(ctx, http.MethodPut, path, payload)
	if err != nil {
		return AWSAccountScope{}, err
	}
	if !is2xx(status) {
		return AWSAccountScope{}, httpError(http.MethodPut, path, status, body)
	}
	return decodeAWSAccountScope("upsert AWS account", body)
}

// ConnectAWSAccount asks XIaC to assume and verify the registered role.
func (c *Client) ConnectAWSAccount(ctx context.Context, accountID string) (AWSAccountScope, error) {
	path := scopePath("aws", accountID) + "/connect"
	body, status, err := c.do(ctx, http.MethodPost, path, nil)
	if err != nil {
		return AWSAccountScope{}, err
	}
	if !is2xx(status) {
		return AWSAccountScope{}, httpError(http.MethodPost, path, status, body)
	}
	return decodeAWSAccountScope("connect AWS account", body)
}

// GetAWSAccount reads current scope state. A 404 is drift, not a client error.
func (c *Client) GetAWSAccount(ctx context.Context, accountID string) (AWSAccountScope, bool, error) {
	path := scopePath("aws", accountID)
	body, status, err := c.do(ctx, http.MethodGet, path, nil)
	if err != nil {
		return AWSAccountScope{}, false, err
	}
	if status == http.StatusNotFound {
		return AWSAccountScope{}, false, nil
	}
	if !is2xx(status) {
		return AWSAccountScope{}, false, httpError(http.MethodGet, path, status, body)
	}
	scope, err := decodeAWSAccountScope("get AWS account", body)
	return scope, true, err
}

// DeleteAWSAccount removes a tenant/account scope. It is idempotent.
func (c *Client) DeleteAWSAccount(ctx context.Context, accountID string) error {
	path := scopePath("aws", accountID)
	body, status, err := c.do(ctx, http.MethodDelete, path, nil)
	if err != nil {
		return err
	}
	if status == http.StatusNotFound || is2xx(status) {
		return nil
	}
	return httpError(http.MethodDelete, path, status, body)
}
