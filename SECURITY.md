# Security policy

## Reporting a vulnerability

Do not put credentials, personal data or an exploit against a live customer in
a public issue. Email **founder@plexivo.io** with the affected version, a concise
description, impact and a minimal reproduction using only your own test data.
This is the currently verified receiving mailbox.

You can also [report a vulnerability privately](https://github.com/plexivo/plexivo-cli/security/advisories/new)
through this repository's Security tab. Public bug reports belong in
[Issues](https://github.com/plexivo/plexivo-cli/issues) only when they do not expose
a security weakness or private information.

We aim to acknowledge security reports within three business days. This is a
response target, not a claim about past reports or a service-level guarantee.
We will agree on coordinated disclosure and credit with the reporter. Keep
details private until a fix or an agreed disclosure date is available.

## Supported versions and fixes

Security fixes target the latest stable release and the main development branch.
Older releases may need an upgrade; separate long-term support is not promised.
A tested source change is not yet an npm, pub.dev or CLI release. Follow the
release notes for the version containing a fix.

## Safe testing

Use local fixtures or resources you own and have permission to test. Do not
access another customer's data, disrupt availability, send a load flood,
attempt persistence, or publish secrets. Report accidental access immediately
and stop; do not download additional data as proof.

## Development rules

Never commit keys or tokens. Use reviewed cryptographic libraries and verified
TLS. Changes to authorization, credentials, network destinations and data
isolation need negative tests as well as successful-path tests. Add a regression
test for each security fix. Automated scans supplement, not replace, human review.

No security certification or OpenSSF badge is claimed by this document. Results
may be displayed only after the issuer has recorded them for this repository.
