package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	_ "gocloud.dev/blob/fileblob"
)

// testLogger returns a logger that discards output.
func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// setupTestBucket creates a file-based bucket and populates it with test data.
func setupTestBucket(t *testing.T, files map[string][]byte) string {
	t.Helper()

	dir := t.TempDir()

	for key, content := range files {
		filePath := filepath.Join(dir, key)
		if err := os.MkdirAll(filepath.Dir(filePath), 0755); err != nil {
			t.Fatalf("failed to create directory for %s: %v", key, err)
		}
		if err := os.WriteFile(filePath, content, 0644); err != nil {
			t.Fatalf("failed to write test file %s: %v", key, err)
		}
	}

	return "file://" + dir
}

// TestNewServer_DefaultLimits tests the default limits.
func TestNewServer_DefaultLimits(t *testing.T) {
	t.Parallel()

	srv := NewServer(testLogger(), ServerConfig{
		bucketURL: "file:///tmp/test",
	})

	if srv.config.maxTemplateSize != defaultMaxTemplateSize {
		t.Errorf("expected maxTemplateSize %d, got %d", defaultMaxTemplateSize, srv.config.maxTemplateSize)
	}
	if srv.config.maxDataSize != defaultMaxDataSize {
		t.Errorf("expected maxDataSize %d, got %d", defaultMaxDataSize, srv.config.maxDataSize)
	}
}

// TestNewServer_CustomLimits tests the custom limits.
func TestNewServer_CustomLimits(t *testing.T) {
	t.Parallel()

	srv := NewServer(testLogger(), ServerConfig{
		bucketURL:       "file:///tmp/test",
		maxTemplateSize: 500,
		maxDataSize:     1000,
	})

	if srv.config.maxTemplateSize != 500 {
		t.Errorf("expected maxTemplateSize 500, got %d", srv.config.maxTemplateSize)
	}
	if srv.config.maxDataSize != 1000 {
		t.Errorf("expected maxDataSize 1000, got %d", srv.config.maxDataSize)
	}
}

// TestHandleGenerate_Errors tests the handleGenerate errors.
func TestHandleGenerate_Errors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name             string
		files            map[string][]byte
		reqBody          string
		wantStatus       int
		wantBodyContains string
	}{
		{
			name:             "missing templateKey",
			files:            map[string][]byte{},
			reqBody:          `{}`,
			wantStatus:       http.StatusBadRequest,
			wantBodyContains: "templateKey is required",
		},
		{
			name:             "empty templateKey",
			files:            map[string][]byte{},
			reqBody:          `{"templateKey": ""}`,
			wantStatus:       http.StatusBadRequest,
			wantBodyContains: "templateKey is required",
		},
		{
			name:             "invalid JSON body",
			files:            map[string][]byte{},
			reqBody:          "not json",
			wantStatus:       http.StatusBadRequest,
			wantBodyContains: "invalid request",
		},
		{
			name:             "both data and dataKey",
			files:            map[string][]byte{"template.typ": []byte("= Hello")},
			reqBody:          `{"templateKey": "template.typ", "data": {"foo": "bar"}, "dataKey": "data.json"}`,
			wantStatus:       http.StatusBadRequest,
			wantBodyContains: "cannot specify both",
		},
		{
			name:             "template not found",
			files:            map[string][]byte{},
			reqBody:          `{"templateKey": "nonexistent.typ"}`,
			wantStatus:       http.StatusInternalServerError,
			wantBodyContains: "failed to fetch template",
		},
		{
			name:             "dataKey not found",
			files:            map[string][]byte{"template.typ": []byte("= Hello")},
			reqBody:          `{"templateKey": "template.typ", "dataKey": "nonexistent.json"}`,
			wantStatus:       http.StatusInternalServerError,
			wantBodyContains: "failed to fetch data",
		},
		{
			name: "invalid JSON in dataKey",
			files: map[string][]byte{
				"template.typ": []byte("= Hello"),
				"bad.json":     []byte("not valid json"),
			},
			reqBody:          `{"templateKey": "template.typ", "dataKey": "bad.json"}`,
			wantStatus:       http.StatusInternalServerError,
			wantBodyContains: "invalid JSON",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			bucketURL := setupTestBucket(t, tt.files)
			srv := NewServer(testLogger(), ServerConfig{bucketURL: bucketURL})

			req := httptest.NewRequest(http.MethodPost, "/generate", strings.NewReader(tt.reqBody))
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()

			srv.handleGenerate(rec, req)

			if rec.Code != tt.wantStatus {
				t.Errorf("expected status %d, got %d", tt.wantStatus, rec.Code)
			}

			body := rec.Body.String()
			if !strings.Contains(body, tt.wantBodyContains) {
				t.Errorf("expected body to contain %q, got: %s", tt.wantBodyContains, body)
			}
		})
	}
}

