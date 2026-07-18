package provider

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const testClientExternalID = "05ac28bb-968f-40a6-bf17-cd0347163ed8"

// readJSON decodes the request body into a generic map for assertions.
func readJSON(t *testing.T, r *http.Request) map[string]any {
	t.Helper()
	b, err := io.ReadAll(r.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	if len(b) == 0 {
		return nil
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("unmarshal body: %v", err)
	}
	return m
}

func requireAPIKey(t *testing.T, r *http.Request) {
	t.Helper()
	if got := r.Header.Get("X-Api-Key"); got != "test-key" {
		t.Fatalf("expected X-Api-Key=test-key, got %q", got)
	}
}

func TestCreateProvider(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requireAPIKey(t, r)
		if r.Method != http.MethodPost || r.URL.Path != "/v1/platform/providers" {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		body := readJSON(t, r)
		if body["kind"] != "aws" {
			t.Fatalf("expected kind=aws, got %v", body["kind"])
		}
		if body["external_id"] != testClientExternalID {
			t.Fatalf("expected client external_id, got %v", body["external_id"])
		}
		if body["sts_region"] != "eu-west-1" {
			t.Fatalf("expected sts_region=eu-west-1, got %v", body["sts_region"])
		}
		regions, _ := body["regions"].([]any)
		if len(regions) != 1 || regions[0] != "us-east-1" {
			t.Fatalf("expected regions=[us-east-1], got %v", body["regions"])
		}
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]any{"id": "prov-1", "external_id": testClientExternalID})
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "test-key")
	id, ext, err := c.CreateProvider(context.Background(), []string{"us-east-1"}, "eu-west-1", testClientExternalID)
	if err != nil {
		t.Fatalf("CreateProvider: %v", err)
	}
	if id != "prov-1" || ext != testClientExternalID {
		t.Fatalf("got id=%q ext=%q", id, ext)
	}
}

func TestConnect_HappyPath(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requireAPIKey(t, r)
		if r.Method != http.MethodPost || r.URL.Path != "/v1/platform/providers/prov-1/connect" {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		body := readJSON(t, r)
		if body["role_arn"] != "arn:aws:iam::111122223333:role/xiac-scan" {
			t.Fatalf("unexpected role_arn %v", body["role_arn"])
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"connected":  true,
			"account_id": "123456789012",
			"read_only":  true,
			"detail":     "connected",
		})
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "test-key")
	connected, accountID, detail, err := c.Connect(context.Background(), "prov-1", "arn:aws:iam::111122223333:role/xiac-scan")
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}
	if !connected || accountID != "123456789012" || detail != "connected" {
		t.Fatalf("got connected=%v account=%q detail=%q", connected, accountID, detail)
	}
}

// TestCreateThenConnect_HappyPath exercises the create->connect sequence the
// resource's Create runs.
func TestCreateThenConnect_HappyPath(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/platform/providers", func(w http.ResponseWriter, r *http.Request) {
		requireAPIKey(t, r)
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]any{"id": "prov-9", "external_id": testClientExternalID})
	})
	mux.HandleFunc("/v1/platform/providers/prov-9/connect", func(w http.ResponseWriter, r *http.Request) {
		requireAPIKey(t, r)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"connected":  true,
			"account_id": "999988887777",
			"read_only":  true,
		})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	c := NewClient(srv.URL, "test-key")
	id, ext, err := c.CreateProvider(context.Background(), nil, "us-east-1", testClientExternalID)
	if err != nil {
		t.Fatalf("CreateProvider: %v", err)
	}
	if id != "prov-9" || ext != testClientExternalID {
		t.Fatalf("got id=%q ext=%q", id, ext)
	}
	connected, accountID, _, err := c.Connect(context.Background(), id, "arn:aws:iam::999988887777:role/x")
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}
	if !connected || accountID != "999988887777" {
		t.Fatalf("got connected=%v account=%q", connected, accountID)
	}
}

