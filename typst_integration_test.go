//go:build integration

package main

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

// typstImage is the official Typst Docker image from GitHub Container Registry.
const typstImage = "ghcr.io/typst/typst:0.14.2"

// pdfMagicBytes is the magic byte sequence at the start of PDF files.
var pdfMagicBytes = []byte("%PDF")

// ContainerTypstCompiler compiles Typst files using a Docker container.
// It implements the TypstCompiler interface for use in integration tests.
type ContainerTypstCompiler struct {
	ctx       context.Context
	container testcontainers.Container
}

// NewContainerTypstCompiler creates a new container-based Typst compiler.
// The container stays running and can be reused for multiple compilations.
func NewContainerTypstCompiler(ctx context.Context) (*ContainerTypstCompiler, error) {
	req := testcontainers.ContainerRequest{
		Image:      typstImage,
		Entrypoint: []string{"sh", "-c", "tail -f /dev/null"},
		WaitingFor: wait.ForLog("").WithStartupTimeout(30 * time.Second),
	}

	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to start typst container: %w", err)
	}

	return &ContainerTypstCompiler{
		ctx:       ctx,
		container: container,
	}, nil
}

// Compile compiles a Typst source file using the container.
func (c *ContainerTypstCompiler) Compile(ctx context.Context, workDir string) error {
	sourcePath := filepath.Join(workDir, sourceFileName)
	if err := c.container.CopyFileToContainer(ctx, sourcePath, "/work/"+sourceFileName, 0644); err != nil {
		return fmt.Errorf("failed to copy source file to container: %w", err)
	}

	dataPath := filepath.Join(workDir, dataFileName)
	if _, err := os.Stat(dataPath); err == nil {
		if copyErr := c.container.CopyFileToContainer(ctx, dataPath, "/work/"+dataFileName, 0644); copyErr != nil {
			return fmt.Errorf("failed to copy data file to container: %w", copyErr)
		}
	}

	exitCode, output, err := c.container.Exec(ctx, []string{
		"typst", "compile", "/work/" + sourceFileName, "/work/" + outputFileName,
	})
	if err != nil {
		return fmt.Errorf("failed to exec typst compile: %w", err)
	}
	if exitCode != 0 {
		buf := new(bytes.Buffer)
		_, _ = buf.ReadFrom(output)
		return fmt.Errorf("compile failed: %s", buf.String())
	}

	reader, err := c.container.CopyFileFromContainer(ctx, "/work/"+outputFileName)
	if err != nil {
		return fmt.Errorf("failed to copy output PDF from container: %w", err)
	}
	defer reader.Close()

	pdfBuf := new(bytes.Buffer)
	if _, bufErr := pdfBuf.ReadFrom(reader); bufErr != nil {
		return fmt.Errorf("failed to read output PDF: %w", bufErr)
	}

	outputPath := filepath.Join(workDir, outputFileName)
	if writeErr := os.WriteFile(outputPath, pdfBuf.Bytes(), 0644); writeErr != nil {
		return fmt.Errorf("failed to write output PDF: %w", writeErr)
	}

	return nil
}

// Close terminates the container.
func (c *ContainerTypstCompiler) Close() error {
	return c.container.Terminate(c.ctx)
}

// testCompiler is the shared compiler instance for all tests.
var testCompiler *ContainerTypstCompiler

// TestMain sets up and tears down the shared container for all tests.
func TestMain(m *testing.M) {
	ctx := context.Background()

	var err error
	testCompiler, err = NewContainerTypstCompiler(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to create container compiler: %v\n", err)
		os.Exit(1)
	}

	code := m.Run()

	if testCompiler != nil {
		_ = testCompiler.Close()
	}

	os.Exit(code)
}

// assertValidPDF verifies that the given bytes represent a valid PDF.
func assertValidPDF(t *testing.T, pdf []byte) {
	t.Helper()

	if len(pdf) == 0 {
		t.Fatal("PDF is empty")
	}

	if !bytes.HasPrefix(pdf, pdfMagicBytes) {
		preview := pdf
		if len(preview) > 10 {
			preview = preview[:10]
		}
		t.Errorf("output does not start with PDF magic bytes, got: %q", preview)
	}
}

