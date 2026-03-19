# Security Policy

## Supported Versions

| Version | Supported |
|---------|-----------|
| latest release | Yes |
| develop (nightly) | Best-effort |
| older releases | No |

## Reporting a Vulnerability

**Do not open a public issue for security vulnerabilities.**

Email **shaik.noorullah.shareef@gmail.com** with:

- Description of the vulnerability
- Steps to reproduce
- Impact assessment
- Suggested fix (if any)

You should receive a response within 48 hours. We'll work with you to understand the issue and coordinate a fix before any public disclosure.

## Security Design

wtfrc is designed with privacy and security as core principles:

- **Local-first** — all data stays on your machine unless you explicitly configure a remote LLM
- **Secret redaction** — API keys, SSH paths, passwords, and tokens are stripped before any LLM processing
- **No telemetry** — zero data collection, no phone-home
- **File exclusions** — `~/.ssh/id_*`, `~/.gnupg/`, `~/.aws/credentials`, and `**/.env` are excluded by default
- **SQLite WAL mode** — database access is safe for concurrent reads

## Scope

Security reports are relevant for:

- Secret redaction bypass (sensitive data reaching the LLM)
- Path traversal in config discovery
- SQL injection in search queries
- Arbitrary code execution via config parsing
- Data exfiltration via crafted config files
