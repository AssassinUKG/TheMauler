package nanocyber

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

const clientFixtureToken = "fixture-token-with-at-least-thirty-two-bytes"

func clientFixtureRequest(now time.Time) LeaseRequest {
	return NewOfflineCodeLease(OfflineCodeInput{
		ParentRunID: "run-42/generation-3", SubagentID: "subagent-7", Role: "Test/Fix",
		EngagementID: "eng_01ARZ3NDEKTSV4RRFFQ69G5FAV",
		AllowedTools: []string{"read_file", "write_file", "run_typed_job"},
		MaxTurns:     6, MaxToolCalls: 12, MaxJobs: 4, MaxOutputBytes: 4 * 1024 * 1024,
		EvidenceRequired: []string{"command_receipt", "test_result"}, TTL: 30 * time.Minute,
	}, now)
}

func TestClientAdmitsAttemptBoundLease(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	request := clientFixtureRequest(now)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, incoming *http.Request) {
		if incoming.URL.Path != "/v1/subagent/leases" || incoming.Method != http.MethodPost {
			t.Errorf("unexpected request: %s %s", incoming.Method, incoming.URL.Path)
		}
		if incoming.Header.Get("Authorization") != "Bearer "+clientFixtureToken {
			t.Error("missing bearer token")
		}
		var received LeaseRequest
		if err := json.NewDecoder(incoming.Body).Decode(&received); err != nil {
			t.Error(err)
		}
		_, digest, _ := EncodeLeaseRequest(received)
		writer.Header().Set("Content-Type", "application/json")
		writer.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(writer).Encode(Lease{
			SchemaVersion: 1, LeaseID: "lease_01ARZ3NDEKTSV4RRFFQ69G5FAV",
			EnvironmentID: "env_01ARZ3NDEKTSV4RRFFQ69G5FAV", ManifestRevision: 1,
			RequestSHA256: digest, IssuedAt: now, ExpiresAt: request.ExpiresAt,
		})
	}))
	defer server.Close()
	client, err := NewClient(server.URL, clientFixtureToken)
	if err != nil {
		t.Fatal(err)
	}
	client.now = func() time.Time { return now }
	lease, err := client.Admit(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if lease.ManifestRevision != 1 || lease.RequestSHA256 == "" {
		t.Fatalf("unexpected lease: %#v", lease)
	}
}

func TestClientReturnsClassifiedAdmissionProblem(t *testing.T) {
	now := time.Now().UTC()
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		writer.WriteHeader(http.StatusConflict)
		_, _ = writer.Write([]byte(`{"error":{"code":"scope_conflict","message":"engagement mismatch","retryable":false}}`))
	}))
	defer server.Close()
	client, _ := NewClient(server.URL, clientFixtureToken)
	client.now = func() time.Time { return now }
	_, err := client.Admit(context.Background(), clientFixtureRequest(now))
	failure, ok := err.(*APIError)
	if !ok || failure.Problem.Code != "scope_conflict" || failure.Problem.Retryable {
		t.Fatalf("unexpected error: %#v", err)
	}
}

func TestClientRejectsMismatchedReceiptAndRedirect(t *testing.T) {
	now := time.Now().UTC()
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, incoming *http.Request) {
		if incoming.URL.Path == "/redirect" {
			http.Redirect(writer, incoming, "/elsewhere", http.StatusFound)
			return
		}
		writer.Header().Set("Content-Type", "application/json")
		writer.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(writer).Encode(Lease{
			SchemaVersion: 1, LeaseID: "lease_01ARZ3NDEKTSV4RRFFQ69G5FAV",
			EnvironmentID: "env_01ARZ3NDEKTSV4RRFFQ69G5FAV", ManifestRevision: 1,
			RequestSHA256: strings.Repeat("0", 64), IssuedAt: now, ExpiresAt: now.Add(time.Minute),
		})
	}))
	defer server.Close()
	client, _ := NewClient(server.URL, clientFixtureToken)
	client.now = func() time.Time { return now }
	if _, err := client.Admit(context.Background(), clientFixtureRequest(now)); err == nil || !strings.Contains(err.Error(), "digest mismatch") {
		t.Fatalf("mismatched receipt accepted: %v", err)
	}

	httpClient, _, err := NewLoopbackHTTPClient(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := httpClient.Get(server.URL + "/redirect"); err == nil || !strings.Contains(err.Error(), "redirects") {
		t.Fatalf("redirect accepted: %v", err)
	}
}

