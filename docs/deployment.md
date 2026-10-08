# Proxyma Server Deployment

This page covers deployment of the `proxyma-server` component and its embedded
browser frontend. It does not describe installation of `proxyma-agent`, which
runs on the workstation where Codex, Claude Code, or another agent runtime
operates. For the agent component, see the [`proxyma-agent` release
installation guide](proxyma-agent-installation-release.md) or the
[`proxyma-agent` source installation guide](proxyma-agent-installation-source.md).

Proxyma Server supports four deployment modes. Choose the mode that matches how
the server will be used, then follow the linked lifecycle guide.

| Server installation type | Intended use | Supported host | Runtime shape | Requirements |
| --- | --- | --- | --- | --- |
| [Local development](deployment-local.md) | Changing or testing Proxyma and its frontend | Windows, Linux, or macOS development checkout | Separate Go API and Vite frontend processes | Go, Node.js, npm, and Git |
| [Precompiled Linux binary](deployment-linux-precompiled.md) | Recommended host-level production or test installation | Linux AMD64 or ARM64 with systemd | Downloaded server binary with the frontend embedded, plus `proxymactl` | `curl`, `tar`, `sha256sum`, and root access; no source checkout or compiler |
| [Source-built Linux binary](deployment-linux-binary.md) | Building and installing a selected source revision | Linux AMD64 or ARM64 with systemd, plus a build host | Locally compiled server binary with the frontend embedded, plus `proxymactl` | Go, Node.js, npm, Git, and root access |
| [Docker](deployment-docker.md) | Container-based production or test installation | Linux AMD64 or ARM64 with rootful Docker | One container with the embedded frontend and `proxymactl` | Docker Engine, Docker Compose v2, and root access for the installer |

`proxymactl` remains a separate admin CLI binary in production. Both Linux
binary modes and the container deployment serve the production frontend from
the Proxyma server. Go and Node.js are required only for local development and
source/release builds, not when installing a precompiled release.

Each guide covers prerequisites, first installation, normal operation and
verification, updates and rollback, troubleshooting, backups, and complete
removal. The removal procedures delete Proxyma configuration and state, so read
their backup warnings before running them.

Production installations serve native HTTPS by default with an automatically
managed self-signed certificate. Install a CA-issued certificate for trusted
browser access. Plain HTTP and its non-Secure development cookie are available
only when the scheme is explicitly changed to `http`.
