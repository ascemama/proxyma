# Source-Built Linux Binary Deployment

Use this mode when you want to compile a selected Proxyma source revision and
then install those locally built binaries on a Linux host. The Proxyma server
binary embeds the production frontend, so no Node.js process is needed at
runtime. `proxymactl` is installed as a separate administrator CLI.

To install official binaries without cloning this repository or compiling
anything, use the
[precompiled Linux binary guide](deployment-linux-precompiled.md) instead.

## Prerequisites

- A Linux AMD64 or ARM64 host using systemd.
- Root access for installation and service management.
- Go, Node.js, and npm on the build host.
- Git and a Proxyma source checkout from which to build the release artifacts.

Clone the repository if a checkout is not already available:

```bash
git clone https://github.com/ascemama/proxyma.git
cd proxyma
```

## Build Release Binaries

Build on Linux from the repository root:

```bash
GOOS=linux GOARCH=amd64 bash ./scripts/build-server-release.sh
```

The script:

1. Runs `npm ci` and `npm run build` in `frontend/`.
2. Copies the frontend build into the ignored Go embed directory.
3. Builds `dist/Proxyma-linux-amd64` with the `embedded_frontend` build tag.
4. Builds `dist/proxymactl-linux-amd64` as a separate admin CLI.

For ARM64 Linux, use:

```bash
GOOS=linux GOARCH=arm64 bash ./scripts/build-server-release.sh
```

On Windows PowerShell, cross-build the Linux artifacts with:

```powershell
.\scripts\build-server-release.ps1 -TargetOS linux -TargetArch amd64
```

The build scripts embed the nearest reachable Git tag and the first seven
characters of the commit in the Proxyma server binary. CI or reproducible
builds can set `PROXYMA_VERSION` and `PROXYMA_COMMIT` explicitly. Run the
compiled server with `--version`, or use the authenticated GUI's expanded
sidebar, to inspect the resulting build identity.

## First Installation

After the release binaries are built, install them on the Linux host:

```bash
sudo GOARCH=amd64 bash ./scripts/install-proxyma-server-linux-release.sh
```

For ARM64 Linux:

```bash
sudo GOARCH=arm64 bash ./scripts/install-proxyma-server-linux-release.sh
```

If `GOARCH` is omitted, the installer detects `amd64` on `x86_64` hosts and
`arm64` on `aarch64` hosts.

New installations serve HTTPS on every host interface on port `8080`. When no
TLS options are supplied, Proxyma creates an ECDSA self-signed certificate
under `/etc/proxyma/tls`. Add the DNS name or IP clients use to its SANs:

```bash
sudo GOARCH=amd64 bash ./scripts/install-proxyma-server-linux-release.sh \
  --bind-address 192.0.2.10 \
  --port 8443 \
  --tls-server-name proxyma.example.com
```

`--bind-address` accepts an IPv4 address, hostname, or bracketed IPv6 address.
The port must be between `1` and `65535`.

The installer:

1. Reads the compiled release artifacts from `dist/`.
2. Installs `Proxyma-linux-<arch>` as `/usr/local/bin/proxyma`.
3. Installs `proxymactl-linux-<arch>` as `/usr/local/bin/proxymactl`.
4. Creates the `proxyma` system user and group when missing.
5. Creates `/etc/proxyma` and `/var/lib/proxyma` with production subdirectories.
6. Creates `/etc/proxyma/proxyma.config.json` on a fresh installation and
   preserves it on updates. New configurations use self-signed HTTPS on
   `0.0.0.0:8080`, making the service available on all host network interfaces,
   subject to host and network firewall rules.
7. On a fresh interactive installation, prompts for the initial `admin` user's
   password and creates that administrator.
8. Retains the matching uninstaller under `/usr/local/lib/proxyma`.
9. Installs and starts the `proxyma` systemd service.

The bootstrap prompt appears only when `/etc/proxyma/users` contains no JSON
user records. Enter the password twice; the installer passes it to `proxymactl`
over standard input without storing it in a file, command-line argument, or
shell history. Existing installations skip the prompt automatically.

For unattended installation, or when the first administrator will be
provisioned separately, disable the prompt explicitly:

```bash
sudo GOARCH=amd64 bash ./scripts/install-proxyma-server-linux-release.sh --no-bootstrap-admin
```

Add policies, agents, targets, and interaction instances as needed after the
service starts. If administrator bootstrap was disabled, create a user
separately with `proxymactl` before attempting to sign in.

## Operation and Verification

Check status and logs:

```bash
sudo systemctl status proxyma
sudo journalctl -u proxyma -f
```

The GUI is available at `https://<server-ip>:8080/`. The default certificate is
self-signed, so browsers and normal TLS clients will reject it until its
certificate is added to their trust store. For diagnosis only:

```bash
curl --insecure https://127.0.0.1:8080/v1/health
```

For normal verification, install a CA-issued certificate as described below
and use `curl https://proxyma.example.com:8080/...`, or point `curl --cacert` at
the trusted issuer. Restrict port `8080` to intended clients with host and
network firewalls. HTTPS protects credentials in transit; do not expose the
explicit HTTP mode to untrusted networks.

Existing installations keep their current `listen_address` when the installer
is rerun without endpoint options. To change an existing endpoint, rerun the
installer with `--bind-address`, `--port`, or both. Supplying only one option
preserves the other component from the existing configuration.

Verify the API with a valid Proxyma agent bearer token:

```bash
curl --insecure -H "Authorization: Bearer $PROXYMA_AGENT_JWT" https://127.0.0.1:8080/v1/health
```

With the default transport, use `https://` and add `--insecure` only while
diagnosing the self-signed installation.

## Certificates and Transport Changes