// TestFetchTemplate_Success tests the fetchTemplate success.
func TestFetchTemplate_Success(t *testing.T) {
	t.Parallel()

	expectedContent := "= Test Template\n\nThis is a test."
	bucketURL := setupTestBucket(t, map[string][]byte{
		"test.typ": []byte(expectedContent),
	})

	srv := NewServer(testLogger(), ServerConfig{bucketURL: bucketURL})

	content, err := srv.fetchTemplate(context.Background(), "test.typ")
	if err != nil {
		t.Fatalf("fetchTemplate() returned error: %v", err)
	}

	if content != expectedContent {
		t.Errorf("expected content %q, got %q", expectedContent, content)
	}
}

// TestFetchTemplate_NotFound tests the fetchTemplate not found.
func TestFetchTemplate_NotFound(t *testing.T) {
	t.Parallel()

	bucketURL := setupTestBucket(t, map[string][]byte{})
	srv := NewServer(testLogger(), ServerConfig{bucketURL: bucketURL})

	_, err := srv.fetchTemplate(context.Background(), "nonexistent.typ")
	if err == nil {
		t.Fatal("fetchTemplate() should return error for missing key")
	}
}

// TestFetchData_Success tests the fetchData success.
func TestFetchData_Success(t *testing.T) {
	t.Parallel()

	dataJSON := `{"name": "John", "age": 30}`
	bucketURL := setupTestBucket(t, map[string][]byte{
		"data.json": []byte(dataJSON),
	})

	srv := NewServer(testLogger(), ServerConfig{bucketURL: bucketURL})

	data, err := srv.fetchData(context.Background(), "data.json")
	if err != nil {
		t.Fatalf("fetchData() returned error: %v", err)
	}

	if data["name"] != "John" {
		t.Errorf("expected name 'John', got %v", data["name"])
	}
	if data["age"] != float64(30) {
		t.Errorf("expected age 30, got %v", data["age"])
	}
}

// TestFetchData_NotFound tests the fetchData not found.
func TestFetchData_NotFound(t *testing.T) {
	t.Parallel()

	bucketURL := setupTestBucket(t, map[string][]byte{})
	srv := NewServer(testLogger(), ServerConfig{bucketURL: bucketURL})

	_, err := srv.fetchData(context.Background(), "nonexistent.json")
	if err == nil {
		t.Fatal("fetchData() should return error for missing key")
	}
}

// TestFetchData_InvalidJSON tests the fetchData invalid JSON.
func TestFetchData_InvalidJSON(t *testing.T) {
	t.Parallel()

	bucketURL := setupTestBucket(t, map[string][]byte{
		"bad.json": []byte("not json"),
	})
	srv := NewServer(testLogger(), ServerConfig{bucketURL: bucketURL})

	_, err := srv.fetchData(context.Background(), "bad.json")
	if err == nil {
		t.Fatal("fetchData() should return error for invalid JSON")
	}

	if !strings.Contains(err.Error(), "invalid JSON") {
		t.Errorf("expected 'invalid JSON' error, got: %v", err)
	}
}

// TestGenerateRequest_JSONSerialization tests the generateRequest JSON serialization.
func TestGenerateRequest_JSONSerialization(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		json     string
		wantData bool
		wantKey  string
	}{
		{
			name:     "with inline data",
			json:     `{"templateKey": "t.typ", "data": {"foo": "bar"}}`,
			wantData: true,
			wantKey:  "",
		},
		{
			name:     "with dataKey",
			json:     `{"templateKey": "t.typ", "dataKey": "d.json"}`,
			wantData: false,
			wantKey:  "d.json",
		},
		{
			name:     "no data",
			json:     `{"templateKey": "t.typ"}`,
			wantData: false,
			wantKey:  "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var req GenerateRequest
			if err := json.Unmarshal([]byte(tt.json), &req); err != nil {
				t.Fatalf("failed to unmarshal: %v", err)
			}

			hasData := req.Data != nil
			if hasData != tt.wantData {
				t.Errorf("expected data present=%v, got %v", tt.wantData, hasData)
			}

			if req.DataKey != tt.wantKey {
				t.Errorf("expected dataKey=%q, got %q", tt.wantKey, req.DataKey)
			}
		})
	}
}

// TestHandler_RegistersRoutes tests the handler registers routes.
func TestHandler_RegistersRoutes(t *testing.T) {
	t.Parallel()

	srv := NewServer(testLogger(), ServerConfig{
		bucketURL: "file:///tmp/test",
	})

	handler := srv.Handler()
	if handler == nil {
		t.Fatal("Handler() returned nil")
	}

	// POST /generate should not return 404.
	req := httptest.NewRequest(http.MethodPost, "/generate", bytes.NewReader([]byte("{}")))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code == http.StatusNotFound {
		t.Error("POST /generate returned 404, route not registered")
	}

	// GET /health should not return 404.
	req = httptest.NewRequest(http.MethodGet, "/health", nil)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code == http.StatusNotFound {
		t.Error("GET /health returned 404, route not registered")
	}
}
