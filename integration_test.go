//go:build integration

package main

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"testing"
	"time"

	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

// Shared test infrastructure for integration tests.
// Manages lifecycle of Typst and SeaweedFS containers.

var (
	// testCompiler is the shared Typst container compiler.
	testCompiler *ContainerTypstCompiler
	// seaweedContainer is the shared SeaweedFS container.
	seaweedContainer testcontainers.Container
	// seaweedHostPort is the host:port for the SeaweedFS S3 API.
	seaweedHostPort string
	// seaweedBucketURL is the gocloud-compatible S3 bucket URL.
	seaweedBucketURL string
)

// TestMain sets up and tears down shared containers for all integration tests.
func TestMain(m *testing.M) {
	ctx := context.Background()

	// Start Typst container.
	var err error
	testCompiler, err = NewContainerTypstCompiler(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to create typst compiler: %v\n", err)
		os.Exit(1)
	}

	// Start SeaweedFS container.
	if err = startSeaweedFS(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "failed to start seaweedfs: %v\n", err)
		if testCompiler != nil {
			_ = testCompiler.Close()
		}
		os.Exit(1)
	}

	code := m.Run()

	// Cleanup containers.
	if testCompiler != nil {
		_ = testCompiler.Close()
	}
	if seaweedContainer != nil {
		_ = testcontainers.TerminateContainer(seaweedContainer)
	}

	os.Exit(code)
}

// startSeaweedFS starts the shared SeaweedFS container.
func startSeaweedFS(ctx context.Context) error {
	req := testcontainers.ContainerRequest{
		Image:        seaweedImage,
		ExposedPorts: []string{seaweedS3Port},
		Cmd:          []string{"server", "-s3", "-dir=/data"},
		WaitingFor: wait.ForAll(
			wait.ForListeningPort(seaweedS3Port),
			wait.ForLog("Start Seaweed S3 API"),
		).WithDeadline(60 * time.Second),
	}

	var err error
	seaweedContainer, err = testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
	})
	if err != nil {
		return fmt.Errorf("start container: %w", err)
	}

	host, err := seaweedContainer.Host(ctx)
	if err != nil {
		return fmt.Errorf("get host: %w", err)
	}

	port, err := seaweedContainer.MappedPort(ctx, seaweedS3Port)
	if err != nil {
		return fmt.Errorf("get port: %w", err)
	}

	seaweedHostPort = net.JoinHostPort(host, port.Port())
	endpoint := fmt.Sprintf("http://%s", seaweedHostPort)

	// Give SeaweedFS a moment to fully initialize.
	time.Sleep(2 * time.Second)

	// Create the initial test bucket.
	if err = createInitialBucket(ctx); err != nil {
		return fmt.Errorf("create bucket: %w", err)
	}

	// Build gocloud-compatible S3 URL.
	seaweedBucketURL = fmt.Sprintf(
		"s3://%s?endpoint=%s&disable_https=true&s3ForcePathStyle=true&region=us-east-1",
		testBucketName,
		url.QueryEscape(endpoint),
	)

	return nil
}

// createInitialBucket creates the test bucket in SeaweedFS.
func createInitialBucket(ctx context.Context) error {
	bucketURL := fmt.Sprintf("http://%s/%s", seaweedHostPort, testBucketName)

	req, err := http.NewRequestWithContext(ctx, http.MethodPut, bucketURL, nil)
	if err != nil {
		return err
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusConflict {
		return fmt.Errorf("unexpected status: %d", resp.StatusCode)
	}

	return nil
}
