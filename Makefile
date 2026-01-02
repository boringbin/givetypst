.PHONY: all tidy vet check fix test test-integration test-all test-coverage clean

# all: Build the project.
all:
	go build -o bin/givetypst .

# tidy: Run the go mod tidy command.
tidy:
	go mod tidy

# vet: Run the vet tool.
vet:
	go vet ./...

# check: Check the code quality.
check:
	golangci-lint run

# fix: Fix the code quality issues.
fix:
	golangci-lint run --fix

# test: Run unit tests (excludes integration tests).
test:
	go test -v -short -race ./...

# test-integration: Run integration tests.
test-integration:
	go test -v -tags=integration ./...

# test-all: Run all tests including integration tests.
test-all:
	go test -v -race ./...
	go test -v -tags=integration ./...

# test-coverage: Run tests with coverage report.
test-coverage:
	go test -v -tags=integration -race -coverprofile=coverage.out ./...
	go tool cover -func=coverage.out

# clean: Clean the project.
clean:
	rm -f bin/givetypst coverage.out coverage.html
