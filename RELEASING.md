# Releasing

The first release is `v0.1.0`. The repository distributes a Go module and a source archive; it does not build application binaries. Use Go 1.27, Python 3, Docker Compose and [GitHub CLI](https://cli.github.com/) for the commands below.

## Configure GitHub once

Create an empty public `ctolon/esodm` repository without a generated README or license. If it already exists, use that repository. The module path in `go.mod` must match the repository path.

In repository settings:

- Enable GitHub Actions with read-only default token permissions. Workflows grant `contents: write` only to the release draft job. No personal access token or publishing secret is needed by CI.
- Enable the dependency graph, Dependabot alerts, private vulnerability reporting, secret scanning and push protection where available. Review the reporting contact in [Code of conduct](CODE_OF_CONDUCT.md).
- After the first successful CI run, protect `main`: require pull requests, resolved conversations and the `CI checks` status from GitHub Actions; block force pushes and deletion. Require one approving review and Code Owner review when another maintainer is available. A sole maintainer cannot approve their own pull request.
- Protect `v*` tags against updates and deletion. Restrict tag creation to release maintainers. Do not require an approval from an account that cannot review its own release.
- Create a `release` environment and allow `v*` tags. Add a required reviewer if a second maintainer should approve draft creation. The workflow uses this environment.
- Enable [immutable releases](https://docs.github.com/en/code-security/how-tos/secure-your-supply-chain/establish-provenance-and-integrity/prevent-release-changes) before publication. The workflow attaches the archive and checksum while the release is still a draft.

These are repository settings, not files that become active on checkout. Apply branch protection after the bootstrap push so an empty repository is not blocked. The [required-check guidance](https://docs.github.com/en/pull-requests/how-tos/merge-and-close-pull-requests/troubleshooting-required-status-checks) explains why the aggregate check uses `always()` and explicitly rejects skipped dependencies.

## First commit

Check the date in the `0.1.0` changelog heading against the planned publication date. The initial entry describes shipped features; unreleased implementation fixes do not need historical migration notes.

```sh
make tools
make fmt
make generate
make docs
make lint
GOTOOLCHAIN=go1.27.0 make verify
python3 -m unittest discover -s scripts -p 'test_*.py'
python3 scripts/release_notes.py v0.1.0

git add .
git diff --cached --check
git diff --cached --stat
git diff --cached
```

Inspect the staged files before committing. `artifacts/`, `bin/`, local notes and temporary credentials must not be staged. Do not use `git add -f` for ignored files.

```sh
git commit -m "feat: initial esodm release"
git remote add origin git@github.com:ctolon/esodm.git
git push -u origin main
```

If `origin` already exists, verify it with `git remote -v` and omit `git remote add`. Authenticate the CLI with `gh auth login`, then find and watch the CI run:

```sh
gh run list --workflow ci.yml --branch main --commit "$(git rev-parse HEAD)"
# Replace RUN_ID with the matching run ID above.
gh run watch RUN_ID --exit-status
```

Wait for CI to succeed and apply the repository settings above before tagging. For the first release there is no previous benchmark baseline; save `make benchmark` output for future comparisons.

## Tag the candidate

Start from a clean `main` that matches the tested remote commit:

```sh
git switch main
git pull --ff-only
test -z "$(git status --porcelain)"
test "$(git rev-parse HEAD)" = "$(git rev-parse origin/main)"
python3 scripts/release_notes.py v0.1.0
git tag -a v0.1.0 -m "esodm v0.1.0"
git push origin v0.1.0
```

Use `git tag -s` instead of `-a` if commit signing is configured. A public Go module tag can be fetched and cached as soon as it is pushed, even before the GitHub release is published. Never move or reuse a published version tag; fix defects in a new version.

The tag triggers ordinary CI and extended verification on the same revision. Budget at least two hours plus queue and setup time. Once both pass, the workflow creates a draft with `esodm-v0.1.0.tar.gz`, `SHA256SUMS` and the matching changelog section. The tag must refer to a commit reachable from `main`. Supported tags are `v0.x.y` and `v1.x.y`, optionally ending in `-alpha.N`, `-beta.N` or `-rc.N` with a positive integer `N`.

```sh
gh run list --workflow release.yml --commit "$(git rev-parse v0.1.0^{commit})"
gh run watch RUN_ID --exit-status
gh release view v0.1.0
```

If infrastructure fails, rerun the failed jobs from the matching workflow run. The workflow can update an existing draft, but refuses to modify a published release. A source fix requires a new commit and version; do not move the old tag to make a failed test disappear. Manual recovery uses `gh workflow run release.yml --ref v0.1.0` and runs all checks again.

## Publish

Review the tag commit, changelog and successful release run. Download and verify the attached archive:

```sh
gh release download v0.1.0 --dir artifacts/release-v0.1.0
(cd artifacts/release-v0.1.0 && sha256sum -c SHA256SUMS)
gh release edit v0.1.0 --draft=false --latest
```

On macOS, use `shasum -a 256 -c SHA256SUMS`. The [CLI release documentation](https://cli.github.com/manual/gh_release_edit) describes draft publication and prerelease flags. For a prerelease, retain the prerelease flag and omit `--latest`.

Confirm module resolution from outside this checkout, so the local module does not hide a publication problem:

```sh
(cd /tmp && GOPROXY=https://proxy.golang.org go list -m github.com/ctolon/esodm@v0.1.0)
```

The proxy and package documentation may take time to refresh. A proxy error is not a reason to recreate the tag.

## Later releases

Record user-visible changes under `Unreleased`. Before tagging, move those entries to a dated version section and leave a new empty `Unreleased` section. Update the compatibility guide when support changes. Minor versions before 1.0 may contain documented breaking changes; patch versions remain backward compatible. Version 2 and later require a `/vN` module path and corresponding release-tool changes.
