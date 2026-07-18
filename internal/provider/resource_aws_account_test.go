package provider

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestEnsureProvider_AdoptsMatchingProviderWithoutCreatingDuplicate(t *testing.T) {
	var methods []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		methods = append(methods, req.Method)
		switch req.Method {
		case http.MethodGet:
			_ = json.NewEncoder(w).Encode(map[string]any{
				"status":      "pending",
				"external_id": testClientExternalID,
			})
		case http.MethodPut:
			body := readJSON(t, req)
			if body["sts_region"] != "eu-west-1" {
				t.Fatalf("expected adopted STS region, got %v", body["sts_region"])
			}
			w.WriteHeader(http.StatusOK)
		default:
			t.Fatalf("adoption must not issue %s", req.Method)
		}
	}))
	defer srv.Close()

	r := &awsAccountResource{client: NewClient(srv.URL, "test-key")}
	id, externalID, err := r.ensureProvider(
		context.Background(),
		"prov-1",
		[]string{"us-east-1"},
		"eu-west-1",
		testClientExternalID,
	)
	if err != nil {
		t.Fatalf("ensureProvider: %v", err)
	}
	if id != "prov-1" || externalID != testClientExternalID {
		t.Fatalf("got id=%q external_id=%q", id, externalID)
	}
	if strings.Join(methods, ",") != "GET,PUT" {
		t.Fatalf("expected GET,PUT adoption sequence, got %v", methods)
	}
}

func TestEnsureProvider_RejectsExternalIDMismatchWithoutPOST(t *testing.T) {
	var postCount int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.Method == http.MethodPost {
			postCount++
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status":      "pending",
			"external_id": "8d8dc95e-c615-4b6a-a20d-fbe209627849",
		})
	}))
	defer srv.Close()

	r := &awsAccountResource{client: NewClient(srv.URL, "test-key")}
	_, _, err := r.ensureProvider(
		context.Background(),
		"prov-1",
		nil,
		"us-east-1",
		testClientExternalID,
	)
	if err == nil || !strings.Contains(err.Error(), "external_id") {
		t.Fatalf("expected external_id mismatch, got %v", err)
	}
	if postCount != 0 {
		t.Fatalf("adoption mismatch created %d duplicate providers", postCount)
	}
}
