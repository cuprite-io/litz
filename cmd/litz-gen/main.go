package main

import (
	"bytes"
	"flag"
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"log"
	"os"
	"path/filepath"
	"strings"
)

type FieldType int

const (
	TypePrimitive FieldType = iota
	TypeString
	TypeBytes
	TypeSlicePrimitive
	TypeSliceString
	TypeAny
	TypeStructPtr
)

type FieldInfo struct {
	Name      string
	TypeStr   string
	BaseType  string // Used for slices e.g. "int" for "[]int"
	Kind      FieldType
	Size      int    // size of primitive type in bytes
}

type StructInfo struct {
	Name   string
	Fields []FieldInfo
}

func main() {
	var inputDir = flag.String("dir", ".", "Directory to parse Go files")
	var outputFile = flag.String("out", "litz_gen_test.go", "Output generated file name")
	flag.Parse()

	absDir, err := filepath.Abs(*inputDir)
	if err != nil {
		log.Fatalf("Failed to resolve absolute path: %v", err)
	}

	packageName, structs, err := parseDir(absDir)
	if err != nil {
		log.Fatalf("Parsing error: %v", err)
	}

	if len(structs) == 0 {
		fmt.Printf("No structs marked with //litz:generate found in %s\n", absDir)
		return
	}

	code, err := generateCode(packageName, structs)
	if err != nil {
		log.Fatalf("Code generation error: %v", err)
	}

	outPath := filepath.Join(absDir, *outputFile)
	err = os.WriteFile(outPath, code, 0644)
	if err != nil {
		log.Fatalf("Failed to write generated file: %v", err)
	}

	fmt.Printf("Successfully generated Litz serialization code for %d structs in %s\n", len(structs), outPath)
}

func parseDir(dir string) (string, []StructInfo, error) {
	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, dir, nil, parser.ParseComments)
	if err != nil {
		return "", nil, err
	}

	var packageName string
	var structs []StructInfo

	for _, pkg := range pkgs {
		for _, file := range pkg.Files {
			for _, decl := range file.Decls {
				genDecl, ok := decl.(*ast.GenDecl)
				if !ok || genDecl.Tok != token.TYPE {
					continue
				}

				// Check if declaration has the trigger comment
				hasTrigger := false
				if genDecl.Doc != nil {
					for _, comment := range genDecl.Doc.List {
						if strings.Contains(comment.Text, "//litz:generate") {
							hasTrigger = true
							break
						}
					}
				}

				if !hasTrigger {
					continue
				}

				packageName = pkg.Name

				for _, spec := range genDecl.Specs {
					typeSpec, ok := spec.(*ast.TypeSpec)
					if !ok {
						continue
					}

					structType, ok := typeSpec.Type.(*ast.StructType)
					if !ok {
						continue
					}

					info := StructInfo{
						Name: typeSpec.Name.Name,
					}

					for _, field := range structType.Fields.List {
						if len(field.Names) == 0 {
							log.Printf("WARNING: struct %s contains embedded struct which is currently unsupported and skipped", info.Name)
							continue // Skip embedded fields for simplicity
						}

						for _, name := range field.Names {
							fieldInfo, err := parseField(name.Name, field.Type)
							if err != nil {
								return "", nil, fmt.Errorf("field %s: %w", name.Name, err)
							}
							info.Fields = append(info.Fields, fieldInfo)
						}
					}

					structs = append(structs, info)
				}
			}
		}
	}

	return packageName, structs, nil
}

