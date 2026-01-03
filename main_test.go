package main

import (
	"bytes"
	"flag"
	"io"
	"os"
	"strings"
	"syscall"
	"testing"
	"time"
)

// TestSetupLogger tests the setupLogger function.
func TestSetupLogger(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		verbose bool
	}{
		{name: "verbose mode", verbose: true},
		{name: "non-verbose mode", verbose: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			logger := setupLogger(tt.verbose)
			if logger == nil {
				t.Fatal("setupLogger() returned nil")
			}
		})
	}
}

// TestPrintUsage tests the printUsage function.
func TestPrintUsage(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	printUsage(&buf, "givetypst")

	output := buf.String()

	expectedStrings := []string{
		"Usage: givetypst [OPTIONS]",
		"Generate PDFs from Typst templates",
		"BUCKET_URL",
		"PORT",
		"Options:",
	}

	for _, expected := range expectedStrings {
		if !strings.Contains(output, expected) {
			t.Errorf("printUsage() output missing %q", expected)
		}
	}
}

// runTestConfig holds configuration for a run() test case.
type runTestConfig struct {
	name               string
	args               []string
	env                map[string]string
	signal             syscall.Signal
	signalDelay        time.Duration
	wantExitCode       int
	wantOutputContains []string
}

// runTest executes a test case for the run() function.
// It handles saving/restoring global state, capturing stdout, and sending signals.
func runTest(t *testing.T, tc runTestConfig) {
	t.Helper()

	// Save and restore global state.
	oldArgs := os.Args
	oldCommandLine := flag.CommandLine
	t.Cleanup(func() {
		os.Args = oldArgs
		flag.CommandLine = oldCommandLine
	})

	// Set environment variables.
	for k, v := range tc.env {
		t.Setenv(k, v)
	}

	// Reset flag.CommandLine and set args.
	flag.CommandLine = flag.NewFlagSet(tc.args[0], flag.ExitOnError)
	os.Args = tc.args

	// Capture stdout.
	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	// Send signal after delay if specified.
	if tc.signal != 0 {
		delay := tc.signalDelay
		if delay == 0 {
			delay = 100 * time.Millisecond
		}
		go func() {
			time.Sleep(delay)
			_ = syscall.Kill(syscall.Getpid(), tc.signal)
		}()
	}

	exitCode := run()

	_ = w.Close()
	os.Stdout = oldStdout

	if exitCode != tc.wantExitCode {
		t.Errorf("run() returned exit code %d, want %d", exitCode, tc.wantExitCode)
	}

	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
	output := buf.String()

	for _, want := range tc.wantOutputContains {
		if !strings.Contains(output, want) {
			t.Errorf("output should contain %q, got: %s", want, output)
		}
	}
}

// TestRun_VersionFlag tests the version flag.
func TestRun_VersionFlag(t *testing.T) {
	runTest(t, runTestConfig{
		name:               "version flag",
		args:               []string{"givetypst", "-version"},
		wantExitCode:       0,
		wantOutputContains: []string{"givetypst version"},
	})
}

// TestRun_MissingBucketURL tests the missing BUCKET_URL.
func TestRun_MissingBucketURL(t *testing.T) {
	runTest(t, runTestConfig{
		name:               "missing BUCKET_URL",
		args:               []string{"givetypst"},
		env:                map[string]string{"BUCKET_URL": ""},
		wantExitCode:       1,
		wantOutputContains: []string{"BUCKET_URL"},
	})
}

// TestRun_PortEnvOverride tests the PORT env overrides flag.
func TestRun_PortEnvOverride(t *testing.T) {
	runTest(t, runTestConfig{
		name:               "PORT env overrides flag",
		args:               []string{"givetypst", "-port", "19099"},
		env:                map[string]string{"BUCKET_URL": "mem://", "PORT": "19001"},
		signal:             syscall.SIGTERM,
		wantExitCode:       0,
		wantOutputContains: []string{"19001"},
	})
}

// TestRun_InvalidPortEnv tests the invalid PORT env.
func TestRun_InvalidPortEnv(t *testing.T) {
	runTest(t, runTestConfig{
		name:               "invalid PORT falls back to flag",
		args:               []string{"givetypst", "-port", "19002"},
		env:                map[string]string{"BUCKET_URL": "mem://", "PORT": "not-a-number"},
		signal:             syscall.SIGTERM,
		wantExitCode:       0,
		wantOutputContains: []string{"19002"},
	})
}

// TestRun_DefaultPort tests the default port 8080.
func TestRun_DefaultPort(t *testing.T) {
	runTest(t, runTestConfig{
		name:               "default port 8080",
		args:               []string{"givetypst"},
		env:                map[string]string{"BUCKET_URL": "mem://"},
		signal:             syscall.SIGTERM,
		wantExitCode:       0,
		wantOutputContains: []string{"8080"},
	})
}

// TestRun_VerboseMode tests the verbose mode.
func TestRun_VerboseMode(t *testing.T) {
	runTest(t, runTestConfig{
		name:               "verbose mode",
		args:               []string{"givetypst", "-v"},
		env:                map[string]string{"BUCKET_URL": "mem://", "PORT": "19003"},
		signal:             syscall.SIGTERM,
		wantExitCode:       0,
		wantOutputContains: []string{"starting HTTP server"},
	})
}

// TestRun_GracefulShutdownSIGINT tests the graceful shutdown SIGINT.
func TestRun_GracefulShutdownSIGINT(t *testing.T) {
	runTest(t, runTestConfig{
		name:               "graceful shutdown SIGINT",
		args:               []string{"givetypst"},
		env:                map[string]string{"BUCKET_URL": "mem://", "PORT": "19004"},
		signal:             syscall.SIGINT,
		wantExitCode:       0,
		wantOutputContains: []string{"server stopped gracefully"},
	})
}

// TestRun_GracefulShutdownSIGTERM tests the graceful shutdown SIGTERM.
func TestRun_GracefulShutdownSIGTERM(t *testing.T) {
	runTest(t, runTestConfig{
		name:               "graceful shutdown SIGTERM",
		args:               []string{"givetypst"},
		env:                map[string]string{"BUCKET_URL": "mem://", "PORT": "19005"},
		signal:             syscall.SIGTERM,
		wantExitCode:       0,
		wantOutputContains: []string{"server stopped gracefully"},
	})
}

// TestRun_BucketURLEnv tests the BUCKET_URL from env.
func TestRun_BucketURLEnv(t *testing.T) {
	runTest(t, runTestConfig{
		name:               "BUCKET_URL from env",
		args:               []string{"givetypst"},
		env:                map[string]string{"BUCKET_URL": "mem://test-bucket", "PORT": "19006"},
		signal:             syscall.SIGTERM,
		wantExitCode:       0,
		wantOutputContains: []string{"starting HTTP server"},
	})
}
