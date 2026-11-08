# Installing `proxyma-client` from source

Use this path when the installed CLI must come from a local checkout or a
specific source revision. Build and package the CLI and harness from the same
checkout, then install both into the project where Codex or Claude Code will run.

The Proxyma server and `proxyma-client` normally run on different machines. The
server should be installed using one of the [server installation guides](proxyma-server-installation.md),
while the client is installed on the workstation where Codex or Claude Code
runs.

Do not mix a source-built CLI with a release harness. The executable and
harness should always come from the same source checkout.

## Prepare a trusted source checkout

Install Git and Go, then clone Proxyma and select the source revision to use.
Go must be a version supported by the `go` directive in `go.mod`.

Linux or macOS:

```bash
git clone https://github.com/ascemama/proxyma.git
cd proxyma
git checkout v1.2.3       # replace with the trusted tag or commit
go version
```

Windows PowerShell:

```powershell
git clone https://github.com/ascemama/proxyma.git
Set-Location .\proxyma
git checkout v1.2.3       # replace with the trusted tag or commit
go version
```

For an existing checkout, run the commands from its root and verify the
revision before building:

```bash
git status --short
git rev-parse --verify HEAD
```

Before configuring the harness, obtain the UUID and JWT signing key of a
Proxyma agent authorized for the required targets. Do not put that signing key
in a prompt, installer argument, command history, or source repository.

## Build the client executable

The build scripts disable CGO and embed the selected version and commit in the
binary. They write a platform-specific executable to `dist/`.

Linux or macOS:

```bash
OUT_DIR="$PWD/dist" bash ./scripts/build-client-release.sh
```

Windows PowerShell:

```powershell
.\scripts\build-client-release.ps1 -OutDir .\dist
```

If dependencies are not already cached, the build downloads the Go module
dependencies automatically. To fetch them explicitly first, run:

```bash
go mod download
```

Confirm that the executable was created:

```bash
# Linux or macOS
ls -l "dist/proxyma-client-$(go env GOOS)-$(go env GOARCH)"
```

```powershell
# Windows PowerShell
Get-Item ".\dist\proxyma-client-$(go env GOOS)-$(go env GOARCH).exe"
```

## Package the matching harness from source

The CLI executable is not the complete client installation. Package the
instruction pack, schemas, examples, and adapters from this same checkout.
The package script deliberately excludes the development credential and local
task state.

Linux or macOS:

```bash
bash ./scripts/package-client-harness.sh
```

With no arguments, the packaging script derives the source version from
`git describe --tags --long --always --dirty` and the source commit from
`git rev-parse --short=7 HEAD`. Set `PROXYMA_VERSION`, `PROXYMA_COMMIT`, or
`OUT_DIR` when reproducible or custom metadata or an alternate output directory
is required. Source mode packages the current working tree, including regular
untracked files in the supported harness directories, regardless of whether
changes are staged. Temporary files placed in those directories are packaged
as well, so remove them before building.

Release and CI packaging continue to use the explicit version and commit
arguments; that mode copies only tracked files.

Windows PowerShell (run from Git Bash, because the packaging script is a
POSIX shell script):

```powershell
& bash ./scripts/package-client-harness.sh
```

The same automatic source metadata behavior applies on Windows. Set the
corresponding environment variables before invoking the script when custom or
reproducible metadata is required.

## Install the built executable and harness

Set `PROJECT_DIR` to the project where Codex or Claude Code will work. Use a separate project
directory if the source checkout is only a build checkout.

Linux or macOS:

```bash
PROJECT_DIR="$PWD"                         # change this when needed
BIN_DIR="$HOME/.local/bin"
ASSET="dist/proxyma-client-$(go env GOOS)-$(go env GOARCH)"

bash ./scripts/install-proxyma-client.sh \
  --binary "$ASSET" \
  --harness "$PWD/dist/proxyma-client-harness.tar.gz" \
  --harness-type codex \
  --project-dir "$PROJECT_DIR" \
  --bin-dir "$BIN_DIR"
```

Windows PowerShell:

```powershell
$ProjectDir = (Get-Location).Path       # change this when needed
$BinDir = Join-Path $env:LOCALAPPDATA "Programs\Proxyma\bin"
$Asset = ".\dist\proxyma-client-$(& go env GOOS)-$(& go env GOARCH).exe"

& .\scripts\install-proxyma-client.ps1 `
  -Binary $Asset `
  -Harness ".\dist\proxyma-client-harness.tar.gz" `
  -HarnessType Codex `
  -ProjectDir $ProjectDir `
  -BinDir $BinDir
```

The source installation does not modify `PATH`. Add `$HOME/.local/bin` on
Linux or macOS, or `%LOCALAPPDATA%\Programs\Proxyma\bin` on Windows, to
`PATH`, then open a new terminal and restart Codex or Claude Code.

After installation, first edit `harness/proxyma-client.json` and replace the
example `proxyma_base_url` value with the URL of the running Proxyma server.
The URL must be reachable from the workstation running `proxyma-client`; for a
local development server it may be `http://127.0.0.1:8080`. The exact JSON
layout and the remaining harness configuration steps are documented in
[Configure the harness](proxyma-client-installation-release.md#configure-the-harness).

Then copy `harness/agent-configuration/agent.example.json` to `agent.json`
and fill in the agent UUID and signing key created in the Proxyma GUI. The
example is included with the harness package; the real credential file is
local-only.

## After installation

The project layout, harness configuration, credential handling, validation
commands, and troubleshooting steps are shared with release installations:

- [Installed project layout](proxyma-client-installation-release.md#installed-project-layout)
- [Configure the harness](proxyma-client-installation-release.md#configure-the-harness)
- [Validate the installation](proxyma-client-installation-release.md#validate-the-installation)
- [Configuration discovery and troubleshooting](proxyma-client-installation-release.md#configuration-discovery-and-troubleshooting)

The harness retrieves target policy guidance lazily before the first command for
each target with `proxyma-client policy --target=<hostname-or-ip>`. It reuses the
guidance until the harness restarts; server-side command enforcement remains
authoritative.
