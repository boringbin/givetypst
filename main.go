package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"
)

// version is the version of the `givetypst` CLI.
// Set to "dev" by default for local builds.
// Overridden by goreleaser via -ldflags "-X main.version=v0.1.0" when creating releases.
var version = "dev"

const (
	// defaultPort is the default HTTP port.
	defaultPort = 8080
	// readHeaderTimeout is the timeout for reading request headers.
	readHeaderTimeout = 10 * time.Second
	// readTimeout is the timeout for reading the entire request.
	readTimeout = 30 * time.Second
	// writeTimeout is the timeout for writing the response.
	writeTimeout = 60 * time.Second
	// shutdownTimeout is the timeout for graceful shutdown.
	shutdownTimeout = 10 * time.Second
	// exitSuccess is the exit code for success.
	exitSuccess = 0
	// exitError is the exit code for error.
	exitError = 1
)

func main() {
	os.Exit(run())
}

func run() int {
	var (
		port        = flag.Int("port", defaultPort, "HTTP port to listen on")
		verbose     = flag.Bool("v", false, "Verbose output (debug mode)")
		showVersion = flag.Bool("version", false, "Show version and exit")
	)

	// Customize usage message
	printUsageFunc := func() {
		printUsage(os.Stderr, os.Args[0])
	}
	flag.CommandLine.Usage = printUsageFunc

	flag.Parse()

	// Handle version flag
	if *showVersion {
		fmt.Fprintf(os.Stdout, "givetypst version %s\n", version)
		return exitSuccess
	}

	// Setup logger
	logger := setupLogger(*verbose)

	// Get bucket URL from environment variable (required)
	bucketURL := os.Getenv("BUCKET_URL")
	if bucketURL == "" {
		logger.Error("BUCKET_URL environment variable is required")
		return exitError
	}

	// Get port from flag or environment variable
	portNum := *port
	if portEnv := os.Getenv("PORT"); portEnv != "" {
		if portFromEnv, err := strconv.Atoi(portEnv); err == nil {
			portNum = portFromEnv
		}
	}

	// Get max template size from environment variable (optional)
	var maxTemplateSize int64
	if maxTemplateSizeEnv := os.Getenv("MAX_TEMPLATE_SIZE"); maxTemplateSizeEnv != "" {
		if parsed, err := strconv.ParseInt(maxTemplateSizeEnv, 10, 64); err == nil && parsed > 0 {
			maxTemplateSize = parsed
		}
	}

	// Get max data size from environment variable (optional)
	var maxDataSize int64
	if maxDataSizeEnv := os.Getenv("MAX_DATA_SIZE"); maxDataSizeEnv != "" {
		if parsed, err := strconv.ParseInt(maxDataSizeEnv, 10, 64); err == nil && parsed > 0 {
			maxDataSize = parsed
		}
	}

	// Create server
	srv := NewServer(logger, ServerConfig{
		bucketURL:       bucketURL,
		maxTemplateSize: maxTemplateSize,
		maxDataSize:     maxDataSize,
	})

	// Create HTTP server
	httpServer := &http.Server{
		Addr:              fmt.Sprintf(":%d", portNum),
		Handler:           srv.Handler(),
		ReadHeaderTimeout: readHeaderTimeout,
		ReadTimeout:       readTimeout,
		WriteTimeout:      writeTimeout,
	}

	// Start server in a goroutine
	serverErrors := make(chan error, 1)
	go func() {
		logger.Info("starting HTTP server", "port", portNum)
		serverErrors <- httpServer.ListenAndServe()
	}()

	// Wait for interrupt signal or server error
	shutdown := make(chan os.Signal, 1)
	signal.Notify(shutdown, syscall.SIGINT, syscall.SIGTERM)

	select {
	case serverErr := <-serverErrors:
		logger.Error("server error", "error", serverErr)
		return exitError
	case sig := <-shutdown:
		logger.Info("received shutdown signal", "signal", sig.String())

		// Graceful shutdown
		shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
		defer cancel()

		if shutdownErr := httpServer.Shutdown(shutdownCtx); shutdownErr != nil {
			logger.Error("graceful shutdown failed", "error", shutdownErr)
			if closeErr := httpServer.Close(); closeErr != nil {
				logger.Error("forced shutdown failed", "error", closeErr)
			}
			return exitError
		}

		logger.Info("server stopped gracefully")
		return exitSuccess
	}
}

// printUsage prints the usage message to the provided writer.
func printUsage(w io.Writer, progName string) {
	fmt.Fprintf(w, "Usage: %s [OPTIONS]\n\n", progName)
	fmt.Fprintf(w, "Generate PDFs from Typst templates stored in cloud storage.\n\n")
	fmt.Fprintf(w, "Environment Variables:\n")
	fmt.Fprintf(w, "  BUCKET_URL          URL of the cloud storage bucket containing templates (required)\n")
	fmt.Fprintf(w, "  PORT                HTTP port to listen on (overrides -port flag)\n")
	fmt.Fprintf(w, "  MAX_TEMPLATE_SIZE   Maximum template file size in bytes (default: 1048576)\n")
	fmt.Fprintf(w, "  MAX_DATA_SIZE       Maximum data file size in bytes (default: 10485760)\n\n")
	fmt.Fprintf(w, "Options:\n")
	flag.CommandLine.SetOutput(w)
	flag.PrintDefaults()
}

// setupLogger sets up the logger based on the verbose flag.
func setupLogger(verbose bool) *slog.Logger {
	logLevel := slog.LevelInfo
	if verbose {
		logLevel = slog.LevelDebug
	}
	return slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: logLevel,
	}))
}
