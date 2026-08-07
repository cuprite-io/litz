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
- **Memory Safety & Alignment Protection**: Enforces 8-byte pointer alignment checks on buffer reuse, protecting against `SIGBUS` faults on strict alignment architectures.
- **Overflow-Safe Bounds Checks**: Utilizes multiplication-free, overflow-immune bounds checking in generated unmarshalers to prevent remote code execution or arbitrary memory reads via corrupt payloads.
- **Cardinality & Alloc Limits**: Dynamic array and map unmarshalers validate key length and array slice bounds against physical buffer capacities, neutralizing Denial of Service (DoS) OOM vectors.
