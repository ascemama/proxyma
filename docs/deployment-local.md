# Local Development Deployment

Use this mode when changing Proxyma code or the frontend. It intentionally runs
two processes so frontend changes can use Vite hot reload. This is a development
deployment, not the recommended production topology.

## Transport Model

Local frontend development intentionally uses plain HTTP:

- Vite serves the frontend at `http://127.0.0.1:5173`.
- Proxyma serves the development API at `http://127.0.0.1:8080`.
- Vite proxies `/v1/...` requests to that HTTP API.

Starting Proxyma with `--scheme https` does not make the separate Vite server
use HTTPS. The checked-in Vite configuration is designed for the explicit HTTP
development mode, so start Proxyma with `--scheme http` when using Vite.

This differs from Linux and Docker production deployments: their frontend is
embedded in Proxyma and is served through the same native HTTPS listener as the
API. Do not expose the local Vite/HTTP development setup to untrusted networks.

## Prerequisites

- Git.
- Go 1.26.3 or the project-pinned Go version from `go.mod`.
- Node.js and npm.

## First Installation

Clone the repository and enter the checkout:

```bash
git clone https://github.com/ascemama/proxyma.git
cd proxyma
```

Download the Go dependencies and install the locked frontend dependencies:

```bash
go mod download
cd frontend
npm ci
cd ..
```

Build and start the Proxyma backend from the repository root:

```bash
go build -o ./bin/proxyma ./cmd/proxyma
./bin/proxyma --scheme http
```

On Windows PowerShell, use:

```powershell
go build -o .\bin\proxyma.exe .\cmd\proxyma
.\bin\proxyma.exe --scheme http
```

In another terminal, start the frontend:

```bash
cd frontend
npm run dev -- --port 5173
```

Open `http://127.0.0.1:5173/`. The Vite dev server proxies `/v1/...` API calls
to Proxyma at `http://127.0.0.1:8080`.

Proxyma defaults to HTTPS even in a source checkout. `--scheme http` is
therefore required for this Vite development workflow. HTTP automatically uses
the non-Secure development session cookie; there is no independent cookie
override.

To exercise the backend's native HTTPS without the Vite workflow, omit
`--scheme http` and access Proxyma directly on port `8080`. Proxyma generates
`tls/self-signed.crt` and `tls/self-signed.key` by default; use
`curl --insecure` only for diagnostics or trust the generated certificate
explicitly. To test a CA-issued PEM full chain and matching unencrypted key:

```bash
./bin/proxyma \
  --scheme https \
  --tls-mode provided \
  --tls-cert-file /secure/proxyma-fullchain.pem \
  --tls-key-file /secure/proxyma-key.pem
```

Replace both provided files and restart the process to test renewal; hot reload
is not supported. Remove those overrides to return to generated self-signed
HTTPS, or restore `--scheme http` for Vite development. Do not commit private
keys or expose the development HTTP listener to untrusted networks.

## Operation and Verification

Keep both the Proxyma and Vite terminals running. Stop either process with
`Ctrl+C`.

Verify the API with a valid Proxyma agent bearer token:

```bash
curl -H "Authorization: Bearer $PROXYMA_AGENT_JWT" http://127.0.0.1:8080/v1/health
```

Local Windows and Linux development default to
`var/proxyma/.ssh/known_hosts` below the working directory. Proxyma creates the
parent directory and OpenSSH-format trust file automatically.

The first credential test for an unknown SSH server displays the key algorithm
and SHA-256 fingerprint in the GUI. Verify the fingerprint through a trusted
channel, then choose **Trust and retry**. Proxyma writes the key only after SSH
authentication and the test command succeed. A changed key remains blocked and
must not be accepted without investigating the server identity change.

New targets, and existing targets whose connection or credential fields change,
must pass the credential test before they can be saved. Metadata-only edits do
not require another test.

## Update an Existing Checkout

Stop both development processes, preserve or commit any intentional source
changes, and update the checkout:

```bash
git pull --ff-only
go mod download
cd frontend
npm ci
cd ..
go build -o ./bin/proxyma ./cmd/proxyma
```

Local builds use the VCS revision embedded by the Go toolchain. The
authenticated sidebar therefore shows `dev+g<commit>` (with `.dirty` when
applicable), while unauthenticated pages do not expose build or license
information.

On Windows, rebuild `./bin/proxyma.exe` instead. Restart the backend and Vite
using the first-installation commands. `npm ci` makes the installed frontend
dependencies match `package-lock.json`; it does not remove Proxyma runtime data.

Before an update that may change data formats, back up the repository-local
configuration directories and `var/proxyma`. Roll back by checking out the
previous known-good revision, reinstalling dependencies, rebuilding, and
restoring the matching backup if necessary.

## Troubleshooting

- If an older `proxyma.config.json` contains
  `security.allow_insecure_user_cookies`, remove that obsolete property when
  convenient. Current Proxyma accepts but ignores it; `server.scheme` alone
  selects HTTPS Secure cookies or the HTTP development cookie.
- Confirm that the backend is listening on `127.0.0.1:8080` and Vite on
  `127.0.0.1:5173`.
- If a port is already in use, stop the previous development process before
  restarting it.
- Run the backend with `--debug` for detailed request and execution logs. Debug
  logs can contain POST bodies and command details; treat them as sensitive.
- Run `npm ci` again after `package-lock.json` changes.

## Completely Remove Local Development Artifacts

Stop the backend and Vite before cleanup. The cleanup deletes ignored files in a
fixed set of repository-local paths, including:

- `bin`, `dist`, `build`, frontend build output, `frontend/node_modules`, and
  test caches.
- `.gocache`, `.gotmp`, `.tmp`, and `internal/web/dist`.
- `proxyma.config.json`, local target, agent, user, interaction, and secret
  records, and `var` runtime state.

Back up any local configuration, secrets, history, or runtime state that you
want to keep. On Linux or macOS, run:

```bash
bash ./scripts/uninstall-local-development.sh --yes
```

On Windows PowerShell, run:

```powershell
.\scripts\uninstall-local-development.ps1 -Yes
```

The scripts use Git's ignored-file rules and a fixed path allowlist, so tracked
examples and source files are retained. They do not delete the checkout, Git
history, globally installed Go/Node/npm tools, or global package caches. Delete
the checkout separately from its parent directory only if it is no longer
needed.
