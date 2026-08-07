# Litz

[![Go Reference](https://pkg.go.dev/badge/github.com/cuprite-io/litz.svg)](https://pkg.go.dev/github.com/cuprite-io/litz)
[![License](https://img.shields.io/badge/License-Apache%202.0-blue.svg)](LICENSE)

Litz is a high-performance serialization library for Go, optimized for low-latency serialization hot-paths. Litz leverages the **SBC-IPR (Segmented Block Copy & In-place Pointer Resolution)** paradigm combined with a **HIBI (Hash Index Block Inlay)** wire format to minimize parsing and allocation overhead during message processing.

For questions regarding bug reporting, pull requests, and security policies, please refer to [CONTRIBUTING.md](CONTRIBUTING.md) and [SECURITY.md](SECURITY.md).

---

## Key Features

- **Zero-Allocation Deserialization**: Resolves strings, slices, and nested structures directly pointing into the backing serialized buffer.
- **In-Place Querying**: The HIBI wire format indexes fields by hash, allowing O(log N) binary search lookups on the byte stream via the dynamic [`litz.Dynamic`](litz.go) reader without parsing the entire payload.
- **Compile-Time Layout Verification**: Code generation outputs compile-time size and field-offset assertions (`unsafe.Offsetof`) to guarantee structural alignment matches exactly.
- **Security Boundaries**: Enforces memory alignment checks and math-safe bounds comparisons to prevent buffer overflow and OOM Denial of Service (DoS) vectors.

---

## Benchmarks & Performance Metrics

Below is a performance comparison of Litz against **Standard Go Protobuf (v2)**, **Vtprotobuf** (Highly optimized VT-codegen Protobuf), and **MessagePack (Msgp)** run on a Linux AMD64 system:

| Payload Size | Library | Marshal (ns/op) | Unmarshal (ns/op) | Heap Allocations (B/op) | Allocs (op) |
| :--- | :--- | :--- | :--- | :--- | :--- |
| **Small** | **Litz** | **2.51 ns** | **0.43 ns** | **0 B** | **0** |
| | Vtprotobuf | 14.59 ns | 5.03 ns | 3 B | 1 |
| | MessagePack | 4.50 ns | 10.25 ns | 0 B | 0 |
| | Protobuf | 64.53 ns | 58.93 ns | 0 B | 0 |
| **Medium** | **Litz** | **5.25 ns** | **2.42 ns** | **0 B** | **0** |
| | Vtprotobuf | 38.02 ns | 41.46 ns | 32 B | 1 |
| | MessagePack | 15.30 ns | 59.52 ns | 16 B | 1 |
| | Protobuf | 127.30 ns | 124.60 ns | 16 B | 1 |
| **Large** | **Litz** | **77.26 ns** | **78.14 ns** | **48 B** | **1** |
| | MessagePack | 90.72 ns | 337.40 ns | 136 B | 7 |
| | Vtprotobuf | 152.30 ns | 684.10 ns | 1081 B | 7 |
| | Protobuf | 378.70 ns | 752.00 ns | 536 B | 13 |

---

## Design Trade-offs & Limitations

While Litz is optimized for speed, it makes several major trade-offs:

1. **Architecture Portability (Little-Endian only)**: The wire format uses little-endian byte ordering directly matching host hardware registers (amd64, arm64, wasm). Paying no cost for byte-swapping means Litz is not portable to big-endian architectures.
2. **Fixed-Size Fields (Larger Wire Footprint)**: Litz does not use variable-length integer encoding (varints). Numerical types are serialized as fixed 8-byte, 4-byte, or 2-byte values, leading to larger wire payloads compared to Protobuf or Msgpack.
3. **Buffer Lifetime Coupling (Zero-Copy)**: Deserialized string and slice fields point directly into the source byte buffer. Reusing or discarding the source buffer will lead to use-after-free corruption unless the deserialized struct is explicitly duplicated via `Clone()`.
4. **Homogeneous Slices Only**: Collection elements must share a single consistent data type. Heterogeneous (mixed-type) slices are unsupported and will reject marshaling.

---

## Quick Start

### 1. Install the Generator

```bash
go install github.com/cuprite-io/litz/cmd/litz-gen@latest
```

### 2. Annotate Struct Definitions

Annotate target structs in your package with the `//litz:generate` comment directive:

```go
package user

//litz:generate
type Profile struct {
	ID     uint64
	Name   string
	Active bool
}
```

### 3. Generate Code

Run the code generator to produce layout mirrors and marshaling helpers (outputs to `*_test.go` files if generated solely for testing packages):

```bash
litz-gen -dir . -out profile_gen.go
```

### 4. Serialize & Deserialize

```go
package main

import (
	"fmt"
	"log"

	"github.com/cuprite-io/litz"
	"mypackage/user" // Import generated package
)

func main() {
	input := &user.Profile{ID: 101, Name: "Jane Doe", Active: true}

	// 1. Marshal struct to buffer
	buf, err := user.MarshalProfile(input, nil)
	if err != nil {
		log.Fatalf("failed to marshal: %v", err)
	}

	// 2. Unmarshal buffer back to struct
	var output user.Profile
	if err := user.UnmarshalProfile(buf, &output); err != nil {
		log.Fatalf("failed to unmarshal: %v", err)
	}

	fmt.Printf("Deserialized User: %s (ID: %d)\n", output.Name, output.ID)

	// 3. Optional: Dynamic lookup of fields without full unmarshaling
	dyn := litz.NewDynamic(buf, litz.TypeMap)
	if activeVal := dyn.Get("Active"); activeVal != nil {
		fmt.Printf("Dynamic Active check: %t\n", activeVal.Bool())
	}
}
```
