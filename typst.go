package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

const (
	// filePermissions is the permission mode for temporary files.
	// Using 0600 for security (owner read/write only).
	filePermissions = 0600
	// sourceFileName is the name of the Typst source file in the work directory.
	sourceFileName = "main.typ"
	// outputFileName is the name of the compiled PDF file in the work directory.
	outputFileName = "output.pdf"
	// dataFileName is the name of the JSON data file in the work directory.
	dataFileName = "data.json"
)

// TypstCompiler defines the interface for compiling Typst files.
// This allows for dependency injection of different compilation strategies.
type TypstCompiler interface {
	// Compile compiles a Typst source file in the given working directory.
	// The source file is expected to be at workDir/main.typ and the output
	// will be written to workDir/output.pdf.
	Compile(ctx context.Context, workDir string) error
}

// LocalTypstCompiler compiles Typst files using the local typst binary.
type LocalTypstCompiler struct{}

// Compile runs the local typst binary to compile the source file.
func (c *LocalTypstCompiler) Compile(ctx context.Context, workDir string) error {
	sourcePath := filepath.Join(workDir, sourceFileName)
	outputPath := filepath.Join(workDir, outputFileName)

	cmd := exec.CommandContext(ctx, "typst", "compile", sourcePath, outputPath)
	cmd.Dir = workDir

	if output, cmdErr := cmd.CombinedOutput(); cmdErr != nil {
		return fmt.Errorf("compile failed: %s", string(output))
	}

	return nil
}

// compileTypst compiles a Typst source file into a PDF using the default compiler.
func compileTypst(source string, data map[string]any) ([]byte, error) {
	return compileTypstWith(context.Background(), &LocalTypstCompiler{}, source, data)
}

// compileTypstWith compiles a Typst source file into a PDF using the specified compiler.
//
// Will create a temporary directory to work in, write the source file and data to it,
// and then compile the source file into a PDF using the provided compiler.
func compileTypstWith(ctx context.Context, compiler TypstCompiler, source string, data map[string]any) ([]byte, error) {
	// Create a temporary directory to work in.
	// This will be used to store the source file and any data.
	workDir, err := os.MkdirTemp("", "typst-*")
	if err != nil {
		return nil, fmt.Errorf("failed to create temp dir: %w", err)
	}
	defer os.RemoveAll(workDir)

	// If data is provided, marshal it to JSON and write it to a file.
	if data != nil {
		dataBytes, marshalErr := json.MarshalIndent(data, "", "  ")
		if marshalErr != nil {
			return nil, fmt.Errorf("failed to marshal data: %w", marshalErr)
		}
		dataPath := filepath.Join(workDir, dataFileName)
		if writeErr := os.WriteFile(dataPath, dataBytes, filePermissions); writeErr != nil {
			return nil, fmt.Errorf("failed to write data file: %w", writeErr)
		}
	}

	// Write the source file to the temporary directory.
	sourcePath := filepath.Join(workDir, sourceFileName)
	if writeErr := os.WriteFile(sourcePath, []byte(source), filePermissions); writeErr != nil {
		return nil, fmt.Errorf("failed to write source file: %w", writeErr)
	}

	// Compile the source file.
	if compileErr := compiler.Compile(ctx, workDir); compileErr != nil {
		return nil, compileErr
	}

	// Read the output file from the temporary directory.
	outputPath := filepath.Join(workDir, outputFileName)
	pdfData, readErr := os.ReadFile(outputPath)
	if readErr != nil {
		return nil, fmt.Errorf("failed to read output PDF: %w", readErr)
	}

	return pdfData, nil
}