Install a PEM certificate/full chain and its matching unencrypted PEM private
key during the first installation or on any later installer run:

```bash
sudo GOARCH=amd64 bash ./scripts/install-proxyma-server-linux-release.sh \
  --tls-cert /secure/proxyma-fullchain.pem \
  --tls-key /secure/proxyma-key.pem
```

This also converts a self-signed or HTTP deployment to provided-certificate
HTTPS. Renew a provided certificate with the same command and the new files.
The installer validates validity dates and key matching before stopping the
active service, stages the pair, and retains the active pair and configuration
if validation fails. Source files must be readable regular files, not symlinks.
The private key must be unencrypted. Proxyma stores the managed pair under
`/etc/proxyma/tls` with the key restricted to mode `0600`; changes take effect
when the installer restarts the service.

Switch modes without deleting inactive certificate files:

```bash
# Return to an automatically managed self-signed certificate.
sudo GOARCH=amd64 bash ./scripts/install-proxyma-server-linux-release.sh \
  --self-signed --tls-server-name proxyma.example.com

# Development/debugging only.
sudo GOARCH=amd64 bash ./scripts/install-proxyma-server-linux-release.sh --http

# Re-enable HTTPS with whichever TLS mode is currently selected.
sudo GOARCH=amd64 bash ./scripts/install-proxyma-server-linux-release.sh --https
```

Self-signed certificates include localhost, loopbacks, the machine hostname,
the concrete bind address, and `--tls-server-name` in their SANs. Proxyma
regenerates one when missing, when required SANs change, or when fewer than 30
days remain. Rerunning the installer with no transport options preserves the
current scheme and TLS mode. A pre-HTTPS installation is migrated as explicit
HTTP so an upgrade does not unexpectedly change its protocol; rerun with
`--self-signed` or a provided pair when ready to move it to HTTPS.

## Update and Roll Back

Back up configuration and state before updating:

```bash
sudo tar -C / -czf proxyma-backup.tgz etc/proxyma var/lib/proxyma
```

Build the desired release artifacts using the commands above, then rerun the
installer for the host architecture:

```bash
GOOS=linux GOARCH=amd64 bash ./scripts/build-server-release.sh
sudo GOARCH=amd64 bash ./scripts/install-proxyma-server-linux-release.sh
```

The installer stops the service, replaces both binaries, preserves the existing
configuration and state unless endpoint options were supplied, rewrites the
systemd unit, reloads systemd, and starts the service. Verify status, logs, GUI
access, and the health endpoint after the update.

To roll back, rebuild or restore the previous Proxyma and `proxymactl` binaries,
rerun the installer, and restore the matching `/etc/proxyma` and
`/var/lib/proxyma` backup if the newer release changed stored data incompatibly.

## Debug an Installation

Start with systemd status and recent service logs. These commands do not
interrupt the running service:

```bash
sudo systemctl status proxyma --no-pager --full
sudo journalctl -u proxyma -n 200 --no-pager
```

To reproduce a problem with debug logging enabled, stop the systemd service so
it releases the configured listen address and does not write to the same state
directories at the same time:

```bash
sudo systemctl stop proxyma
sudo systemctl is-active proxyma
```

`systemctl is-active` should report `inactive`. Run the installed binary in the
foreground as the same `proxyma` account used by systemd, with the same
configuration file:

```bash
sudo -u proxyma /usr/local/bin/proxyma \
  --config /etc/proxyma/proxyma.config.json \
  --debug
```

The `--debug` command-line flag overrides `"debug": false` from the installed
configuration for this process only. Logs appear directly in the terminal.
Keep that terminal open, reproduce the problem from the GUI or API, and inspect
the output. Debug mode logs HTTP request details, POST bodies, and command
execution details, so treat the output as sensitive and remove secrets before
sharing it.

Press `Ctrl+C` to stop the foreground process. Restore normal systemd operation
and confirm that the service is healthy:

```bash
sudo systemctl start proxyma
sudo systemctl status proxyma --no-pager --full
sudo journalctl -u proxyma -f
```

If the foreground process reports that the listen address is already in use,
confirm that no Proxyma process remains before retrying:

```bash
sudo systemctl is-active proxyma
pgrep -a proxyma
```

Do not run the foreground process and the systemd service simultaneously. Both
would use the same configuration and state directories.

## SSH Host-Key Trust

Proxyma uses its own OpenSSH-format trust store rather than a login user's or
machine-wide SSH trust file. The Linux service uses
`/var/lib/proxyma/.ssh/known_hosts`. Proxyma creates the parent directory and
file automatically. The Linux installer owns them as the `proxyma` service
account with restrictive permissions.

The first credential test for an unknown SSH server displays the key algorithm
and SHA-256 fingerprint in the GUI. Verify the fingerprint through a trusted
channel, then choose **Trust and retry**. Proxyma writes the key only after SSH
authentication and the test command succeed. A changed key remains blocked and
must not be accepted without investigating the server identity change.

New targets, and existing targets whose connection or credential fields change,
must pass the credential test before they can be saved. Metadata-only edits do
not require another test.

## Completely Uninstall

Back up `/etc/proxyma` and `/var/lib/proxyma` first if the host contains data
you want to keep. Then run:

```bash
sudo bash ./scripts/uninstall-proxyma-server-linux-release.sh --yes
```

The uninstaller stops and disables the service, removes the systemd unit,
`/usr/local/bin/proxyma`, `/usr/local/bin/proxymactl`, `/etc/proxyma`, and
`/var/lib/proxyma`, the installed lifecycle script under
`/usr/local/lib/proxyma`, and the `proxyma` system user and group when possible.
This permanently deletes Proxyma configuration and state. The source checkout,
build toolchains, and any backup you created are retained.