func parseField(name string, expr ast.Expr) (FieldInfo, error) {
	switch t := expr.(type) {
	case *ast.Ident:
		typeStr := t.Name
		switch typeStr {
		case "int", "int64", "uint64", "float64":
			return FieldInfo{Name: name, TypeStr: typeStr, Kind: TypePrimitive, Size: 8}, nil
		case "int32", "uint32", "float32":
			return FieldInfo{Name: name, TypeStr: typeStr, Kind: TypePrimitive, Size: 4}, nil
		case "int16", "uint16":
			return FieldInfo{Name: name, TypeStr: typeStr, Kind: TypePrimitive, Size: 2}, nil
		case "int8", "uint8", "byte", "bool":
			return FieldInfo{Name: name, TypeStr: typeStr, Kind: TypePrimitive, Size: 1}, nil
		case "string":
			return FieldInfo{Name: name, TypeStr: "string", Kind: TypeString}, nil
		case "any":
			return FieldInfo{Name: name, TypeStr: "any", Kind: TypeAny}, nil
		default:
			// Treat other identifiers as primitives for custom types (e.g. enum/int aliases)
			return FieldInfo{Name: name, TypeStr: typeStr, Kind: TypePrimitive, Size: 8}, nil
		}

	case *ast.InterfaceType:
		// interface{} is treated as TypeAny
		return FieldInfo{Name: name, TypeStr: "any", Kind: TypeAny}, nil

	case *ast.ArrayType:
		// Slices
		eltIdent, ok := t.Elt.(*ast.Ident)
		if !ok {
			return FieldInfo{}, fmt.Errorf("unsupported slice element type")
		}
		baseType := eltIdent.Name
		if baseType == "byte" {
			return FieldInfo{Name: name, TypeStr: "[]byte", BaseType: "byte", Kind: TypeBytes}, nil
		}
		if baseType == "string" {
			return FieldInfo{Name: name, TypeStr: "[]string", BaseType: "string", Kind: TypeSliceString}, nil
		}
		return FieldInfo{Name: name, TypeStr: "[]" + baseType, BaseType: baseType, Kind: TypeSlicePrimitive}, nil

	case *ast.StarExpr:
		// Pointer to struct
		structIdent, ok := t.X.(*ast.Ident)
		if !ok {
			return FieldInfo{}, fmt.Errorf("unsupported pointer type")
		}
		return FieldInfo{Name: name, TypeStr: "*" + structIdent.Name, BaseType: structIdent.Name, Kind: TypeStructPtr}, nil

	default:
		return FieldInfo{}, fmt.Errorf("unsupported AST expression type")
	}
}

