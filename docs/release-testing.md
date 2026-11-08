# Release artifact testing

This guide is for Proxyma maintainers who build and publish server and
`proxyma-client` release artifacts. End users should follow the
[`proxyma-client` release installation guide](proxyma-client-installation-release.md)
instead.

## Release lifecycle

The release workflow is triggered by pushing a tag beginning with `v`:

```bash
git tag -a v1.2.3-rc.1 -m "Proxyma v1.2.3-rc.1"
git push origin v1.2.3-rc.1
```

Tags containing a hyphen, such as `v1.2.3-rc.1`, are published as GitHub
prereleases. A final tag such as `v1.2.3` is published as the stable release
and becomes the target of the `latest` installer URL.

Release tags and assets are immutable in this workflow. If a candidate fails,
fix the source and publish a new tag such as `v1.2.3-rc.2`; do not try to
replace the existing release.

## Automated GitHub Actions checks

The complete release workflow runs on GitHub-hosted runners. A local machine is
not required.

### Before publication

The workflow first builds the server, Docker, agent, harness, and installer
assets. It then assembles one complete `release-assets` workflow artifact,
generates `SHA256SUMS`, and checks that every expected asset exists and matches
its checksum.

The staged client installer is tested on Ubuntu, macOS, and Windows before a
GitHub release is created. These tests:

1. Use the exact tag version.
2. Download the installer and payload from the staged release layout.
3. Install into temporary project and binary directories.
4. Verify the binary, version, harness metadata, configuration, and Codex
   adapter.
5. Seed local configuration and task state.
6. Run the installer again and verify that local configuration and task state
   are preserved.

If any pre-publication check fails, the publication job is skipped and no
GitHub release is created.

ARM64 assets are included in the manifest and checksum checks. They are not
executed unless a matching ARM64 runner is available.

### Publication and approval

Prerelease tags publish automatically after the pre-publication checks pass.
Stable tags use the `production-release` GitHub environment. Configure that
environment in repository settings with the required reviewers who must
approve the publication job.

The publication job consumes the exact staged artifact that passed testing; it
does not rebuild or reassemble the release files.

### After publication

When the repository is public, a second matrix job downloads the installer from
the exact public release URL on Ubuntu, macOS, and Windows. It repeats the
installation and version checks with retry logic for release propagation.

This job is skipped while the repository is private because the installers do
not authenticate against private GitHub release assets. Private releases are
covered by the staged pre-publication test instead.

If the post-publication test fails, the GitHub release already exists and is
not rolled back automatically. Investigate the workflow output, then publish a
new prerelease or corrective version. Do not overwrite the failed immutable
release.

## Local and private artifact testing

The workflow performs the normal staged test on GitHub. For local debugging or
private release development, use the same release layout with the checked-out
installer and the `PROXYMA_RELEASE_BASE_URL` override.

The minimum layout is:

```text
private-release/
└── releases/
    └── download/
        └── v0.0.0-test/
            ├── install-proxyma-client.sh
            ├── install-proxyma-client.ps1
            ├── proxyma-client-linux-amd64
            ├── proxyma-client-harness.tar.gz
            └── SHA256SUMS
```

The checksum file must contain the exact SHA-256 entries for the binary and
harness payload. On Linux or macOS, run the reusable smoke test against a local
file mirror:

```bash
bash scripts/test-client-release.sh \
  v0.0.0-test \
  file:///path/to/private-release
```

On Windows, run:

```powershell
& .\scripts\test-client-release.ps1 `
  -Version v0.0.0-test `
  -ReleaseBaseUrl ([Uri]::new((Resolve-Path .\private-release).Path).AbsoluteUri.TrimEnd('/'))
```

The reusable scripts test installation and update behavior. The installer’s
`--binary`/`-Binary` and `--harness`/`-Harness` options are useful for local
build debugging, but intentionally bypass release downloads and checksum
verification.

## What is not part of the release smoke test

The smoke test does not run `proxyma-client doctor`, submit commands, or access
a real Proxyma server. Those operations require a server URL, agent UUID,
signing key, and appropriate target authorization. Keep those credentials out
of release jobs unless a separately protected integration environment is
introduced.

The existing fixture tests remain important: they cover checksum failures,
unsafe archives, symbolic links, conflicting transaction paths, invalid
versions, and preservation behavior without requiring a GitHub release.

## Failure investigation

- **Asset assembly fails:** inspect the missing asset or checksum output in the
  `release-assets` job.
- **Staged smoke test fails:** inspect the runner-specific pre-publication job;
  no GitHub release should exist for that tag.
- **Stable publication is waiting:** approve or reject the `production-release`
  environment deployment after reviewing the successful pre-publication jobs.
- **Public smoke test fails:** inspect the exact release URL, asset list,
  checksum output, and runner logs. The release exists and must be handled with
  a new immutable version.
