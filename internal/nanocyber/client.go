package nanocyber

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const maxAPIResponseBytes = 64 * 1024

var leaseDigestPattern = regexp.MustCompile(`^[a-f0-9]{64}$`)

var problemCodePattern = regexp.MustCompile(`^[a-z][a-z0-9_]{0,63}$`)

// Lease is the opaque, attempt-bound admission receipt returned by NanoCyber.
// It grants no Docker, shell, or host-filesystem authority by itself.
type Lease struct {
	SchemaVersion    uint32    `json:"schema_version"`
	LeaseID          string    `json:"lease_id"`
	EnvironmentID    string    `json:"environment_id"`
	ManifestRevision uint64    `json:"manifest_revision"`
	RequestSHA256    string    `json:"request_sha256"`
	IssuedAt         time.Time `json:"issued_at"`
	ExpiresAt        time.Time `json:"expires_at"`
}

type Problem struct {
	Code      string `json:"code"`
	Message   string `json:"message"`
	Retryable bool   `json:"retryable"`
}

type APIError struct {
	Status  int
	Problem Problem
}

func (failure *APIError) Error() string {
	if failure == nil {
		return "NanoCyber admission failed"
	}
	message := strings.TrimSpace(failure.Problem.Message)
	if message == "" {
		message = "NanoCyber admission failed"
	}
	return fmt.Sprintf("%s (HTTP %d, code=%s)", message, failure.Status, failure.Problem.Code)
}

// Client is a strict loopback-only lease-admission client. It never follows a
// redirect or uses an ambient HTTP proxy.
type Client struct {
	endpoint   string
	credential string
	httpClient *http.Client
	now        func() time.Time
}

func NewClient(endpoint, credential string) (*Client, error) {
	credential = strings.TrimSpace(credential)
	if len(credential) < 32 || len(credential) > 512 || strings.ContainsAny(credential, "\r\n\x00") {
		return nil, fmt.Errorf("NanoCyber token must contain between 32 and 512 non-control characters")
	}
	httpClient, canonical, err := NewLoopbackHTTPClient(endpoint)
	if err != nil {
		return nil, err
	}
	return &Client{
		endpoint: canonical, credential: credential, httpClient: httpClient,
		now: func() time.Time { return time.Now().UTC() },
	}, nil
}

// NewLoopbackHTTPClient returns a proxy-free client pinned to the exact
// configured loopback host and port. The transport ignores request-supplied
// destinations, preventing callers from widening the authority later.
func NewLoopbackHTTPClient(endpoint string) (*http.Client, string, error) {
	parsed, err := url.Parse(strings.TrimSpace(endpoint))
	if err != nil || !parsed.IsAbs() || parsed.Scheme != "http" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || (parsed.Path != "" && parsed.Path != "/") {
		return nil, "", fmt.Errorf("NanoCyber endpoint must be an absolute loopback HTTP origin")
	}
	host, port := strings.ToLower(strings.TrimSpace(parsed.Hostname())), parsed.Port()
	portNumber, portErr := strconv.Atoi(port)
	if portErr != nil || portNumber < 1 || portNumber > 65535 {
		return nil, "", fmt.Errorf("NanoCyber endpoint must include an explicit TCP port")
	}
	if host != "localhost" {
		ip := net.ParseIP(host)
		if ip == nil || !ip.IsLoopback() {
			return nil, "", fmt.Errorf("NanoCyber endpoint must use localhost or a loopback IP")
		}
	}
	canonical := strings.TrimSuffix(parsed.String(), "/")
	transport := &http.Transport{
		Proxy:             nil,
		DisableKeepAlives: true,
		DialContext: func(ctx context.Context, network, _ string) (net.Conn, error) {
			addresses, err := net.DefaultResolver.LookupIPAddr(ctx, host)
			if err != nil {
				return nil, fmt.Errorf("resolve NanoCyber loopback endpoint: %w", err)
			}
			if len(addresses) == 0 {
				return nil, fmt.Errorf("NanoCyber loopback endpoint resolved to no addresses")
			}
			for _, address := range addresses {
				if !address.IP.IsLoopback() {
					return nil, fmt.Errorf("NanoCyber endpoint resolved outside loopback")
				}
			}
			dialer := net.Dialer{Timeout: 2 * time.Second}
			var lastErr error
			for _, address := range addresses {
				connection, dialErr := dialer.DialContext(ctx, network, net.JoinHostPort(address.IP.String(), port))
				if dialErr == nil {
					return connection, nil
				}
				lastErr = dialErr
			}
			return nil, lastErr
		},
	}
	return &http.Client{
		Transport: transport,
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return fmt.Errorf("NanoCyber redirects are not allowed")
		},
	}, canonical, nil
}

