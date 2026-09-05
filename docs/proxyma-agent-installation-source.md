# Installing `proxyma-agent` from source

Use this path when the installed CLI must come from a local checkout or a
specific source revision. Build and package the CLI and harness from the same
checkout, then install both into the project where Codex will run.

The Proxyma server and `proxyma-agent` normally run on different machines. The
server should be installed using one of the [deployment guides](deployment.md),
while the Agent is installed on the workstation where Codex or Claude Code
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

## Build the agent executable

The build scripts disable CGO and embed the selected version and commit in the
binary. They write a platform-specific executable to `dist/`.

Linux or macOS:

```bash
OUT_DIR="$PWD/dist" bash ./scripts/build-agent-release.sh
```

Windows PowerShell:

```powershell
.\scripts\build-agent-release.ps1 -OutDir .\dist
```

If dependencies are not already cached, the build downloads the Go module
dependencies automatically. To fetch them explicitly first, run:

```bash
go mod download
```

Confirm that the executable was created:

```bash
# Linux or macOS
ls -l "dist/proxyma-agent-$(go env GOOS)-$(go env GOARCH)"
```

```powershell
# Windows PowerShell
Get-Item ".\dist\proxyma-agent-$(go env GOOS)-$(go env GOARCH).exe"
```

## Package the matching harness from source

The CLI executable is not the complete agent installation. Package the
instruction pack, schemas, examples, and adapters from this same checkout.
The package script deliberately excludes the development credential and local
task state.

Linux or macOS:

```bash
SOURCE_VERSION="$(git describe --tags --exact-match 2>/dev/null || printf 'v0.0.0-source')"
SOURCE_COMMIT="$(git rev-parse --short=7 HEAD)"
bash ./scripts/package-agent-harness.sh \
  "$SOURCE_VERSION" \
  "$SOURCE_COMMIT" \
  ./dist
```

Windows PowerShell (run from Git Bash, because the packaging script is a
POSIX shell script):

```powershell
git describe --tags --exact-match 2>$null
git rev-parse --short=7 HEAD
```

Then run the same `package-agent-harness.sh` command from Git Bash. Use the
semantic-version tag printed by the first command as `SOURCE_VERSION`; if no
tag is present, use `v0.0.0-source`. Use the commit printed by the second
command as `SOURCE_COMMIT`.

## Install the built executable and harness

Set `PROJECT_DIR` to the project where Codex will work. Use a separate project
directory if the source checkout is only a build checkout.

Linux or macOS:

```bash
PROJECT_DIR="$PWD"                         # change this when needed
BIN_DIR="$HOME/.local/bin"
ASSET="dist/proxyma-agent-$(go env GOOS)-$(go env GOARCH)"

bash ./scripts/install-proxyma-agent.sh \
  --binary "$ASSET" \
  --harness "$PWD/dist/proxyma-agent-harness.tar.gz" \
  --project-dir "$PROJECT_DIR" \
  --bin-dir "$BIN_DIR"
```

Windows PowerShell:

```powershell
$ProjectDir = (Get-Location).Path       # change this when needed
$BinDir = Join-Path $env:LOCALAPPDATA "Programs\Proxyma\bin"
$Asset = ".\dist\proxyma-agent-$(& go env GOOS)-$(& go env GOARCH).exe"

& .\scripts\install-proxyma-agent.ps1 `
  -Binary $Asset `
  -Harness ".\dist\proxyma-agent-harness.tar.gz" `
  -ProjectDir $ProjectDir `
  -BinDir $BinDir
```

The source installation does not modify `PATH`. Add `$HOME/.local/bin` on
Linux or macOS, or `%LOCALAPPDATA%\Programs\Proxyma\bin` on Windows, to
`PATH`, then open a new terminal and restart Codex.

## After installation

The project layout, harness configuration, credential handling, validation
commands, and troubleshooting steps are shared with release installations:

- [Installed project layout](proxyma-agent-installation-release.md#installed-project-layout)
- [Configure the harness](proxyma-agent-installation-release.md#configure-the-harness)
- [Validate the installation](proxyma-agent-installation-release.md#validate-the-installation)
- [Configuration discovery and troubleshooting](proxyma-agent-installation-release.md#configuration-discovery-and-troubleshooting)
