package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os/exec"
	"time"

	"gocloud.dev/blob"
	_ "gocloud.dev/blob/s3blob"
)

const (
	// fetchTimeout is the timeout for fetching files from storage.
	fetchTimeout = 30 * time.Second
	// defaultMaxTemplateSize is the default maximum size of a template file (1MB).
	defaultMaxTemplateSize = 1024 * 1024
	// defaultMaxDataSize is the default maximum size of a data file (10MB).
	defaultMaxDataSize = 10 * 1024 * 1024
)

// ServerConfig is the configuration for the server.
type ServerConfig struct {
	// bucketURL is the URL of the storage bucket.
	bucketURL string
	// maxTemplateSize is the maximum size of a template file in bytes.
	maxTemplateSize int64
	// maxDataSize is the maximum size of a data file in bytes.
	maxDataSize int64
}

// Server is the server for the `givetypst` CLI.
type Server struct {
	// logger is the logger for the server.
	logger *slog.Logger
	// config is the configuration for the server.
	config ServerConfig
}

// NewServer creates a new server.
func NewServer(logger *slog.Logger, config ServerConfig) *Server {
	// Apply defaults if not set.
	if config.maxTemplateSize <= 0 {
		config.maxTemplateSize = defaultMaxTemplateSize
	}
	if config.maxDataSize <= 0 {
		config.maxDataSize = defaultMaxDataSize
	}

	return &Server{
		logger: logger,
		config: config,
	}
}

// Handler returns the HTTP handler for the server.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("POST /generate", s.handleGenerate)
	mux.HandleFunc("GET /health", s.handleHealth)

	return mux
}

// handleHealth checks if the typst command is available.
//
// Will return an "OK" response if everything looks good.
func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	// First, check if the typst command is available.
	if _, err := exec.LookPath("typst"); err != nil {
		http.Error(w, "typst not found", http.StatusServiceUnavailable)
		return
	}
	// Next, check if we have access to the storage bucket.
	bucket, bucketErr := blob.OpenBucket(r.Context(), s.config.bucketURL)
	if bucketErr != nil {
		http.Error(w, "failed to open bucket", http.StatusServiceUnavailable)
		return
	}
	_ = bucket.Close()

	if _, writeErr := w.Write([]byte("OK")); writeErr != nil {
		s.logger.Error("failed to write health response", "error", writeErr)
	}
}

// GenerateRequest is the request body for the /generate endpoint.
type GenerateRequest struct {
	// TemplateKey is the key of the template in the storage bucket.
	TemplateKey string `json:"templateKey"`
	// Data is the inline data to inject into the template.
	Data map[string]any `json:"data,omitempty"`
	// DataKey is the key of a JSON data file in the storage bucket.
	DataKey string `json:"dataKey,omitempty"`
}

// handleGenerate generates a PDF from a template.
func (s *Server) handleGenerate(w http.ResponseWriter, r *http.Request) {
	var req GenerateRequest

	// Check if the request is valid.
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}

	// Validate templateKey is provided.
	if req.TemplateKey == "" {
		http.Error(w, "templateKey is required", http.StatusBadRequest)
		return
	}

	// Validate that both data and dataKey are not provided.
	if req.Data != nil && req.DataKey != "" {
		http.Error(w, "cannot specify both 'data' and 'dataKey'", http.StatusBadRequest)
		return
	}

	// Resolve data: either from inline data or from bucket.
	var data map[string]any
	if req.DataKey != "" {
		fetchedData, fetchErr := s.fetchData(r.Context(), req.DataKey)
		if fetchErr != nil {
			http.Error(w, fmt.Sprintf("failed to fetch data: %v", fetchErr), http.StatusInternalServerError)
			return
		}
		data = fetchedData
	} else {
		data = req.Data // May be nil, which is valid.
	}

	// Fetch the template from the storage bucket.
	source, err := s.fetchTemplate(r.Context(), req.TemplateKey)
	if err != nil {
		http.Error(w, fmt.Sprintf("failed to fetch template: %v", err), http.StatusInternalServerError)
		return
	}

	// Compile the template into a PDF.
	pdf, err := compileTypst(source, data)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Return the PDF.
	w.Header().Set("Content-Type", "application/pdf")
	w.Header().Set("Content-Disposition", "inline; filename=\"output.pdf\"")
	if _, writeErr := w.Write(pdf); writeErr != nil {
		s.logger.Error("failed to write PDF response", "error", writeErr)
	}
}

// fetchFromBucket fetches a file from the storage bucket with size limiting.
func (s *Server) fetchFromBucket(ctx context.Context, key string, maxSize int64) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, fetchTimeout)
	defer cancel()

	bucket, err := blob.OpenBucket(ctx, s.config.bucketURL)
	if err != nil {
		return nil, fmt.Errorf("open bucket: %w", err)
	}
	defer bucket.Close()

	reader, err := bucket.NewReader(ctx, key, nil)
	if err != nil {
		return nil, fmt.Errorf("open key %s: %w", key, err)
	}
	defer reader.Close()

	data, err := io.ReadAll(io.LimitReader(reader, maxSize))
	if err != nil {
		return nil, fmt.Errorf("read: %w", err)
	}

	return data, nil
}

// fetchTemplate fetches a template from the storage bucket.
func (s *Server) fetchTemplate(ctx context.Context, key string) (string, error) {
	data, err := s.fetchFromBucket(ctx, key, s.config.maxTemplateSize)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// fetchData fetches a JSON data file from the storage bucket.
func (s *Server) fetchData(ctx context.Context, key string) (map[string]any, error) {
	rawData, err := s.fetchFromBucket(ctx, key, s.config.maxDataSize)
	if err != nil {
		return nil, err
	}

	var data map[string]any
	if unmarshalErr := json.Unmarshal(rawData, &data); unmarshalErr != nil {
		return nil, fmt.Errorf("invalid JSON: %w", unmarshalErr)
	}

	return data, nil
}
