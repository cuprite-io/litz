# Security Policy

## Supported Versions

We currently support and provide security updates for the following versions:

| Version | Supported          |
| ------- | ------------------ |
| v0.x.x  | :white_check_mark: |

## Reporting a Vulnerability

We take the security of Litz seriously. If you believe you have found a security vulnerability, please do not open a public issue or Pull Request. Doing so exposes all active systems using Litz to zero-day exploits.

Instead, please report vulnerabilities privately:
1. Navigate to the **Security** tab of this repository on GitHub.
2. Click **Advisories**, then **New draft security advisory**.
3. Provide a detailed description, proof of concept, and impact details.

We follow coordinated disclosure and will work with you to patch the issue privately before releasing a public security advisory.

## Built-in Security Features

Litz is designed to safely handle untrusted binary payloads directly in low-latency hot-paths:
- **Memory Safety & Alignment Protection**: Enforces 8-byte pointer alignment checks (`EnsureAligned`, `ErrUnalignedBuffer`) and guarantees 8-byte boundary alignment and zeroed padding between variable-length segments, eliminating `SIGBUS` faults on strict alignment architectures.
- **Overflow-Safe Bounds Checks**: Utilizes multiplication-free, overflow-immune bounds checking in generated unmarshalers and HIBI dynamic lookups. Payloads and element offsets are clamped to 4 GiB (`math.MaxUint32`), returning `ErrSizeOverflow` to prevent silent 32-bit offset wrapping on 64-bit platforms.
- **Recursion & Call-Stack Protection**: Recursive pointer hierarchies, maps, slices, and `CloneAny` traversals strictly enforce depth checks (`DefaultMaxDepth = 64`, `ErrMaxDepthExceeded`), neutralizing stack-exhaustion DoS vectors.
- **Strict AST Type Environment**: Compile-time code generator validates struct AST models, rejecting unsafe pointer-bearing types, embedded fields, maps, or unbounded interfaces to maintain wire format safety.
- **Deterministic Field Defaults & Strict Unmarshaling**: Unmarshalers deterministically reset destination structs on decode, and strict unmarshalers (`Unmarshal*Strict`) guarantee zero-tolerance validation of payload extents without silent field truncation.
- **Target Architecture Quarantine**: SBC-IPR requires 64-bit word size and little-endian register ordering (`amd64`, `arm64`, `riscv64`, `ppc64le`, `loong64`, `mips64le`). 32-bit and big-endian architectures are safely rejected at compile time via Go build constraints.
