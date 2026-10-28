package main

import (
	"go/ast"
	"go/parser"
	"go/token"
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

func parseSource(t *testing.T, src string) (string, []StructInfo, error) {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "test.go", src, parser.ParseComments)
	if err != nil {
		t.Fatalf("failed to parse test source: %v", err)
	}
	pkgs := map[string]*ast.Package{
		"testpkg": {
			Name:  "testpkg",
			Files: map[string]*ast.File{"test.go": f},
		},
	}
	return parsePackages(pkgs)
}

func TestGenerator_TypeValidation(t *testing.T) {
	tests := []struct {
		name        string
		src         string
		wantErrSub  string
		wantSuccess bool
	}{
		{
			name: "reject slice of any",
			src: `package testpkg
//litz:generate
type BadSliceAny struct {
	Items []any
}`,
			wantErrSub: "unsupported slice type '[]any'",
		},
		{
			name: "reject slice of structs",
			src: `package testpkg
type Other struct {
	Val int
}
//litz:generate
type BadSliceStruct struct {
	Items []Other
}`,
			wantErrSub: "unsupported slice of structs '[]Other'",
		},
		{
			name: "reject non-pointer struct field",
			src: `package testpkg
type Nested struct {
	Val int
}
//litz:generate
type BadDirectStruct struct {
	N Nested
}`,
			wantErrSub: "has non-pointer struct type \"Nested\"",
		},
		{
			name: "reject slice of pointers",
			src: `package testpkg
type Target struct {
	Val int
}
//litz:generate
type BadPointerSlice struct {
	Items []*Target
}`,
			wantErrSub: "unsupported slice element type *ast.StarExpr",
		},
		{
			name: "reject pointer to primitive",
			src: `package testpkg
//litz:generate
type BadPrimPtr struct {
	Val *int
}`,
			wantErrSub: "has pointer to primitive type '*int'",
		},
		{
			name: "reject fixed-size array",
			src: `package testpkg
//litz:generate
type BadArray struct {
	Data [16]byte
}`,
			wantErrSub: "has fixed-size array type",
		},
		{
			name: "reject map field",
			src: `package testpkg
//litz:generate
type BadMap struct {
	Tags map[string]string
}`,
			wantErrSub: "maps, channels, functions, and selector types are unsupported",
		},
		{
			name: "reject embedded field",
			src: `package testpkg
type Base struct {
	ID uint64
}
//litz:generate
type BadEmbedded struct {
	Base
}`,
			wantErrSub: "contains an embedded field which is unsupported",
		},
		{
			name: "reject pointer to unmarked struct",
			src: `package testpkg
type Unmarked struct {
	Val int
}
//litz:generate
type BadRef struct {
	Child *Unmarked
}`,
			wantErrSub: "references Unmarked, but Unmarked is not marked with //litz:generate",
		},
		{
			name: "accept named numeric type alias",
			src: `package testpkg
type UserID uint64
type Status uint8
//litz:generate
type GoodAliases struct {
	ID    UserID
	State Status
}`,
			wantSuccess: true,
		},
		{
			name: "accept slice of named numeric type alias",
			src: `package testpkg
type TagID uint32
//litz:generate
type GoodSliceAlias struct {
	Tags []TagID
}`,
			wantSuccess: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			pkgName, structs, err := parseSource(t, tc.src)
			if tc.wantSuccess {
				if err != nil {
					t.Fatalf("unexpected error for %s: %v", tc.name, err)
				}
				if len(structs) != 1 {
					t.Fatalf("expected 1 struct, got %d", len(structs))
				}
				if _, err := generateCode(pkgName, structs); err != nil {
					t.Fatalf("generateCode failed for valid aliases: %v", err)
				}
			} else {
				if err == nil {
					t.Fatalf("expected error containing %q, got nil", tc.wantErrSub)
				}
				if !strings.Contains(err.Error(), tc.wantErrSub) {
					t.Fatalf("error %q does not contain %q", err.Error(), tc.wantErrSub)
				}
			}
		})
	}
}
