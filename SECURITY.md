# Security policy

## Reporting a vulnerability

Report suspected vulnerabilities through [GitHub private vulnerability reporting](https://github.com/ctolon/esodm/security/advisories/new). If private reporting is unavailable, email [Cevat Batuhan Tolon](mailto:cevatbatuhan.tolon@gmail.com). Include the affected revision, the Elasticsearch and client versions involved, a minimal reproduction and the impact. Do not post exploit details, credentials or private data in public issues.

Reports are acknowledged as soon as possible; there is no guaranteed response time. Disclosure is coordinated with the reporter once a fix is available.

## Supported versions

Security fixes target the latest published release. Use the latest patch version; older minor versions are not maintained. Before the first tag, fixes land on `main`.

## Deployment guidance

- Configure TLS verification, authentication and least-privilege roles on the official client. See the [privilege table](docs/production.md#required-privileges).
- Treat `Error.Body`, `Error.Reason` and `Error.Cause` as potentially containing document data. Use `Error.LogValue` or `Error.Redacted` when logging.
- Cursors are not signed. Authenticate cursors that cross a trust boundary before resuming iteration.
- Keep migration checkpoints in trusted storage; a checkpoint authorizes alias changes when resumed.

CI runs `govulncheck` on every push, and Dependabot proposes dependency updates weekly.
