//go:build tools
// +build tools

package tools

import (
	// source code linting (golangci-lint)
	_ "github.com/golangci/golangci-lint/cmd/golangci-lint"
)
