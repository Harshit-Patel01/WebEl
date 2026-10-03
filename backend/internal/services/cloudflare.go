package services

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

const cloudflareAPIBase = "https://api.cloudflare.com/client/v4"

type CloudflareAPI struct {
	token  string
	client *http.Client
}

func NewCloudflareAPI(token string) *CloudflareAPI {
	return &CloudflareAPI{
		token: token,
		client: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// API Response structures
type cfResponse struct {
	Success  bool            `json:"success"`
	Errors   []cfError       `json:"errors"`
	Messages []interface{}   `json:"messages"`
	Result   json.RawMessage `json:"result"`
}

type cfError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// Token verification
type TokenVerifyResult struct {
	Status    string     `json:"status"`
	ExpiresOn *time.Time `json:"expires_on,omitempty"`
	ID        string     `json:"id,omitempty"`
}

func (c *CloudflareAPI) VerifyToken(ctx context.Context) (*TokenVerifyResult, error) {
	resp, err := c.doRequest(ctx, "GET", "/user/tokens/verify", nil)
	if err != nil {
		return nil, err
	}

	var result TokenVerifyResult
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		return nil, fmt.Errorf("failed to parse token verify response: %w", err)
	}

	return &result, nil
}

// Account structures
type Account struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

func (c *CloudflareAPI) ListAccounts(ctx context.Context) ([]Account, error) {
	resp, err := c.doRequest(ctx, "GET", "/accounts", nil)
	if err != nil {
		return nil, err
	}

	var accounts []Account
	if err := json.Unmarshal(resp.Result, &accounts); err != nil {
		return nil, fmt.Errorf("failed to parse accounts: %w", err)
	}

	return accounts, nil
}

// Zone structures
type Zone struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

func (c *CloudflareAPI) ListZones(ctx context.Context) ([]Zone, error) {
	resp, err := c.doRequest(ctx, "GET", "/zones", nil)
	if err != nil {
		return nil, err
	}

	var zones []Zone
	if err := json.Unmarshal(resp.Result, &zones); err != nil {
		return nil, fmt.Errorf("failed to parse zones: %w", err)
	}

	return zones, nil
}

// Tunnel structures
type Tunnel struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Status      string    `json:"status"`
	CreatedAt   time.Time `json:"created_at"`
	Connections []struct {
		ColoName string `json:"colo_name"`
	} `json:"connections"`
}

type TunnelCreateRequest struct {
	Name         string `json:"name"`
	TunnelSecret string `json:"tunnel_secret"`
}

type TunnelCreateResponse struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Token string `json:"token"`
}

func (c *CloudflareAPI) CreateTunnel(ctx context.Context, accountID, name string) (*TunnelCreateResponse, error) {
	// Generate tunnel secret (32 random bytes, base64 encoded)
	secret := make([]byte, 32)
	if _, err := rand.Read(secret); err != nil {
		return nil, fmt.Errorf("failed to generate tunnel secret: %w", err)
	}
	tunnelSecret := base64.StdEncoding.EncodeToString(secret)

	req := TunnelCreateRequest{
		Name:         name,
		TunnelSecret: tunnelSecret,
	}

	resp, err := c.doRequest(ctx, "POST", fmt.Sprintf("/accounts/%s/cfd_tunnel", accountID), req)
	if err != nil {
		return nil, err
	}

	var result TunnelCreateResponse
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		return nil, fmt.Errorf("failed to parse tunnel create response: %w", err)
	}

	return &result, nil
}

func (c *CloudflareAPI) GetTunnel(ctx context.Context, accountID, tunnelID string) (*Tunnel, error) {
	resp, err := c.doRequest(ctx, "GET", fmt.Sprintf("/accounts/%s/cfd_tunnel/%s", accountID, tunnelID), nil)
	if err != nil {
		return nil, err
	}

	var tunnel Tunnel
	if err := json.Unmarshal(resp.Result, &tunnel); err != nil {
		return nil, fmt.Errorf("failed to parse tunnel: %w", err)
	}

	return &tunnel, nil
}

// GetTunnelConfiguration retrieves the ingress configuration from Cloudflare
func (c *CloudflareAPI) GetTunnelConfiguration(ctx context.Context, accountID, tunnelID string) (*TunnelConfiguration, error) {
	resp, err := c.doRequest(ctx, "GET", fmt.Sprintf("/accounts/%s/cfd_tunnel/%s/configurations", accountID, tunnelID), nil)
	if err != nil {
		return nil, err
	}

	var config TunnelConfiguration
	if err := json.Unmarshal(resp.Result, &config); err != nil {
		return nil, fmt.Errorf("failed to parse tunnel configuration: %w", err)
	}

	return &config, nil
}