// TestCompileTypst_SimpleDocument verifies basic Typst compilation without data.
func TestCompileTypst_SimpleDocument(t *testing.T) {
	source := `= Hello World

This is a simple test document.`

	pdf, err := compileTypstWith(context.Background(), testCompiler, source, nil)
	if err != nil {
		t.Fatalf("compileTypstWith() returned error: %v", err)
	}

	assertValidPDF(t, pdf)
}

// TestCompileTypst_WithData verifies compilation with JSON data.
func TestCompileTypst_WithData(t *testing.T) {
	source := `#let data = json("data.json")

= #data.title

#data.content`

	data := map[string]any{
		"title":   "Test Title",
		"content": "Test content paragraph.",
	}

	pdf, err := compileTypstWith(context.Background(), testCompiler, source, data)
	if err != nil {
		t.Fatalf("compileTypstWith() with data returned error: %v", err)
	}

	assertValidPDF(t, pdf)
}

// TestCompileTypst_WithNestedData verifies compilation with nested JSON structures.
func TestCompileTypst_WithNestedData(t *testing.T) {
	source := `#let data = json("data.json")

= #data.user.name

Email: #data.user.email

Items:
#for item in data.items [
  - #item
]`

	data := map[string]any{
		"user": map[string]any{
			"name":  "John Doe",
			"email": "john@example.com",
		},
		"items": []string{"Item 1", "Item 2", "Item 3"},
	}

	pdf, err := compileTypstWith(context.Background(), testCompiler, source, data)
	if err != nil {
		t.Fatalf("compileTypstWith() with nested data returned error: %v", err)
	}

	assertValidPDF(t, pdf)
}

// TestCompileTypst_InvalidSyntax verifies error handling for invalid Typst syntax.
func TestCompileTypst_InvalidSyntax(t *testing.T) {
	source := `#let x = (`

	_, err := compileTypstWith(context.Background(), testCompiler, source, nil)
	if err == nil {
		t.Fatal("compileTypstWith() with invalid syntax should return error")
	}
}

// TestCompileTypst_MissingDataFile verifies error when template expects missing data.
func TestCompileTypst_MissingDataFile(t *testing.T) {
	source := `#let data = json("data.json")

= #data.title`

	_, err := compileTypstWith(context.Background(), testCompiler, source, nil)
	if err == nil {
		t.Fatal("compileTypstWith() referencing missing data.json should return error")
	}
}

// TestCompileTypst_EmptySource verifies compilation of empty source produces valid PDF.
func TestCompileTypst_EmptySource(t *testing.T) {
	pdf, err := compileTypstWith(context.Background(), testCompiler, "", nil)
	if err != nil {
		t.Fatalf("compileTypstWith() with empty source returned error: %v", err)
	}

	assertValidPDF(t, pdf)
}

// TestCompileTypst_EmptyData verifies compilation with empty data map.
func TestCompileTypst_EmptyData(t *testing.T) {
	source := `#let data = json("data.json")

= Empty Data Test`

	pdf, err := compileTypstWith(context.Background(), testCompiler, source, map[string]any{})
	if err != nil {
		t.Fatalf("compileTypstWith() with empty data returned error: %v", err)
	}

	assertValidPDF(t, pdf)
}

// TestCompileTypst_UsingTestdata verifies compilation using testdata files.
func TestCompileTypst_UsingTestdata(t *testing.T) {
	source, err := os.ReadFile(filepath.Join("testdata", "simple.typ"))
	if err != nil {
		t.Fatalf("failed to read testdata/simple.typ: %v", err)
	}

	// Modify source to use data.json instead of simple.json.
	modifiedSource := bytes.Replace(source, []byte(`json("simple.json")`), []byte(`json("data.json")`), 1)

	data := map[string]any{
		"title":   "Test Document",
		"content": "This is a simple test document for givetypst.",
		"date":    "2026-01-02",
	}

	pdf, err := compileTypstWith(context.Background(), testCompiler, string(modifiedSource), data)
	if err != nil {
		t.Fatalf("compileTypstWith() with testdata returned error: %v", err)
	}

	assertValidPDF(t, pdf)
}
