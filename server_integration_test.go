//go:build integration

package main

import (
	"bytes"
	"context"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

const (
	// seaweedImage is the SeaweedFS Docker image.
	seaweedImage = "chrislusf/seaweedfs:latest"
	// seaweedS3Port is the S3 API port for SeaweedFS.
	seaweedS3Port = "8333/tcp"
	// testBucketName is the name of the test bucket.
	testBucketName = "test-bucket"
)

// setupSeaweedFS starts a SeaweedFS container and returns a gocloud-compatible bucket URL.
func setupSeaweedFS(t *testing.T) (bucketURL string, hostPort string) {
	t.Helper()

	ctx := context.Background()

	req := testcontainers.ContainerRequest{
		Image:        seaweedImage,
		ExposedPorts: []string{seaweedS3Port},
		Cmd:          []string{"server", "-s3", "-dir=/data"},
		WaitingFor: wait.ForAll(
			wait.ForListeningPort(seaweedS3Port),
			wait.ForLog("Start Seaweed S3 API"),
		).WithDeadline(60 * time.Second),
	}

	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
	})
	if err != nil {
		t.Fatalf("failed to start seaweedfs container: %v", err)
	}

	t.Cleanup(func() {
		if termErr := testcontainers.TerminateContainer(container); termErr != nil {
			t.Logf("failed to terminate container: %v", termErr)
		}
	})

	host, err := container.Host(ctx)
	if err != nil {
		t.Fatalf("failed to get host: %v", err)
	}

	port, err := container.MappedPort(ctx, seaweedS3Port)
	if err != nil {
		t.Fatalf("failed to get port: %v", err)
	}

	hostPort = net.JoinHostPort(host, port.Port())
	endpoint := fmt.Sprintf("http://%s", hostPort)

	// Give SeaweedFS a moment to fully initialize all services
	time.Sleep(2 * time.Second)

	// Create the test bucket using a direct HTTP PUT request
	createBucket(t, ctx, hostPort, testBucketName)

	// Set AWS credentials for gocloud.dev/blob/s3blob
	// SeaweedFS doesn't require auth by default, but the AWS SDK needs non-empty values
	t.Setenv("AWS_ACCESS_KEY_ID", "any")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "any")

	// Build gocloud-compatible S3 URL.
	// See: https://pkg.go.dev/gocloud.dev/blob/s3blob
	bucketURL = fmt.Sprintf(
		"s3://%s?endpoint=%s&disable_https=true&s3ForcePathStyle=true&region=us-east-1",
		testBucketName,
		url.QueryEscape(endpoint),
	)

	return bucketURL, hostPort
}

// createBucket creates a bucket in SeaweedFS using the S3 CreateBucket API.
func createBucket(t *testing.T, ctx context.Context, hostPort, bucket string) {
	t.Helper()

	bucketURL := fmt.Sprintf("http://%s/%s", hostPort, bucket)

	req, err := http.NewRequestWithContext(ctx, http.MethodPut, bucketURL, nil)
	if err != nil {
		t.Fatalf("failed to create bucket request: %v", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("failed to create bucket: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusConflict {
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
	bucketURL, hostPort := setupSeaweedFS(t)

	expectedContent := "= Test Template\n\nThis is a test from S3."
	uploadToSeaweedFS(t, hostPort, testBucketName, "test.typ", []byte(expectedContent))

	srv := NewServer(testLogger(), ServerConfig{bucketURL: bucketURL})

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
	bucketURL, _ := setupSeaweedFS(t)

	srv := NewServer(testLogger(), ServerConfig{bucketURL: bucketURL})

	_, err := srv.fetchTemplate(context.Background(), "nonexistent.typ")
	if err == nil {
		t.Fatal("fetchTemplate() should return error for missing key")
	}
}

// TestFetchData_S3 tests fetching and parsing JSON data from S3.
func TestFetchData_S3(t *testing.T) {
	bucketURL, hostPort := setupSeaweedFS(t)

	dataJSON := `{"name": "John", "age": 30}`
	uploadToSeaweedFS(t, hostPort, testBucketName, "data.json", []byte(dataJSON))

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

// TestFetchData_S3_NotFound tests fetching non-existent data from S3.
func TestFetchData_S3_NotFound(t *testing.T) {
	bucketURL, _ := setupSeaweedFS(t)

	srv := NewServer(testLogger(), ServerConfig{bucketURL: bucketURL})

	_, err := srv.fetchData(context.Background(), "nonexistent.json")
	if err == nil {
		t.Fatal("fetchData() should return error for missing key")
	}
}

// TestFetchData_S3_InvalidJSON tests fetching invalid JSON from S3.
func TestFetchData_S3_InvalidJSON(t *testing.T) {
	bucketURL, hostPort := setupSeaweedFS(t)

	uploadToSeaweedFS(t, hostPort, testBucketName, "bad.json", []byte("not valid json"))

	srv := NewServer(testLogger(), ServerConfig{bucketURL: bucketURL})

	_, err := srv.fetchData(context.Background(), "bad.json")
	if err == nil {
		t.Fatal("fetchData() should return error for invalid JSON")
	}

	if !strings.Contains(err.Error(), "invalid JSON") {
		t.Errorf("expected 'invalid JSON' error, got: %v", err)
	}
}