func TestClientLifecycleAcknowledgements(t *testing.T) {
	leaseID := "lease_01ARZ3NDEKTSV4RRFFQ69G5FAV"
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, incoming *http.Request) {
		if incoming.Header.Get("Authorization") != "Bearer "+clientFixtureToken {
			t.Error("missing bearer token")
		}
		writer.Header().Set("Content-Type", "application/json")
		switch {
		case incoming.Method == http.MethodDelete && incoming.URL.Path == "/v1/subagent/leases/"+leaseID:
			_ = json.NewEncoder(writer).Encode(map[string]any{"lease_id": leaseID, "accepted": true})
		case incoming.Method == http.MethodPost && incoming.URL.Path == "/v1/subagent/leases/"+leaseID+"/complete":
			_ = json.NewEncoder(writer).Encode(map[string]any{"lease_id": leaseID, "accepted": true})
		case incoming.Method == http.MethodPost && incoming.URL.Path == "/v1/subagent/cancellations":
			var request struct {
				Caller      string `json:"caller"`
				ParentRunID string `json:"parent_run_id"`
			}
			if err := json.NewDecoder(incoming.Body).Decode(&request); err != nil || request.Caller != "themauler" || request.ParentRunID != "run-42/generation-3" {
				t.Errorf("unexpected parent cancellation: %#v err=%v", request, err)
			}
			_ = json.NewEncoder(writer).Encode(map[string]any{"cancelled": 2})
		default:
			http.Error(writer, "unexpected", http.StatusNotFound)
		}
	}))
	defer server.Close()
	client, err := NewClient(server.URL, clientFixtureToken)
	if err != nil {
		t.Fatal(err)
	}
	if err := client.CancelLease(context.Background(), leaseID); err != nil {
		t.Fatal(err)
	}
	if err := client.CompleteLease(context.Background(), leaseID); err != nil {
		t.Fatal(err)
	}
	if cancelled, err := client.CancelParent(context.Background(), "run-42/generation-3"); err != nil || cancelled != 2 {
		t.Fatalf("unexpected parent cancellation result: cancelled=%d err=%v", cancelled, err)
	}
}

func TestClientRejectsInvalidLifecycleAndExpiredReceipt(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	request := clientFixtureRequest(now)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		writer.WriteHeader(http.StatusCreated)
		_, digest, _ := EncodeLeaseRequest(request)
		_ = json.NewEncoder(writer).Encode(Lease{
			SchemaVersion: 1, LeaseID: "lease_01ARZ3NDEKTSV4RRFFQ69G5FAV",
			EnvironmentID: "env_01ARZ3NDEKTSV4RRFFQ69G5FAV", ManifestRevision: 1,
			RequestSHA256: digest, IssuedAt: now.Add(-2 * time.Minute), ExpiresAt: now.Add(-time.Minute),
		})
	}))
	defer server.Close()
	client, _ := NewClient(server.URL, clientFixtureToken)
	client.now = func() time.Time { return now }
	if _, err := client.Admit(context.Background(), request); err == nil || !strings.Contains(err.Error(), "lifetime") {
		t.Fatalf("expired receipt accepted: %v", err)
	}
	if err := client.CancelLease(context.Background(), "lease-bad"); err == nil {
		t.Fatal("invalid lease lifecycle identity accepted")
	}
	if _, err := client.CancelParent(context.Background(), "bad parent with spaces"); err == nil {
		t.Fatal("invalid parent cancellation identity accepted")
	}
}

func TestClientRejectsMismatchedLifecycleAcknowledgement(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(writer).Encode(map[string]any{
			"lease_id": "lease_01ARZ3NDEKTSV4RRFFQ69G5FB0", "accepted": true,
		})
	}))
	defer server.Close()
	client, err := NewClient(server.URL, clientFixtureToken)
	if err != nil {
		t.Fatal(err)
	}
	if err := client.CancelLease(context.Background(), "lease_01ARZ3NDEKTSV4RRFFQ69G5FAV"); err == nil || !strings.Contains(err.Error(), "acknowledgement") {
		t.Fatalf("mismatched lifecycle acknowledgement accepted: %v", err)
	}
}
