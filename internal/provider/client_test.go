package provider

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

const testClientExternalID = "05ac28bb-968f-40a6-bf17-cd0347163ed8"

func readJSON(t *testing.T, r *http.Request) map[string]any {
	t.Helper()
	body, err := io.ReadAll(r.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	if len(body) == 0 {
		return nil
	}
	var value map[string]any
	if err := json.Unmarshal(body, &value); err != nil {
		t.Fatalf("unmarshal body: %v", err)
	}
	return value
}

func requireAPIKey(t *testing.T, r *http.Request) {
	t.Helper()
	if got := r.Header.Get("X-Api-Key"); got != "test-key" {
		t.Fatalf("expected X-Api-Key=test-key, got %q", got)
	}
}

func TestUpsertAWSAccountUsesScopePathAndReturnsNoInternalID(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requireAPIKey(t, r)
		if r.Method != http.MethodPut || r.URL.Path != "/v1/platform/scopes/aws/123456789012" {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		body := readJSON(t, r)
		if body["account_id"] != "123456789012" {
			t.Fatalf("expected account_id, got %v", body["account_id"])
		}
		if body["iam_role"] != "arn:aws:iam::123456789012:role/xiac-readonly" {
			t.Fatalf("expected iam_role, got %v", body["iam_role"])
		}
		if body["external_id"] != testClientExternalID {
			t.Fatalf("expected external_id, got %v", body["external_id"])
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"kind":        "aws",
			"scope_id":    "123456789012",
			"account_id":  "123456789012",
			"status":      "pending",
			"external_id": testClientExternalID,
		})
	}))
	defer srv.Close()

	client := NewClient(srv.URL, "test-key")
	scope, err := client.UpsertAWSAccount(context.Background(), AWSAccountScope{
		AccountID:  "123456789012",
		IAMRole:    "arn:aws:iam::123456789012:role/xiac-readonly",
		ExternalID: testClientExternalID,
		STSRegion:  "eu-west-1",
		Regions:    []string{"eu-west-1"},
		ReadOnly:   true,
	})
	if err != nil {
		t.Fatalf("UpsertAWSAccount: %v", err)
	}
	if scope.AccountID != "123456789012" || scope.Status != "pending" {
		t.Fatalf("unexpected scope: %#v", scope)
	}
}

func TestConnectAWSAccountUsesScopeIdentity(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requireAPIKey(t, r)
		if r.Method != http.MethodPost || r.URL.Path != "/v1/platform/scopes/aws/123456789012/connect" {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"kind":       "aws",
			"scope_id":   "123456789012",
			"account_id": "123456789012",
			"status":     "connected",
			"connected":  true,
			"read_only":  true,
		})
	}))
	defer srv.Close()

	client := NewClient(srv.URL, "test-key")
	scope, err := client.ConnectAWSAccount(context.Background(), "123456789012")
	if err != nil {
		t.Fatalf("ConnectAWSAccount: %v", err)
	}
	if !scope.Connected || !scope.ReadOnly || scope.Status != "connected" {
		t.Fatalf("unexpected connected scope: %#v", scope)
	}
}

func TestGetAWSAccountFoundAndNotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requireAPIKey(t, r)
		if r.URL.Path == "/v1/platform/scopes/aws/missing" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"kind":        "aws",
			"scope_id":    "123456789012",
			"account_id":  "123456789012",
			"status":      "connected",
			"external_id": testClientExternalID,
		})
	}))
	defer srv.Close()

	client := NewClient(srv.URL, "test-key")
	scope, found, err := client.GetAWSAccount(context.Background(), "123456789012")
	if err != nil || !found || scope.AccountID != "123456789012" {
		t.Fatalf("found=%v scope=%#v err=%v", found, scope, err)
	}
	_, found, err = client.GetAWSAccount(context.Background(), "missing")
	if err != nil || found {
		t.Fatalf("missing found=%v err=%v", found, err)
	}
}

func TestDeleteAWSAccountIsIdempotent(t *testing.T) {
	for _, status := range []int{http.StatusNoContent, http.StatusNotFound} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requireAPIKey(t, r)
				if r.Method != http.MethodDelete || r.URL.Path != "/v1/platform/scopes/aws/123456789012" {
					t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
				}
				w.WriteHeader(status)
			}))
			defer srv.Close()

			client := NewClient(srv.URL, "test-key")
			if err := client.DeleteAWSAccount(context.Background(), "123456789012"); err != nil {
				t.Fatalf("DeleteAWSAccount: %v", err)
			}
		})
	}
}