// TestConnect_Rejected mirrors the platform rejecting a write-capable role:
// connected=false with a detail explaining why. The client surfaces this
// without error (it is a valid 200 response).
func TestConnect_Rejected(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requireAPIKey(t, r)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"connected": false,
			"read_only": false,
			"detail":    "role is not read-only",
		})
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "test-key")
	connected, _, detail, err := c.Connect(context.Background(), "prov-1", "arn:aws:iam::111122223333:role/writer")
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}
	if connected {
		t.Fatal("expected connected=false for a rejected role")
	}
	if detail != "role is not read-only" {
		t.Fatalf("expected rejection detail, got %q", detail)
	}
}

func TestGetProvider_Found(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requireAPIKey(t, r)
		if r.Method != http.MethodGet || r.URL.Path != "/v1/platform/providers/prov-1" {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status":      "connected",
			"account_id":  "123456789012",
			"external_id": "ext-abc",
		})
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "test-key")
	status, accountID, externalID, found, err := c.GetProvider(context.Background(), "prov-1")
	if err != nil {
		t.Fatalf("GetProvider: %v", err)
	}
	if !found || status != "connected" || accountID != "123456789012" || externalID != "ext-abc" {
		t.Fatalf("got found=%v status=%q account=%q ext=%q", found, status, accountID, externalID)
	}
}

func TestGetProvider_NotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requireAPIKey(t, r)
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "test-key")
	_, _, _, found, err := c.GetProvider(context.Background(), "missing")
	if err != nil {
		t.Fatalf("GetProvider: %v", err)
	}
	if found {
		t.Fatal("expected found=false for a 404")
	}
}

func TestDeleteProvider(t *testing.T) {
	var hit bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requireAPIKey(t, r)
		if r.Method != http.MethodDelete || r.URL.Path != "/v1/platform/providers/prov-1" {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		hit = true
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "test-key")
	if err := c.DeleteProvider(context.Background(), "prov-1"); err != nil {
		t.Fatalf("DeleteProvider: %v", err)
	}
	if !hit {
		t.Fatal("expected the delete endpoint to be hit")
	}
}

func TestDeleteProvider_404Tolerated(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requireAPIKey(t, r)
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "test-key")
	if err := c.DeleteProvider(context.Background(), "gone"); err != nil {
		t.Fatalf("DeleteProvider should tolerate 404, got %v", err)
	}
}

func TestCreateProvider_ServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":"boom"}`))
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "test-key")
	_, _, err := c.CreateProvider(context.Background(), nil, "us-east-1", testClientExternalID)
	if err == nil {
		t.Fatal("expected an error for a 500 response")
	}
}

func TestCreateProvider_RequiresClientExternalID(t *testing.T) {
	c := NewClient("http://127.0.0.1:1", "test-key")
	_, _, err := c.CreateProvider(context.Background(), nil, "us-east-1", "")
	if err == nil || !strings.Contains(err.Error(), "external_id is required") {
		t.Fatalf("expected missing external_id error, got %v", err)
	}
}

func TestUpdateProvider_SendsRegionsAndSTSRegion(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requireAPIKey(t, r)
		if r.Method != http.MethodPut || r.URL.Path != "/v1/platform/providers/prov-1" {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		body := readJSON(t, r)
		if body["sts_region"] != "eu-west-1" {
			t.Fatalf("expected sts_region=eu-west-1, got %v", body["sts_region"])
		}
		regions, _ := body["regions"].([]any)
		if len(regions) != 1 || regions[0] != "ap-southeast-2" {
			t.Fatalf("expected regions=[ap-southeast-2], got %v", body["regions"])
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id":"prov-1"}`))
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "test-key")
	if err := c.UpdateProvider(context.Background(), "prov-1", []string{"ap-southeast-2"}, "eu-west-1"); err != nil {
		t.Fatalf("UpdateProvider: %v", err)
	}
}
