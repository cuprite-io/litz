package main

import (
	"strings"
	"testing"
)

func TestGenerator_ParseAndGenerate(t *testing.T) {
	// Parse the parent directory which contains test_structs_test.go
	pkgName, structs, err := parseDir("../..")
	if err != nil {
		t.Fatalf("parseDir failed: %v", err)
	}

	// 1. Verify correct package detection
	if pkgName != "litz_test" {
		t.Errorf("Expected package name 'litz_test', got '%s'", pkgName)
	}

	// 2. Verify we parsed our test structures
	foundUserProfile := false
	foundSingleKey := false
	for _, s := range structs {
		if s.Name == "UserProfile" {
			foundUserProfile = true
		}
		if s.Name == "SingleKey" {
			foundSingleKey = true
		}
	}

	if !foundUserProfile {
		t.Error("Expected to find UserProfile struct, but it was missing")
	}
	if !foundSingleKey {
		t.Error("Expected to find SingleKey struct, but it was missing")
	}

	// 3. Verify code generation output
	codeBytes, err := generateCode(pkgName, structs)
	if err != nil {
		t.Fatalf("generateCode failed: %v", err)
	}

	codeStr := string(codeBytes)

	// Verify generated marshalling helpers
	if !strings.Contains(codeStr, "func MarshalUserProfile(") {
		t.Error("Generated code missing MarshalUserProfile function")
	}
	if !strings.Contains(codeStr, "func UnmarshalUserProfile(") {
		t.Error("Generated code missing UnmarshalUserProfile function")
	}
	if !strings.Contains(codeStr, "func MarshalSingleKey(") {
		t.Error("Generated code missing MarshalSingleKey function")
	}
	if !strings.Contains(codeStr, "func UnmarshalSingleKey(") {
		t.Error("Generated code missing UnmarshalSingleKey function")
	}

	// Verify build tags are generated at the very top
	if !strings.HasPrefix(codeStr, "//go:build ") {
		t.Error("Generated code missing go:build constraint header")
	}
}
