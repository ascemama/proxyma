# Precompiled Linux Binary Deployment

Use this mode for a host-level Proxyma installation without cloning the
repository or compiling the application. GitHub Releases provide static Linux
binaries for AMD64 and ARM64. The server binary contains the production
frontend, and `proxymactl` is installed as a separate administrator CLI.

Go, Node.js, npm, Git, and a source checkout are **not required** on the target
host.

To compile the binaries yourself, use the
[source-built Linux binary guide](proxyma-server-installation-linux-binary.md).

## Supported Hosts and Prerequisites

- A systemd-based Linux host using `x86_64`/`amd64` or
  `aarch64`/`arm64`.
- Root access for service installation.
- `curl`, `tar`, `sha256sum`, and standard Linux account-management tools.
- Network access to `https://github.com/ascemama/proxyma/releases`.

The downloader rejects other operating systems and architectures before
installing anything.

## Install the Latest Stable Release

Download the installer as a file so it can be inspected before it receives
root privileges. Do not pipe it directly into `sudo bash`; the first
administrator setup also requires an interactive terminal.

```bash
curl -fL -o install-proxyma-server-linux.sh \
  https://github.com/ascemama/proxyma/releases/latest/download/install-proxyma-server-linux.sh

sudo bash ./install-proxyma-server-linux.sh
```

The downloader:

1. Selects the AMD64 or ARM64 release archive for the current host.
2. Downloads the archive and `SHA256SUMS` from the latest stable GitHub Release.
3. Verifies the archive's SHA-256 checksum.
4. Rejects unsafe archive paths, links, and missing payload files.
5. Runs the version-matched system installer from the verified archive.

The system installer installs `/usr/local/bin/proxyma` and
`/usr/local/bin/proxymactl`, creates the `proxyma` service account,
configuration under `/etc/proxyma`, state under `/var/lib/proxyma`, and enables
the `proxyma` systemd service. It also retains the matching uninstaller under
`/usr/local/lib/proxyma`.

On a fresh interactive installation, enter the initial `admin` user's password
twice. Existing user configuration suppresses this prompt. For unattended
provisioning, pass `--no-bootstrap-admin` and create the administrator later
with `proxymactl`.

## Install an Exact Version

Pin releases in production so installations and rollbacks are reproducible:

```bash
sudo bash ./install-proxyma-server-linux.sh --version v0.2.0
```

`--version` accepts a complete semantic version beginning with `v`, including
an exact prerelease such as `v0.2.0-rc.1`. The default `latest` selector never
chooses a prerelease.

All other arguments are passed to the system installer. For example:

```bash
sudo bash ./install-proxyma-server-linux.sh \
  --version v0.2.0 \
  --bind-address 192.0.2.10 \
  --port 8443 \
  --tls-server-name proxyma.example.com
```

Use `-- --help` to display the bundled system installer's complete option list:

```bash
sudo bash ./install-proxyma-server-linux.sh -- --help
```

## Manual Download and Verification

The release archive can be downloaded and installed without the downloader.
Set the exact version and architecture explicitly:

```bash
version=v0.2.0
arch=amd64
base="https://github.com/ascemama/proxyma/releases/download/$version"

curl -fLO "$base/proxyma_linux_$arch.tar.gz"
curl -fLO "$base/SHA256SUMS"
grep "  proxyma_linux_$arch.tar.gz$" SHA256SUMS | sha256sum --check
tar -xzf "proxyma_linux_$arch.tar.gz"
sudo GOARCH="$arch" bash ./proxyma/scripts/install-proxyma-server-linux-release.sh
```

Use `arch=arm64` on an ARM64 host. Do not install an archive whose checksum
fails or whose checksum entry is missing.

For an additional provenance check, install the GitHub CLI and verify the
downloaded archive against this repository:

```bash
gh attestation verify "proxyma_linux_$arch.tar.gz" \
  --repo ascemama/proxyma
```

SHA-256 verification is always required. Provenance verification is an
additional check that the asset was produced by the repository's GitHub
Actions release workflow.

## HTTPS, Address, and Initial Configuration

Fresh installations serve HTTPS on all host interfaces at port `8080` and
create a managed self-signed certificate. Browsers will report that certificate
as untrusted until it is added to the client trust store.

To install a CA-issued PEM certificate/full chain and matching unencrypted
private key:

```bash
sudo bash ./install-proxyma-server-linux.sh \
  --version v0.2.0 \
  --tls-cert /secure/proxyma-fullchain.pem \
  --tls-key /secure/proxyma-key.pem
```

For generated self-signed HTTPS, include the name clients use:

```bash
sudo bash ./install-proxyma-server-linux.sh \
  --tls-server-name proxyma.example.com
```

Plain HTTP is available only for isolated development or diagnosis:

```bash
sudo bash ./install-proxyma-server-linux.sh --http
```

Do not expose HTTP to untrusted networks because it does not protect
credentials in transit.

## Verify and Operate the Service

Check service status and recent logs:

```bash
/usr/local/bin/proxyma --version
sudo systemctl status proxyma --no-pager --full
sudo journalctl -u proxyma -n 200 --no-pager
```

With the default self-signed certificate, a diagnostic health request is:

```bash
curl --insecure https://127.0.0.1:8080/v1/health
```

Use normal certificate verification after installing a trusted certificate.
The authenticated GUI is available at `https://<server-ip>:8080/`.

## Update

Back up configuration and state first:

```bash
sudo tar -C / -czf proxyma-backup.tgz etc/proxyma var/lib/proxyma
```

Download the current installer asset again and run it with the desired exact
version:

```bash
curl -fL -o install-proxyma-server-linux.sh \
  https://github.com/ascemama/proxyma/releases/latest/download/install-proxyma-server-linux.sh
sudo bash ./install-proxyma-server-linux.sh --version v0.3.0
```

The installer replaces both binaries and its retained uninstaller, rewrites the
systemd unit, and restarts the service. Existing `/etc/proxyma` configuration
and `/var/lib/proxyma` state are preserved unless endpoint or transport options
explicitly request a change.

Verify the service, logs, GUI, and health endpoint after every update.

## Roll Back

Back up current configuration and state, then select the previous exact release:

```bash
sudo bash ./install-proxyma-server-linux.sh --version v0.2.0
```

If the newer release introduced an incompatible data change, restore the
matching `/etc/proxyma` and `/var/lib/proxyma` backup before starting the older
service.

## Troubleshoot

Start with:

```bash
sudo systemctl status proxyma --no-pager --full
sudo journalctl -u proxyma -n 200 --no-pager
```

Download failures require access to GitHub Releases. A checksum failure means
the archive must not be installed; remove the downloads and retry. An
unsupported-architecture error can be checked with `uname -m`.

To run the installed server interactively with debug logging, first stop
systemd so two processes cannot use the same port or state:

```bash
sudo systemctl stop proxyma
sudo -u proxyma /usr/local/bin/proxyma \
  --config /etc/proxyma/proxyma.config.json \
  --debug
```

Debug logs can contain sensitive request details. Press `Ctrl+C` when finished,
then restore the service:

```bash
sudo systemctl start proxyma
```

## Completely Uninstall

Back up `/etc/proxyma` and `/var/lib/proxyma` before continuing. The following
command permanently deletes Proxyma configuration and state:

```bash
sudo bash /usr/local/lib/proxyma/uninstall-proxyma-server-linux-release.sh --yes
```

The uninstaller stops and disables the service, removes both installed
binaries, the systemd unit, `/etc/proxyma`, `/var/lib/proxyma`, its own retained
lifecycle files, and the `proxyma` system user and group when possible.
