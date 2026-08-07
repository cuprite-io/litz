# Contributing to Litz

First off, thank you for considering contributing to Litz! It's contributions from the community that make Litz a powerful, robust, and safe serialization protocol.

## Technical Philosophy

- **Zero-Allocation Hot Path**: The core serialization and deserialization flows (SBC-IPR) must remain zero-allocation on heap memory. Always structure buffers around pools and reuse memory efficiently.
- **Explicit Alignment Guarding**: Serialized payloads and dynamic blocks must be aligned on 8-byte boundaries. Always verify pointer alignment at the entry of unmarshaling steps to ensure safety across strict-alignment hardware (like ARM and WASM).
- **Strict Pointer Safety**: Perform pointer arithmetic inline using `unsafe.Add` on `unsafe.Pointer` to ensure the Go garbage collector can properly trace root pointers. Avoid storing unsafe pointers in `uintptr` variables across statements.

## Development Workflow

1. **Fork the Repo**: Create a dedicated feature branch.
2. **Local Development**:
   - Ensure your code follows `go fmt`.
   - Run the full test suite with race detector enabled: `go test -v -race ./...`
3. **Generator Updates**:
   - If you make changes to test structures, run the code generator to update `litz_gen_test.go`:
     `go run cmd/litz-gen/main.go`
4. **Benchmarking**:
   - Before submitting changes affecting the hot path, run the comparative benchmarks:
     `go test -bench=. -benchmem ./benchmark`
   - Verify that there is zero performance degradation compared to baseline numbers.
5. **Pull Request**:
   - Describe your changes clearly in the PR description.
   - Verify that all GitHub Actions verification steps pass.

## Code of Conduct

Be respectful, professional, and collaborative. We aim to foster a welcoming, high-quality development community.

## Reporting Bugs

Please report bugs using GitHub Issues. Make sure to provide:
- A clear, concise description of the bug.
- Minimal steps or schema details to reproduce the issue.
- Environment details (Go version, OS, target architecture).
