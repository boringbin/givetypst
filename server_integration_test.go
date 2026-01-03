//go:build integration

package main

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"
)

const (
	// seaweedImage is the SeaweedFS Docker image.
	seaweedImage = "chrislusf/seaweedfs:latest"
	// seaweedS3Port is the S3 API port for SeaweedFS.
	seaweedS3Port = "8333/tcp"
	// testBucketName is the name of the test bucket.
	testBucketName = "test-bucket"
)

// resetBucket deletes and recreates the test bucket to ensure test isolation.
func resetBucket(t *testing.T) {
	t.Helper()

	ctx := context.Background()

	// Delete the bucket.
	deleteURL := fmt.Sprintf("http://%s/%s?force=true", seaweedHostPort, testBucketName)
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, deleteURL, nil)
	if err != nil {
		t.Fatalf("failed to create delete request: %v", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("failed to delete bucket: %v", err)
	}
	resp.Body.Close()

	// Recreate the bucket.
	createURL := fmt.Sprintf("http://%s/%s", seaweedHostPort, testBucketName)
	req, err = http.NewRequestWithContext(ctx, http.MethodPut, createURL, nil)
	if err != nil {
		t.Fatalf("failed to create bucket request: %v", err)
	}

	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("failed to create bucket: %v", err)
	}
	resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("failed to create bucket, status: %d", resp.StatusCode)
	}
}

// uploadToSeaweedFS uploads a file to SeaweedFS using HTTP PUT.
func uploadToSeaweedFS(t *testing.T, hostPort, bucket, key string, content []byte) {
	t.Helper()

	ctx := context.Background()
	objectURL := fmt.Sprintf("http://%s/%s/%s", hostPort, bucket, key)

	req, err := http.NewRequestWithContext(ctx, http.MethodPut, objectURL, bytes.NewReader(content))
	if err != nil {
		t.Fatalf("failed to create upload request: %v", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("failed to upload object: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("failed to upload object, status: %d", resp.StatusCode)
	}
}

// TestFetchTemplate_S3 tests fetching a template from S3.
func TestFetchTemplate_S3(t *testing.T) {
	resetBucket(t)
	t.Setenv("AWS_ACCESS_KEY_ID", "any")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "any")

	expectedContent := "= Test Template\n\nThis is a test from S3."
	uploadToSeaweedFS(t, seaweedHostPort, testBucketName, "test.typ", []byte(expectedContent))

	srv := NewServer(testLogger(), ServerConfig{bucketURL: seaweedBucketURL})

	content, err := srv.fetchTemplate(context.Background(), "test.typ")
	if err != nil {
		t.Fatalf("fetchTemplate() returned error: %v", err)
	}

	if content != expectedContent {
		t.Errorf("expected content %q, got %q", expectedContent, content)
	}
}

// TestFetchTemplate_S3_NotFound tests fetching a non-existent template from S3.
func TestFetchTemplate_S3_NotFound(t *testing.T) {
	resetBucket(t)
	t.Setenv("AWS_ACCESS_KEY_ID", "any")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "any")

	srv := NewServer(testLogger(), ServerConfig{bucketURL: seaweedBucketURL})

	_, err := srv.fetchTemplate(context.Background(), "nonexistent.typ")
	if err == nil {
		t.Fatal("fetchTemplate() should return error for missing key")
	}
}

// TestFetchData_S3 tests fetching and parsing JSON data from S3.
func TestFetchData_S3(t *testing.T) {
	resetBucket(t)
	t.Setenv("AWS_ACCESS_KEY_ID", "any")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "any")

	dataJSON := `{"name": "John", "age": 30}`
	uploadToSeaweedFS(t, seaweedHostPort, testBucketName, "data.json", []byte(dataJSON))

	srv := NewServer(testLogger(), ServerConfig{bucketURL: seaweedBucketURL})

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

// TestFetchData_S3_NotFound tests fetching non-existent data from S3.
func TestFetchData_S3_NotFound(t *testing.T) {
	resetBucket(t)
	t.Setenv("AWS_ACCESS_KEY_ID", "any")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "any")

	srv := NewServer(testLogger(), ServerConfig{bucketURL: seaweedBucketURL})

	_, err := srv.fetchData(context.Background(), "nonexistent.json")
	if err == nil {
		t.Fatal("fetchData() should return error for missing key")
	}
}

// TestFetchData_S3_InvalidJSON tests fetching invalid JSON from S3.
func TestFetchData_S3_InvalidJSON(t *testing.T) {
	resetBucket(t)
	t.Setenv("AWS_ACCESS_KEY_ID", "any")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "any")

	uploadToSeaweedFS(t, seaweedHostPort, testBucketName, "bad.json", []byte("not valid json"))

	srv := NewServer(testLogger(), ServerConfig{bucketURL: seaweedBucketURL})

	_, err := srv.fetchData(context.Background(), "bad.json")
	if err == nil {
		t.Fatal("fetchData() should return error for invalid JSON")
	}

	if !strings.Contains(err.Error(), "invalid JSON") {
		t.Errorf("expected 'invalid JSON' error, got: %v", err)
	}
}