func (client *Client) Admit(ctx context.Context, request LeaseRequest) (Lease, error) {
	if client == nil || client.httpClient == nil {
		return Lease{}, fmt.Errorf("NanoCyber client is not configured")
	}
	now := client.now().UTC()
	if err := request.Validate(now); err != nil {
		return Lease{}, err
	}
	encoded, digest, err := EncodeLeaseRequest(request)
	if err != nil {
		return Lease{}, err
	}
	httpRequest, err := http.NewRequestWithContext(ctx, http.MethodPost, client.endpoint+"/v1/subagent/leases", bytes.NewReader(encoded))
	if err != nil {
		return Lease{}, err
	}
	httpRequest.Header.Set("Accept", "application/json")
	httpRequest.Header.Set("Content-Type", "application/json")
	httpRequest.Header.Set("Authorization", "Bearer "+client.credential)
	response, data, err := client.do(httpRequest)
	if err != nil {
		return Lease{}, err
	}
	if response.StatusCode != http.StatusCreated {
		return Lease{}, decodeAPIError(response.StatusCode, data)
	}
	var lease Lease
	if err := decodeStrictJSON(data, &lease); err != nil {
		return Lease{}, fmt.Errorf("decode NanoCyber lease: %w", err)
	}
	if err := lease.Validate(request, digest, now); err != nil {
		return Lease{}, err
	}
	return lease, nil
}

// CancelLease requests cancellation and waits for NanoCyber to confirm that
// cleanup reached a terminal state. An ambiguous transport failure is not a
// cleanup receipt and must not be treated as success by callers.
func (client *Client) CancelLease(ctx context.Context, leaseID string) error {
	return client.finishLease(ctx, leaseID, http.MethodDelete, "")
}

// CompleteLease marks the attempt complete and waits for NanoCyber's cleanup
// confirmation before accepting the lifecycle transition.
func (client *Client) CompleteLease(ctx context.Context, leaseID string) error {
	return client.finishLease(ctx, leaseID, http.MethodPost, "/complete")
}

func (client *Client) finishLease(ctx context.Context, leaseID, method, suffix string) error {
	if client == nil || client.httpClient == nil {
		return fmt.Errorf("NanoCyber client is not configured")
	}
	if !nanoIDPattern.MatchString(leaseID) || !strings.HasPrefix(leaseID, "lease_") {
		return fmt.Errorf("NanoCyber lifecycle requires a valid lease ID")
	}
	httpRequest, err := http.NewRequestWithContext(ctx, method, client.endpoint+"/v1/subagent/leases/"+leaseID+suffix, nil)
	if err != nil {
		return err
	}
	response, data, err := client.do(httpRequest)
	if err != nil {
		return err
	}
	if response.StatusCode != http.StatusOK {
		return decodeAPIError(response.StatusCode, data)
	}
	var acknowledgement struct {
		LeaseID  string `json:"lease_id"`
		Accepted bool   `json:"accepted"`
	}
	if err := decodeStrictJSON(data, &acknowledgement); err != nil || acknowledgement.LeaseID != leaseID || !acknowledgement.Accepted {
		return fmt.Errorf("NanoCyber returned an invalid lifecycle acknowledgement")
	}
	return nil
}