// TunnelConfiguration represents the tunnel's ingress configuration
type TunnelConfiguration struct {
	Config struct {
		Ingress []IngressRule `json:"ingress"`
	} `json:"config"`
}

type IngressRule struct {
	Hostname string `json:"hostname,omitempty"`
	Service  string `json:"service"`
	Path     string `json:"path,omitempty"`
}

func (c *CloudflareAPI) ListTunnels(ctx context.Context, accountID string) ([]Tunnel, error) {
	resp, err := c.doRequest(ctx, "GET", fmt.Sprintf("/accounts/%s/cfd_tunnel", accountID), nil)
	if err != nil {
		return nil, err
	}

	var tunnels []Tunnel
	if err := json.Unmarshal(resp.Result, &tunnels); err != nil {
		return nil, fmt.Errorf("failed to parse tunnels: %w", err)
	}

	return tunnels, nil
}

func (c *CloudflareAPI) DeleteTunnel(ctx context.Context, accountID, tunnelID string) error {
	_, err := c.doRequest(ctx, "DELETE", fmt.Sprintf("/accounts/%s/cfd_tunnel/%s", accountID, tunnelID), nil)
	return err
}

// DNS structures
type DNSRecord struct {
	ID      string `json:"id"`
	Type    string `json:"type"`
	Name    string `json:"name"`
	Content string `json:"content"`
	Proxied bool   `json:"proxied"`
	TTL     int    `json:"ttl"`
}

type DNSRecordCreateRequest struct {
	Type    string `json:"type"`
	Name    string `json:"name"`
	Content string `json:"content"`
	Proxied bool   `json:"proxied"`
	TTL     int    `json:"ttl"`
}

func (c *CloudflareAPI) CreateDNSRecord(ctx context.Context, zoneID string, req DNSRecordCreateRequest) (*DNSRecord, error) {
	resp, err := c.doRequest(ctx, "POST", fmt.Sprintf("/zones/%s/dns_records", zoneID), req)
	if err != nil {
		return nil, err
	}

	var record DNSRecord
	if err := json.Unmarshal(resp.Result, &record); err != nil {
		return nil, fmt.Errorf("failed to parse DNS record: %w", err)
	}

	return &record, nil
}

func (c *CloudflareAPI) ListDNSRecords(ctx context.Context, zoneID, name string) ([]DNSRecord, error) {
	url := fmt.Sprintf("/zones/%s/dns_records?name=%s", zoneID, name)
	resp, err := c.doRequest(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}

	var records []DNSRecord
	if err := json.Unmarshal(resp.Result, &records); err != nil {
		return nil, fmt.Errorf("failed to parse DNS records: %w", err)
	}

	return records, nil
}

func (c *CloudflareAPI) DeleteDNSRecord(ctx context.Context, zoneID, recordID string) error {
	_, err := c.doRequest(ctx, "DELETE", fmt.Sprintf("/zones/%s/dns_records/%s", zoneID, recordID), nil)
	return err
}

// GetDNSRecord retrieves a specific DNS record by ID
func (c *CloudflareAPI) GetDNSRecord(ctx context.Context, zoneID, recordID string) (*DNSRecord, error) {
	resp, err := c.doRequest(ctx, "GET", fmt.Sprintf("/zones/%s/dns_records/%s", zoneID, recordID), nil)
	if err != nil {
		return nil, err
	}

	var record DNSRecord
	if err := json.Unmarshal(resp.Result, &record); err != nil {
		return nil, fmt.Errorf("failed to parse DNS record: %w", err)
	}

	return &record, nil
}

// HTTP request helper
func (c *CloudflareAPI) doRequest(ctx context.Context, method, path string, body interface{}) (*cfResponse, error) {
	var reqBody io.Reader
	if body != nil {
		jsonData, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal request: %w", err)
		}
		reqBody = bytes.NewReader(jsonData)
	}

	req, err := http.NewRequestWithContext(ctx, method, cloudflareAPIBase+path, reqBody)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	var cfResp cfResponse
	if err := json.Unmarshal(respBody, &cfResp); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w (body: %s)", err, string(respBody))
	}

	if !cfResp.Success {
		if len(cfResp.Errors) > 0 {
			return nil, fmt.Errorf("cloudflare API error: %s (code %d)", cfResp.Errors[0].Message, cfResp.Errors[0].Code)
		}
		return nil, fmt.Errorf("cloudflare API request failed")
	}

	return &cfResp, nil
}
