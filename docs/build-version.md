# Build and Version Metadata

Proxyma embeds its version and source commit in the server binary. The
authenticated GUI displays this identity, and the same values are returned by
successful user login and session responses.

Git tags are authoritative for release builds. A build at `v1.2.3` reports
`1.2.3`; the fourth commit after `v1.2.3-rc.1` reports a value such as
`1.2.3-rc.1+4.g2396d74`. A build from a modified worktree appends `.dirty`.

Ordinary `go build` commands use the VCS metadata embedded by the Go toolchain
and report `dev+g<commit>` when no release version was injected. Release scripts
derive the nearest reachable tag and source commit automatically.

Reproducible build jobs may set:

| Build type | Version input | Commit input |
| --- | --- | --- |
| Server binary | `PROXYMA_VERSION` | `PROXYMA_COMMIT` |
| Client CLI | `PROXYMA_VERSION` | `PROXYMA_COMMIT` |
| Container image | `BUILD_VERSION` | `BUILD_COMMIT` |

Both values are compiled into the Proxyma server. The production GUI build is
embedded in that server, so its displayed identity belongs to the serving
binary rather than to a separately versioned frontend.

Inspect a built, installed, or downloaded server without starting it:

```text
proxyma --version
```

The output has the form:

```text
Proxyma <version> (commit <commit>)
```

See the [source-built server installation guide](proxyma-server-installation-linux-binary.md) for release
build commands and the [local development guide](proxyma-server-installation-local.md) for
development builds.