// CancelParent cancels queued and active leases owned by one exact Mauler
// parent-run identity. It returns only NanoCyber's bounded cancellation count.
func (client *Client) CancelParent(ctx context.Context, parentRunID string) (int, error) {
	if client == nil || client.httpClient == nil {
		return 0, fmt.Errorf("NanoCyber client is not configured")
	}
	if !labelPattern.MatchString(parentRunID) {
		return 0, fmt.Errorf("NanoCyber parent cancellation requires a bounded parent run ID")
	}
	body, err := json.Marshal(struct {
		Caller      string `json:"caller"`
		ParentRunID string `json:"parent_run_id"`
	}{Caller: "themauler", ParentRunID: parentRunID})
	if err != nil {
		return 0, err
	}
	httpRequest, err := http.NewRequestWithContext(ctx, http.MethodPost, client.endpoint+"/v1/subagent/cancellations", bytes.NewReader(body))
	if err != nil {
		return 0, err
	}
	httpRequest.Header.Set("Content-Type", "application/json")
	response, data, err := client.do(httpRequest)
	if err != nil {
		return 0, err
	}
	if response.StatusCode != http.StatusOK {
		return 0, decodeAPIError(response.StatusCode, data)
	}
	var acknowledgement struct {
		Cancelled int `json:"cancelled"`
	}
	if err := decodeStrictJSON(data, &acknowledgement); err != nil || acknowledgement.Cancelled < 0 {
		return 0, fmt.Errorf("NanoCyber returned an invalid parent-cancellation acknowledgement")
	}
	return acknowledgement.Cancelled, nil
}

func (client *Client) do(request *http.Request) (*http.Response, []byte, error) {
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Authorization", "Bearer "+client.credential)
	response, err := client.httpClient.Do(request)
	if err != nil {
		return nil, nil, err
	}
	defer response.Body.Close()
	data, err := readBoundedJSON(response)
	return response, data, err
}

func decodeAPIError(status int, data []byte) error {
	var payload struct {
		Error Problem `json:"error"`
	}
	if err := decodeStrictJSON(data, &payload); err != nil || !problemCodePattern.MatchString(payload.Error.Code) ||
		strings.TrimSpace(payload.Error.Message) == "" || len(payload.Error.Message) > 512 || strings.ContainsAny(payload.Error.Message, "\r\n\x00") {
		return fmt.Errorf("NanoCyber returned HTTP %d with an invalid error body", status)
	}
	return &APIError{Status: status, Problem: payload.Error}
}

func decodeStrictJSON(data []byte, output any) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(output); err != nil {
		return err
	}
	if decoder.More() {
		return fmt.Errorf("response contains trailing JSON values")
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			return fmt.Errorf("response contains trailing JSON values")
		}
		return err
	}
	return nil
}

func EncodeLeaseRequest(request LeaseRequest) ([]byte, string, error) {
	encoded, err := json.Marshal(request)
	if err != nil {
		return nil, "", fmt.Errorf("encode NanoCyber lease request: %w", err)
	}
	digestBytes := sha256.Sum256(encoded)
	return encoded, hex.EncodeToString(digestBytes[:]), nil
}

func (lease Lease) Validate(request LeaseRequest, digest string, now time.Time) error {
	if lease.SchemaVersion != 1 || !nanoIDPattern.MatchString(lease.LeaseID) || !strings.HasPrefix(lease.LeaseID, "lease_") || !nanoIDPattern.MatchString(lease.EnvironmentID) || !strings.HasPrefix(lease.EnvironmentID, "env_") {
		return fmt.Errorf("NanoCyber returned an invalid lease identity")
	}
	if lease.ManifestRevision == 0 {
		return fmt.Errorf("NanoCyber returned an invalid manifest revision")
	}
	if !leaseDigestPattern.MatchString(lease.RequestSHA256) || lease.RequestSHA256 != digest {
		return fmt.Errorf("NanoCyber lease request digest mismatch")
	}
	if lease.IssuedAt.IsZero() || lease.IssuedAt.Before(now.Add(-time.Minute)) || lease.IssuedAt.After(now.Add(time.Minute)) ||
		!lease.ExpiresAt.After(now) || !lease.ExpiresAt.After(lease.IssuedAt) || lease.ExpiresAt.After(request.ExpiresAt) {
		return fmt.Errorf("NanoCyber returned an invalid lease lifetime")
	}
	return nil
}

func readBoundedJSON(response *http.Response) ([]byte, error) {
	mediaType, _, err := mime.ParseMediaType(response.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		return nil, fmt.Errorf("NanoCyber returned a non-JSON response")
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, maxAPIResponseBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxAPIResponseBytes {
		return nil, fmt.Errorf("NanoCyber response exceeded the size limit")
	}
	return data, nil
}