func generateCode(pkgName string, structs []StructInfo) ([]byte, error) {
	var buf bytes.Buffer

	fmt.Fprintf(&buf, "//go:build amd64 || arm64 || 386 || arm || wasm || mips64le || ppc64le || riscv64\n\n// Code generated by litz-gen. DO NOT EDIT.\npackage %s\n\n", pkgName)
	buf.WriteString("import (\n")
	buf.WriteString("\t\"strings\"\n")
	buf.WriteString("\t\"unsafe\"\n")
	if pkgName != "litz" {
		buf.WriteString("\t\"github.com/cuprite-io/litz\"\n")
	}
	buf.WriteString(")\n\n")

	prefix := ""
	if pkgName != "litz" {
		prefix = "litz."
	}

	for _, s := range structs {
		mirrorName := strings.ToLower(s.Name[0:1]) + s.Name[1:] + "LitzMirror"

		// 1. Generate the Mirror Struct
		fmt.Fprintf(&buf, "// %s is the GC-safe layout mirror for %s\n", mirrorName, s.Name)
		fmt.Fprintf(&buf, "type %s struct {\n", mirrorName)
		for _, f := range s.Fields {
			switch f.Kind {
			case TypePrimitive:
				fmt.Fprintf(&buf, "\t%s %s\n", f.Name, f.TypeStr)
			case TypeString:
				fmt.Fprintf(&buf, "\t%s struct { offset uintptr; length int }\n", f.Name)
			case TypeAny:
				fmt.Fprintf(&buf, "\t%s struct { offset uintptr; length uint32; valType uint32 }\n", f.Name)
			case TypeBytes, TypeSlicePrimitive, TypeSliceString:
				fmt.Fprintf(&buf, "\t%s struct { offset uintptr; length int; capacity int }\n", f.Name)
			case TypeStructPtr:
				fmt.Fprintf(&buf, "\t%s uintptr\n", f.Name)
			}
		}
		buf.WriteString("}\n\n")

		// Add Compile-Time size assertions to verify structural layout matches between user type and mirror type
		buf.WriteString("// Compile-time size assertions to guarantee layout identity\n")
		fmt.Fprintf(&buf, "var _ [int(unsafe.Sizeof(%s{})) - int(unsafe.Sizeof(%s{}))]byte\n", s.Name, mirrorName)
		fmt.Fprintf(&buf, "var _ [int(unsafe.Sizeof(%s{})) - int(unsafe.Sizeof(%s{}))]byte\n", mirrorName, s.Name)
		for _, f := range s.Fields {
			fmt.Fprintf(&buf, "var _ [int(unsafe.Offsetof(%s{}.%s)) - int(unsafe.Offsetof(%s{}.%s))]byte\n", s.Name, f.Name, mirrorName, f.Name)
			fmt.Fprintf(&buf, "var _ [int(unsafe.Offsetof(%s{}.%s)) - int(unsafe.Offsetof(%s{}.%s))]byte\n", mirrorName, f.Name, s.Name, f.Name)
		}
		buf.WriteString("\n")

		// 2. Generate Marshal Method
		fmt.Fprintf(&buf, "func Marshal%s(u *%s, buf []byte) ([]byte, error) {\n", s.Name, s.Name)
		fmt.Fprintf(&buf, "\tfixedSize := int(unsafe.Sizeof(%s{}))\n", mirrorName)
		buf.WriteString("\ttotalSize := 8 + fixedSize\n\n")

		// Calculate sizes of variable fields
		for _, f := range s.Fields {
			switch f.Kind {
			case TypeString:
				fmt.Fprintf(&buf, "\t%sLen := len(u.%s)\n", f.Name, f.Name)
				fmt.Fprintf(&buf, "\tif totalSize + %sLen < totalSize { return nil, %sErrSizeOverflow }\n", f.Name, prefix)
				fmt.Fprintf(&buf, "\ttotalSize += %sLen\n", f.Name)
			case TypeBytes:
				fmt.Fprintf(&buf, "\t%sLen := len(u.%s)\n", f.Name, f.Name)
				fmt.Fprintf(&buf, "\tif totalSize + %sLen < totalSize { return nil, %sErrSizeOverflow }\n", f.Name, prefix)
				fmt.Fprintf(&buf, "\ttotalSize += %sLen\n", f.Name)
			case TypeSlicePrimitive:
				fmt.Fprintf(&buf, "\t%sLen := len(u.%s)\n", f.Name, f.Name)
				fmt.Fprintf(&buf, "\tvar %sDummy %s\n", f.Name, f.BaseType)
				fmt.Fprintf(&buf, "\t%sSize := %sLen * int(unsafe.Sizeof(%sDummy))\n", f.Name, f.Name, f.Name)
				fmt.Fprintf(&buf, "\tif %sLen > 0 && %sSize / %sLen != int(unsafe.Sizeof(%sDummy)) { return nil, %sErrSizeOverflow }\n", f.Name, f.Name, f.Name, f.Name, prefix)
				fmt.Fprintf(&buf, "\tif totalSize + %sSize < totalSize { return nil, %sErrSizeOverflow }\n", f.Name, prefix)
				fmt.Fprintf(&buf, "\ttotalSize += %sSize\n", f.Name)
			case TypeSliceString:
				fmt.Fprintf(&buf, "\t%sLen := len(u.%s)\n", f.Name, f.Name)
				buf.WriteString("\t// Slice array of string headers (size of Go string header)\n")
				fmt.Fprintf(&buf, "\t%sHeaderSize := %sLen * int(unsafe.Sizeof(\"\"))\n", f.Name, f.Name)
				fmt.Fprintf(&buf, "\tif %sLen > 0 && %sHeaderSize / %sLen != int(unsafe.Sizeof(\"\")) { return nil, %sErrSizeOverflow }\n", f.Name, f.Name, f.Name, prefix)
				fmt.Fprintf(&buf, "\tif totalSize + %sHeaderSize < totalSize { return nil, %sErrSizeOverflow }\n", f.Name, prefix)
				fmt.Fprintf(&buf, "\ttotalSize += %sHeaderSize\n", f.Name)
				buf.WriteString("\t// Add string backing bytes\n")
				fmt.Fprintf(&buf, "\tfor i := 0; i < %sLen; i++ {\n", f.Name)
				fmt.Fprintf(&buf, "\t\tstrLen := len(u.%s[i])\n", f.Name)
				fmt.Fprintf(&buf, "\t\tif totalSize + strLen < totalSize { return nil, %sErrSizeOverflow }\n", prefix)
				fmt.Fprintf(&buf, "\t\ttotalSize += strLen\n")
				buf.WriteString("\t}\n")
			case TypeAny:
				fmt.Fprintf(&buf, "\t%sBytes, %sType, err := %sMarshalAny(u.%s)\n", f.Name, f.Name, prefix, f.Name)
				buf.WriteString("\tif err != nil { return nil, err }\n")
				fmt.Fprintf(&buf, "\t%sLen := len(%sBytes)\n", f.Name, f.Name)
				fmt.Fprintf(&buf, "\tif totalSize + %sLen < totalSize { return nil, %sErrSizeOverflow }\n", f.Name, prefix)
				fmt.Fprintf(&buf, "\ttotalSize += %sLen\n", f.Name)
			case TypeStructPtr:
				fmt.Fprintf(&buf, "\tvar %sBytes []byte\n", f.Name)
				fmt.Fprintf(&buf, "\tif u.%s != nil {\n", f.Name)
				buf.WriteString("\t\tvar err error\n")
				fmt.Fprintf(&buf, "\t\t%sBytes, err = Marshal%s(u.%s, nil)\n", f.Name, f.BaseType, f.Name)
				buf.WriteString("\t\tif err != nil { return nil, err }\n")
				buf.WriteString("\t}\n")
				fmt.Fprintf(&buf, "\t%sLen := len(%sBytes)\n", f.Name, f.Name)
				fmt.Fprintf(&buf, "\tif totalSize + %sLen < totalSize { return nil, %sErrSizeOverflow }\n", f.Name, prefix)
				fmt.Fprintf(&buf, "\ttotalSize += %sLen\n", f.Name)
			}
		}

		buf.WriteString("\n\t// Ensure output buffer is aligned and large enough\n")
		buf.WriteString("\tif cap(buf) < totalSize || (cap(buf) > 0 && uintptr(unsafe.Pointer(&buf[0]))%8 != 0) {\n")
		fmt.Fprintf(&buf, "\t\tbuf = %sAlignedBuffer(totalSize)\n", prefix)
		buf.WriteString("\t}\n")
		buf.WriteString("\tbuf = buf[:totalSize]\n\n")

		buf.WriteString("\t// Write 8-byte format signature: 'LTZ0' + Version 1\n")
		buf.WriteString("\t*(*[4]byte)(unsafe.Pointer(&buf[0])) = [4]byte{'L', 'T', 'Z', '0'}\n")
		buf.WriteString("\tbuf[4] = 1\n")
		buf.WriteString("\tbuf[5] = 0\n")
		buf.WriteString("\tbuf[6] = 0\n")
		buf.WriteString("\tbuf[7] = 0\n\n")

		buf.WriteString("\t// Write directly to the buffer's mirror pointer (O1 Optimization: No stack allocation/double-copy!)\n")
		buf.WriteString("\t// Zero the fixed size header to prevent padding data leaks (Issue 1)\n")
		fmt.Fprintf(&buf, "\t*(*%s)(unsafe.Pointer(&buf[8])) = %s{}\n", mirrorName, mirrorName)
		fmt.Fprintf(&buf, "\tmirror := (*%s)(unsafe.Pointer(&buf[8]))\n\n", mirrorName)

		// Primitive assignments directly to the buffer
		for _, f := range s.Fields {
			if f.Kind == TypePrimitive {
				fmt.Fprintf(&buf, "\tmirror.%s = u.%s\n", f.Name, f.Name)
			}
		}

		// Write offsets and copy variable payloads
		hasVariableFields := false
		for _, f := range s.Fields {
			if f.Kind != TypePrimitive {
				hasVariableFields = true
				break
			}
		}
		if hasVariableFields {
			buf.WriteString("\tcurrentOffset := uintptr(8 + fixedSize)\n\n")
		}

		for _, f := range s.Fields {
			switch f.Kind {
			case TypeString:
				fmt.Fprintf(&buf, "\tmirror.%s.offset = currentOffset\n", f.Name)
				fmt.Fprintf(&buf, "\tmirror.%s.length = %sLen\n", f.Name, f.Name)
				fmt.Fprintf(&buf, "\tcopy(buf[currentOffset:], u.%s)\n", f.Name)
				fmt.Fprintf(&buf, "\tcurrentOffset += uintptr(%sLen)\n\n", f.Name)
			case TypeBytes:
				fmt.Fprintf(&buf, "\tmirror.%s.offset = currentOffset\n", f.Name)
				fmt.Fprintf(&buf, "\tmirror.%s.length = %sLen\n", f.Name, f.Name)
				fmt.Fprintf(&buf, "\tmirror.%s.capacity = cap(u.%s)\n", f.Name, f.Name)
				fmt.Fprintf(&buf, "\tcopy(buf[currentOffset:], u.%s)\n", f.Name)
				fmt.Fprintf(&buf, "\tcurrentOffset += uintptr(%sLen)\n\n", f.Name)
			case TypeSlicePrimitive:
				fmt.Fprintf(&buf, "\tmirror.%s.offset = currentOffset\n", f.Name)
				fmt.Fprintf(&buf, "\tmirror.%s.length = %sLen\n", f.Name, f.Name)
				fmt.Fprintf(&buf, "\tmirror.%s.capacity = cap(u.%s)\n", f.Name, f.Name)
				fmt.Fprintf(&buf, "\tif %sLen > 0 {\n", f.Name)
				fmt.Fprintf(&buf, "\t\tvarSize := %sLen * int(unsafe.Sizeof(%sDummy))\n", f.Name, f.Name)
				fmt.Fprintf(&buf, "\t\tsliceBytes := unsafe.Slice((*byte)(unsafe.Pointer(&u.%s[0])), varSize)\n", f.Name)
				buf.WriteString("\t\tcopy(buf[currentOffset:], sliceBytes)\n")
				fmt.Fprintf(&buf, "\t\tcurrentOffset += uintptr(varSize)\n")
				buf.WriteString("\t}\n\n")
			case TypeSliceString:
				fmt.Fprintf(&buf, "\tmirror.%s.offset = currentOffset\n", f.Name)
				fmt.Fprintf(&buf, "\tmirror.%s.length = %sLen\n", f.Name, f.Name)
				fmt.Fprintf(&buf, "\tmirror.%s.capacity = %sLen\n", f.Name, f.Name)
				fmt.Fprintf(&buf, "\tif %sLen > 0 {\n", f.Name)
				buf.WriteString("\t\t// First reserve space for slice of string headers (16B each)\n")
				buf.WriteString("\t\tstrArrayOffset := currentOffset\n")
				fmt.Fprintf(&buf, "\t\tcurrentOffset += uintptr(%sLen * 16)\n", f.Name)
				buf.WriteString("\t\t// Write string bytes and fill array headers\n")
				fmt.Fprintf(&buf, "\t\tfor i := 0; i < %sLen; i++ {\n", f.Name)
				fmt.Fprintf(&buf, "\t\t\tstrLen := len(u.%s[i])\n", f.Name)
				fmt.Fprintf(&buf, "\t\t\tcopy(buf[currentOffset:], u.%s[i])\n", f.Name)
				buf.WriteString("\t\t\t// Fill header in slice array\n")
				buf.WriteString("\t\t\tentryPtr := strArrayOffset + uintptr(i*16)\n")
				buf.WriteString("\t\t\t*(*uintptr)(unsafe.Pointer(&buf[entryPtr])) = currentOffset\n")
				buf.WriteString("\t\t\t*(*int)(unsafe.Pointer(&buf[entryPtr+8])) = strLen\n")
				buf.WriteString("\t\t\tcurrentOffset += uintptr(strLen)\n")
				buf.WriteString("\t\t}\n")
				buf.WriteString("\t}\n\n")
			case TypeAny:
				fmt.Fprintf(&buf, "\tmirror.%s.offset = currentOffset\n", f.Name)
				fmt.Fprintf(&buf, "\tmirror.%s.length = uint32(%sLen)\n", f.Name, f.Name)
				fmt.Fprintf(&buf, "\tmirror.%s.valType = uint32(%sType)\n", f.Name, f.Name)
				fmt.Fprintf(&buf, "\tcopy(buf[currentOffset:], %sBytes)\n", f.Name)
				fmt.Fprintf(&buf, "\tcurrentOffset += uintptr(%sLen)\n\n", f.Name)
			case TypeStructPtr:
				fmt.Fprintf(&buf, "\tif %sLen > 0 {\n", f.Name)
				fmt.Fprintf(&buf, "\t\tmirror.%s = currentOffset\n", f.Name)
				fmt.Fprintf(&buf, "\t\tcopy(buf[currentOffset:], %sBytes)\n", f.Name)
				fmt.Fprintf(&buf, "\t\tcurrentOffset += uintptr(%sLen)\n", f.Name)
				buf.WriteString("\t} else {\n")
				fmt.Fprintf(&buf, "\t\tmirror.%s = 0\n", f.Name)
				buf.WriteString("\t}\n\n")
			}
		}

		buf.WriteString("\treturn buf, nil\n")
		buf.WriteString("}\n\n")

		// 3. Generate Unmarshal Method
		fmt.Fprintf(&buf, "// Unmarshal%s deserializes the buffer using SBC-IPR.\n", s.Name)
		buf.WriteString("// WARNING: Unmarshaled strings and slices point directly into buf.\n")
		buf.WriteString("// The input buf MUST outlive the returned structure u to prevent use-after-free corruption.\n")
		fmt.Fprintf(&buf, "func Unmarshal%s(buf []byte, u *%s) error {\n", s.Name, s.Name)
		fmt.Fprintf(&buf, "\tif len(buf) < 8 {\n")
		fmt.Fprintf(&buf, "\t\treturn %sErrBufferTooShort\n", prefix)
		buf.WriteString("\t}\n")
		fmt.Fprintf(&buf, "\tif *(*[4]byte)(unsafe.Pointer(&buf[0])) != [4]byte{'L', 'T', 'Z', '0'} || buf[4] != 1 {\n")
		fmt.Fprintf(&buf, "\t\treturn %sErrInvalidHeader\n", prefix)
		buf.WriteString("\t}\n")
		fmt.Fprintf(&buf, "\tmirror := (*%s)(unsafe.Pointer(&buf[8]))\n\n", mirrorName)

		// Bounds checking utilizing sentinel errors (guarded by offset bounds checking for schema evolution)
		for _, f := range s.Fields {
			fmt.Fprintf(&buf, "\tif 8 + unsafe.Offsetof(mirror.%s) + unsafe.Sizeof(mirror.%s) <= uintptr(len(buf)) {\n", f.Name, f.Name)
			switch f.Kind {
			case TypeString:
				fmt.Fprintf(&buf, "\t\tif mirror.%s.offset > uintptr(len(buf)) || mirror.%s.length < 0 || mirror.%s.length > len(buf)-int(mirror.%s.offset) {\n", f.Name, f.Name, f.Name, f.Name)
				fmt.Fprintf(&buf, "\t\t\treturn %sErrStringOutOfBounds\n", prefix)
				buf.WriteString("\t\t}\n")
			case TypeAny:
				fmt.Fprintf(&buf, "\t\tif mirror.%s.offset > uintptr(len(buf)) || mirror.%s.length < 0 || int(mirror.%s.length) > len(buf)-int(mirror.%s.offset) {\n", f.Name, f.Name, f.Name, f.Name)
				fmt.Fprintf(&buf, "\t\t\treturn %sErrStringOutOfBounds\n", prefix)
				buf.WriteString("\t\t}\n")
			case TypeBytes:
				fmt.Fprintf(&buf, "\t\tif mirror.%s.offset > uintptr(len(buf)) || mirror.%s.length < 0 || mirror.%s.length > len(buf)-int(mirror.%s.offset) {\n", f.Name, f.Name, f.Name, f.Name)
				fmt.Fprintf(&buf, "\t\t\treturn %sErrSliceOutOfBounds\n", prefix)
				buf.WriteString("\t\t}\n")
			case TypeSlicePrimitive:
				fmt.Fprintf(&buf, "\t\tvar %sDummy %s\n", f.Name, f.BaseType)
				fmt.Fprintf(&buf, "\t\tif mirror.%s.offset > uintptr(len(buf)) || mirror.%s.length < 0 || mirror.%s.length > (len(buf)-int(mirror.%s.offset))/int(unsafe.Sizeof(%sDummy)) {\n", f.Name, f.Name, f.Name, f.Name, f.Name)
				fmt.Fprintf(&buf, "\t\t\treturn %sErrSliceOutOfBounds\n", prefix)
				buf.WriteString("\t\t}\n")
			case TypeSliceString:
				fmt.Fprintf(&buf, "\t\tif mirror.%s.offset > uintptr(len(buf)) || mirror.%s.length < 0 || mirror.%s.length > (len(buf)-int(mirror.%s.offset))/int(unsafe.Sizeof(\"\")) {\n", f.Name, f.Name, f.Name, f.Name)
				fmt.Fprintf(&buf, "\t\t\treturn %sErrSliceOutOfBounds\n", prefix)
				buf.WriteString("\t\t}\n")
			case TypeStructPtr:
				// Secure nested bounds checks: check start offset AND full nested struct size
				fmt.Fprintf(&buf, "\t\tif mirror.%s > uintptr(len(buf)) || (mirror.%s > 0 && uintptr(len(buf))-mirror.%s < unsafe.Sizeof(%s{})) {\n", f.Name, f.Name, f.Name, getMirrorName(f.BaseType))
				fmt.Fprintf(&buf, "\t\t\treturn %sErrPointerOutOfBounds\n", prefix)
				buf.WriteString("\t\t}\n")
			}
			buf.WriteString("\t}\n")
		}
		buf.WriteString("\n")

		// Assign Primitives individually (guarded by offset bounds checking for schema evolution)
		for _, f := range s.Fields {
			if f.Kind == TypePrimitive {
				fmt.Fprintf(&buf, "\tif 8 + unsafe.Offsetof(mirror.%s) + unsafe.Sizeof(mirror.%s) <= uintptr(len(buf)) {\n", f.Name, f.Name)
				fmt.Fprintf(&buf, "\t\tu.%s = mirror.%s\n", f.Name, f.Name)
				buf.WriteString("\t}\n")
			}
		}

		// Swizzle variable fields using inlined pointer arithmetic and unsafe.Add (O5 & O11 Optimization)
		for _, f := range s.Fields {
			if f.Kind == TypePrimitive {
				continue
			}
			fmt.Fprintf(&buf, "\tif 8 + unsafe.Offsetof(mirror.%s) + unsafe.Sizeof(mirror.%s) <= uintptr(len(buf)) {\n", f.Name, f.Name)
			switch f.Kind {
			case TypeString:
				fmt.Fprintf(&buf, "\t\tif mirror.%s.length > 0 {\n", f.Name)
				fmt.Fprintf(&buf, "\t\t\tu.%s = unsafe.String((*byte)(unsafe.Add(unsafe.Pointer(&buf[0]), mirror.%s.offset)), mirror.%s.length)\n", f.Name, f.Name, f.Name)
				buf.WriteString("\t\t} else {\n")
				fmt.Fprintf(&buf, "\t\t\tu.%s = \"\"\n", f.Name)
				buf.WriteString("\t\t}\n")
			case TypeBytes:
				fmt.Fprintf(&buf, "\t\tif mirror.%s.length > 0 {\n", f.Name)
				fmt.Fprintf(&buf, "\t\t\tu.%s = unsafe.Slice((*byte)(unsafe.Add(unsafe.Pointer(&buf[0]), mirror.%s.offset)), mirror.%s.length)\n", f.Name, f.Name, f.Name)
				buf.WriteString("\t\t} else {\n")
				fmt.Fprintf(&buf, "\t\t\tu.%s = nil\n", f.Name)
				buf.WriteString("\t\t}\n")
			case TypeSlicePrimitive:
				fmt.Fprintf(&buf, "\t\tif mirror.%s.length > 0 {\n", f.Name)
				fmt.Fprintf(&buf, "\t\t\tu.%s = unsafe.Slice((*%s)(unsafe.Add(unsafe.Pointer(&buf[0]), mirror.%s.offset)), mirror.%s.length)\n", f.Name, f.BaseType, f.Name, f.Name)
				buf.WriteString("\t\t} else {\n")
				fmt.Fprintf(&buf, "\t\t\tu.%s = nil\n", f.Name)
				buf.WriteString("\t\t}\n")
			case TypeSliceString:
				fmt.Fprintf(&buf, "\t\tif mirror.%s.length > 0 {\n", f.Name)
				fmt.Fprintf(&buf, "\t\t\tstrHeaders := unsafe.Slice((*struct{ offset uintptr; length int })(unsafe.Add(unsafe.Pointer(&buf[0]), mirror.%s.offset)), mirror.%s.length)\n", f.Name, f.Name)
				// O1: Range validation scan to find maxExtent in 1 pass
				buf.WriteString("\t\t\tmaxExtent := 0\n")
				buf.WriteString("\t\t\tfor i := 0; i < len(strHeaders); i++ {\n")
				buf.WriteString("\t\t\t\tendOffset := int(strHeaders[i].offset) + strHeaders[i].length\n")
				buf.WriteString("\t\t\t\tif endOffset > maxExtent {\n")
				buf.WriteString("\t\t\t\t\tmaxExtent = endOffset\n")
				buf.WriteString("\t\t\t\t}\n")
				buf.WriteString("\t\t\t}\n")
				fmt.Fprintf(&buf, "\t\t\tif maxExtent > len(buf) {\n")
				fmt.Fprintf(&buf, "\t\t\t\treturn %sErrStringOutOfBounds\n", prefix)
				fmt.Fprintf(&buf, "\t\t\t}\n")
				fmt.Fprintf(&buf, "\t\t\tslice := make([]string, len(strHeaders))\n")
				buf.WriteString("\t\t\tfor i := 0; i < len(strHeaders); i++ {\n")
				buf.WriteString("\t\t\t\tif strHeaders[i].length > 0 {\n")
				fmt.Fprintf(&buf, "\t\t\t\t\tslice[i] = unsafe.String((*byte)(unsafe.Add(unsafe.Pointer(&buf[0]), strHeaders[i].offset)), strHeaders[i].length)\n")
				buf.WriteString("\t\t\t\t}\n")
				buf.WriteString("\t\t\t}\n")
				fmt.Fprintf(&buf, "\t\t\tu.%s = slice\n", f.Name)
				buf.WriteString("\t\t} else {\n")
				fmt.Fprintf(&buf, "\t\t\tu.%s = nil\n", f.Name)
				buf.WriteString("\t\t}\n")
			case TypeAny:
				fmt.Fprintf(&buf, "\t\tif mirror.%s.length > 0 {\n", f.Name)
				fmt.Fprintf(&buf, "\t\t\tu.%s = %sNewDynamic(buf[mirror.%s.offset : mirror.%s.offset+uintptr(mirror.%s.length)], uint8(mirror.%s.valType))\n", f.Name, prefix, f.Name, f.Name, f.Name, f.Name)
				buf.WriteString("\t\t} else {\n")
				fmt.Fprintf(&buf, "\t\t\tu.%s = nil\n", f.Name)
				buf.WriteString("\t\t}\n")
			case TypeStructPtr:
				fmt.Fprintf(&buf, "\t\tif mirror.%s > 0 {\n", f.Name)
				fmt.Fprintf(&buf, "\t\t\tif u.%s == nil {\n", f.Name)
				fmt.Fprintf(&buf, "\t\t\t\tu.%s = new(%s)\n", f.Name, f.BaseType)
				buf.WriteString("\t\t\t}\n")
				fmt.Fprintf(&buf, "\t\t\terr := Unmarshal%s(buf[mirror.%s:], u.%s)\n", f.BaseType, f.Name, f.Name)
				buf.WriteString("\t\t\tif err != nil { return err }\n")
				buf.WriteString("\t\t} else {\n")
				fmt.Fprintf(&buf, "\t\t\tu.%s = nil\n", f.Name)
				buf.WriteString("\t\t}\n")
			}
			buf.WriteString("\t}\n")
		}

		// Generate Clone method for deep-copying (addresses issue #5: CloneAny deep copy escape hatch)
		fmt.Fprintf(&buf, "\n\treturn nil\n}\n\n")
		fmt.Fprintf(&buf, "// Clone creates a deep-copy of %s, allocating fresh memory for all string and slice fields.\n", s.Name)
		fmt.Fprintf(&buf, "func (u *%s) Clone() *%s {\n", s.Name, s.Name)
		fmt.Fprintf(&buf, "\tres := new(%s)\n", s.Name)
		for _, f := range s.Fields {
			switch f.Kind {
			case TypePrimitive:
				fmt.Fprintf(&buf, "\tres.%s = u.%s\n", f.Name, f.Name)
			case TypeString:
				fmt.Fprintf(&buf, "\tres.%s = strings.Clone(u.%s)\n", f.Name, f.Name)
			case TypeBytes:
				fmt.Fprintf(&buf, "\tif u.%s != nil {\n", f.Name)
				fmt.Fprintf(&buf, "\t\tres.%s = make([]byte, len(u.%s))\n", f.Name, f.Name)
				fmt.Fprintf(&buf, "\t\tcopy(res.%s, u.%s)\n", f.Name, f.Name)
				buf.WriteString("\t}\n")
			case TypeSlicePrimitive:
				fmt.Fprintf(&buf, "\tif u.%s != nil {\n", f.Name)
				fmt.Fprintf(&buf, "\t\tres.%s = make([]%s, len(u.%s))\n", f.Name, f.BaseType, f.Name)
				fmt.Fprintf(&buf, "\t\tcopy(res.%s, u.%s)\n", f.Name, f.Name)
				buf.WriteString("\t}\n")
			case TypeSliceString:
				fmt.Fprintf(&buf, "\tif u.%s != nil {\n", f.Name)
				fmt.Fprintf(&buf, "\t\tres.%s = make([]string, len(u.%s))\n", f.Name, f.Name)
				fmt.Fprintf(&buf, "\t\tfor i := 0; i < len(u.%s); i++ {\n", f.Name)
				fmt.Fprintf(&buf, "\t\t\tres.%s[i] = strings.Clone(u.%s[i])\n", f.Name, f.Name)
				buf.WriteString("\t\t}\n")
				buf.WriteString("\t}\n")
			case TypeAny:
				fmt.Fprintf(&buf, "\tres.%s = %sCloneAny(u.%s)\n", f.Name, prefix, f.Name)
			case TypeStructPtr:
				fmt.Fprintf(&buf, "\tif u.%s != nil {\n", f.Name)
				fmt.Fprintf(&buf, "\t\tres.%s = u.%s.Clone()\n", f.Name, f.Name)
				buf.WriteString("\t}\n")
			}
		}
		buf.WriteString("\treturn res\n}\n\n")
	}

	// Format code using gofmt
	formatted, err := format.Source(buf.Bytes())
	if err != nil {
		return buf.Bytes(), fmt.Errorf("gofmt failed: %w", err)
	}

	return formatted, nil
}

func getMirrorName(baseType string) string {
	return strings.ToLower(baseType[0:1]) + baseType[1:] + "LitzMirror"
}
