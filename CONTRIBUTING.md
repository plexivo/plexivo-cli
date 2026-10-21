# Contributing

## Local checks

Use Go 1.26.10 or newer and run `gofmt -w cmd internal`, `go vet ./...`,
`go test -race ./...` and `go build ./...`. CI runs the same checks on Linux,
macOS and Windows. Credential-file mode assertions apply to POSIX systems;
Windows file access also depends on the current user's directory ACLs.

Use [Issues](https://github.com/plexivo/plexivo-cli/issues) for reproducible bugs and
enhancement proposals. Search existing issues first. For security concerns,
follow [SECURITY.md](SECURITY.md) instead of posting details publicly.

1. Start from the latest `main` on a separate branch.
2. Keep changes focused and explain the user-visible behavior and compatibility.
3. Add automated tests for new behavior and regressions, including malformed
   input and authorization failures where relevant. Never use live credentials
   or real customer data in tests.
4. Run the checks described in the README and the CI workflow. Fix warnings and
   failures; do not skip tests or weaken checks to make CI green.
5. Open a pull request with the test results and documentation/changelog changes.
   A maintainer reviews it before merging. An automated or self-review is not
   described as an independent human review.

Use the language's standard formatter and clear names. Keep secrets out of
source, issue reports, logs and screenshots. Add third-party code only with a
compatible license and attribution. Contributions are licensed under this
repository's Apache-2.0 license.

## Releases

Use semantic versions and human-readable release notes describing behavior,
compatibility changes and publicly disclosed security fixes. A merge to main
does not publish a package automatically. Publishing is a separate maintainer
action; do not overwrite an existing release or tag.
