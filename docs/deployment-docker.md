# Docker Deployment

Use this mode when Proxyma should run as a Docker container. The image contains
the Proxyma server binary with the frontend embedded and the separate
`proxymactl` binary. The official image is published at
`ghcr.io/ascemama/proxyma` for Linux AMD64 and ARM64. Public packages can be
pulled anonymously; registry login is not required.

## Prerequisites and Image Selection

The automated installer supports a Linux host with root access, a standard
rootful Docker Engine, and Docker Compose v2 with support for `up --wait`.
Rootless Docker and rootful Docker with `userns-remap` are not supported by the
installer because their subordinate UID/GID mappings do not match the bind-mount
ownership model described below.

Release tags have the following behavior:

- `latest` points to the newest final release.
- `1`, `1.2`, and `1.2.3` are stable version aliases.
- Prereleases such as `1.3.0-rc.1` are published only under their exact tag and
  never move `latest`.
- A digest such as `ghcr.io/ascemama/proxyma@sha256:...` identifies one immutable
  published manifest and provides the most reproducible deployment.

The expanded authenticated sidebar displays the version embedded in the
running server image. Official images receive this value from their release
tag and commit. Custom image builds can provide the same metadata with
`--build-arg BUILD_VERSION=<tag-or-description>` and
`--build-arg BUILD_COMMIT=<commit>`.

## First Installation

For a standard rootful Docker Engine installation on Linux, run the installer
from a source checkout:

```bash
sudo bash ./scripts/install-docker.sh
```

The default host publication is native self-signed HTTPS on `0.0.0.0:8080`.
Choose a different host address or port and include the access name in the
generated certificate with:

```bash
sudo bash ./scripts/install-docker.sh \
  --bind-address 192.0.2.10 \
  --host-port 8443 \
  --tls-server-name proxyma.example.com
```

The selected host endpoint maps to port `8080` inside the container. The
container listener and health check remain on that internal port.
`--bind-address` accepts an IPv4 address, hostname, or bracketed IPv6 address;
the host port must be between `1` and `65535`.

The installer:

1. Checks that it is running on Linux as root and that Docker Engine and Docker
   Compose v2 are available.
2. Rejects rootless Docker and `userns-remap`, whose user-namespace ownership
   models are not supported by this installer.
3. Creates `proxyma-config` and `proxyma-data` with all required subdirectories.
4. Saves the host publication in `proxyma-compose.env` and creates
   `proxyma-config/proxyma.config.json` only when it is missing.
5. Sets restrictive permissions and numeric ownership for the container's
   UID/GID `10001:10001`.
6. Pulls `ghcr.io/ascemama/proxyma:latest`, or the image selected with `--image`.
7. On a fresh interactive installation, prompts for the initial `admin` password
   and creates that administrator through the container image's `proxymactl`
   binary.
8. Starts Proxyma with Docker Compose and waits for its health check to pass.

For a production deployment, pin an exact release instead of following
`latest`:

```bash
sudo bash ./scripts/install-docker.sh \
  --image ghcr.io/ascemama/proxyma:0.1.0
```

For maximum reproducibility, use the digest displayed by GHCR for the release:

```bash
sudo bash ./scripts/install-docker.sh \
  --image ghcr.io/ascemama/proxyma@sha256:<manifest-digest>
```

The password is entered twice and passed to `proxymactl` over standard input. It
is not stored in a file, command-line argument, or shell history. The temporary
JSON record contains only non-secret user metadata and is removed automatically.

Existing configuration, users, and state are preserved on reruns. If any user
JSON record already exists, the installer skips administrator bootstrap
automatically.

Rerunning without endpoint options preserves the settings in
`proxyma-compose.env`. To change the published endpoint, rerun with
`--bind-address`, `--host-port`, or both. Supplying only one option preserves
the other existing component.

For unattended preparation, or when the administrator will be provisioned
separately, disable the interactive prompt:

```bash
sudo bash ./scripts/install-docker.sh --no-bootstrap-admin
```

Install a PEM certificate/full chain and matching unencrypted PEM key at first
installation, convert an existing self-signed or HTTP deployment, or renew the
active provided certificate with the same command:

```bash
sudo bash ./scripts/install-docker.sh \
  --tls-cert /secure/proxyma-fullchain.pem \
  --tls-key /secure/proxyma-key.pem
```

The installer rejects symlinks, unreadable files, invalid validity dates, and
mismatched keys before changing the active pair. It stores managed files under
`proxyma-config/tls`; the private key is mode `0600`. The service is recreated
after a successful change. Failed validation leaves the active certificate and
configuration unchanged.

Switch back to generated HTTPS, use explicit development HTTP, or re-enable
the currently selected HTTPS certificate mode:

```bash
sudo bash ./scripts/install-docker.sh \
  --self-signed --tls-server-name proxyma.example.com
sudo bash ./scripts/install-docker.sh --http
sudo bash ./scripts/install-docker.sh --https
```

Inactive certificate files are retained when switching. Rerunning with no
transport options preserves the current scheme and TLS mode. Self-signed
certificates contain localhost, loopbacks, the container hostname, any concrete
container listener, and `--tls-server-name`. Because the standard container
listener is a wildcard, set `--tls-server-name` to the host DNS name or IP used
by clients. Proxyma renews generated certificates when fewer than 30 days
remain or required SANs change. A pre-HTTPS installation is migrated as
explicit HTTP to avoid a surprise protocol change; rerun with `--self-signed`
or a provided pair when ready. Do not expose HTTP to untrusted networks.

## Operation and Verification

Check status and logs:

```bash
docker compose --env-file proxyma-compose.env -f docker-compose.example.yml ps
docker compose --env-file proxyma-compose.env -f docker-compose.example.yml logs -f Proxyma
```

Stop Proxyma without deleting the bind-mounted configuration and state:

```bash
docker compose --env-file proxyma-compose.env -f docker-compose.example.yml down
```

This is an operational stop, not an uninstall. Restart with:

```bash
docker compose --env-file proxyma-compose.env -f docker-compose.example.yml up -d --wait
```

When the deployment uses a non-default image, set `PROXYMA_IMAGE` to that exact
tag or digest when running `pull` or `up` commands.

Open the GUI at `https://<server-ip>:<host-port>/`. Browsers reject the default
self-signed certificate until it is trusted. Use `curl --insecure` only for
self-signed diagnostics; normal checks should use a CA-issued certificate or
`curl --cacert <issuer.pem>`. The protocol-aware container health check supports
both default HTTPS and explicit HTTP. Publishing on `0.0.0.0` exposes the port
on every host interface; restrict it to intended client networks with host and
upstream firewalls.

## Update and Roll Back

Back up `proxyma-config` and `proxyma-data` before upgrading. From the repository
root, one possible backup is:

```bash
sudo tar -czf proxyma-docker-backup.tgz proxyma-config proxyma-data
```

Choose the new exact release or digest, pull it, and recreate the service:

```bash
export PROXYMA_IMAGE=ghcr.io/ascemama/proxyma:0.2.0
docker compose --env-file proxyma-compose.env -f docker-compose.example.yml pull Proxyma
docker compose --env-file proxyma-compose.env -f docker-compose.example.yml up -d --wait
```

Alternatively, rerun the installer with the new image. It preserves existing
configuration, users, and state:

```bash
sudo bash ./scripts/install-docker.sh \
  --image ghcr.io/ascemama/proxyma:0.2.0
```

Roll back by restoring the previous `PROXYMA_IMAGE` version or digest and
running the same `docker compose up -d --wait` command. Restore the matching data
backup if a release introduced an incompatible data migration.

Rebuild a local contributor image after frontend or backend code changes.
Deployment scripts and the Dockerfile usually change only when runtime shape
changes, such as new ports, new required OS packages, or new config/state paths.

## Manual Installation and Troubleshooting

The automated installer is recommended. The commands below document its
underlying steps for custom deployments and troubleshooting.

Pull a published release:

```bash
docker pull ghcr.io/ascemama/proxyma:latest
```

Contributors can instead build the current checkout locally:

```bash
docker build -t proxyma:local .
```

The Dockerfile uses separate build stages for the frontend, Go binaries, and
runtime image. Node.js and Go are not present in the final runtime image. The
runtime process runs as numeric UID/GID `10001:10001`, and the AGPL-3.0 license
is included at `/usr/share/licenses/proxyma/LICENSE`.

Create the mounted directories:

```bash
mkdir -p proxyma-config/{targets,policies,agents,users,interactions,secrets/private_keys,tls}
mkdir -p proxyma-data/{tasks,interactions,user_sessions,credential_tokens,.ssh}
```

Create `proxyma-config/proxyma.config.json`:

```json
{
  "server": {
    "listen_address": "0.0.0.0:8080",
    "scheme": "https",
    "tls": {
      "mode": "self_signed",
      "certificate_file": "/etc/proxyma/tls/self-signed.crt",
      "private_key_file": "/etc/proxyma/tls/self-signed.key",
      "server_name": "proxyma.example.com"
    }
  },
  "paths": {
    "target_dir": "/etc/proxyma/targets",
    "policy_dir": "/etc/proxyma/policies",
    "agent_dir": "/etc/proxyma/agents",
    "user_dir": "/etc/proxyma/users",
    "task_dir": "/var/lib/proxyma/tasks",
    "interaction_dir": "/var/lib/proxyma/interactions",
    "interaction_history_db": "/var/lib/proxyma/interaction-history.db",
    "interaction_config_dir": "/etc/proxyma/interactions",
    "user_session_dir": "/var/lib/proxyma/user_sessions",
    "credential_token_dir": "/var/lib/proxyma/credential_tokens",
    "known_hosts_path": "/var/lib/proxyma/.ssh/known_hosts"
  },
  "execution": {
    "command_timeout": "5m",
    "interaction_timeout": "24h",
    "output_limit": 1048576,
    "require_command_approval": true
  },
  "security": {
    "user_session_ttl": "12h",
    "user_activation_token_ttl": "12h",
    "user_activation_token_max_ttl": "48h"
  },
  "logging": {
    "debug": false
  }
}
```

The container runs as UID `10001`. Linux bind mounts use the host directory's
real numeric ownership and permissions; Docker does not remap them for this
setup. Make the mounted directories writable by UID `10001`:

```bash
sudo chown -R 10001:10001 proxyma-config proxyma-data
sudo chmod -R u=rwX,g=,o= proxyma-config proxyma-data
```

If startup fails with an error like `secure known hosts directory: chmod
/var/lib/proxyma/.ssh: operation not permitted`, the bind-mounted
`proxyma-data/.ssh` directory is still owned by the wrong UID on the host.
Re-run the `chown` command before starting the container.

If the host does not have a user named `proxyma`, that is fine. Ownership is
checked by numeric UID/GID, not by username. A directory owned by `10001:10001`
is valid even if the host has no local account with that name.

Create at least the administrator before attempting to sign in. Policies,
agents, and targets can be added after the service starts.

Run directly with Docker when Compose is not desired:

```bash
docker run --name Proxyma --rm \
  -p 8080:8080 \
  -e PROXYMA_LISTEN=0.0.0.0:8080 \
  -v "$PWD/proxyma-config:/etc/proxyma" \
  -v "$PWD/proxyma-data:/var/lib/proxyma" \
  ghcr.io/ascemama/proxyma:latest
```

The backend is available at `https://127.0.0.1:8080/`. For manual
provided-certificate HTTPS or renewal, use `proxymactl` to validate, stage, and
replace the pair, then atomically select it in the JSON configuration:

```bash
docker run --rm --entrypoint /usr/local/bin/proxymactl \
  -v "/secure:/source:ro" \
  -v "$PWD/proxyma-config:/etc/proxyma" \
  ghcr.io/ascemama/proxyma:latest \
  certificates install \
  --cert-file /source/proxyma-fullchain.pem \
  --key-file /source/proxyma-key.pem \
  --dest-cert-file /etc/proxyma/tls/provided.crt \
  --dest-key-file /etc/proxyma/tls/provided.key

docker run --rm --entrypoint /usr/local/bin/proxymactl \
  -v "$PWD/proxyma-config:/etc/proxyma" \
  ghcr.io/ascemama/proxyma:latest \
  config update-transport \
  --file /etc/proxyma/proxyma.config.json \
  --scheme https --tls-mode provided \
  --tls-cert-file /etc/proxyma/tls/provided.crt \
  --tls-key-file /etc/proxyma/tls/provided.key
```

Then recreate the container. The source mount must contain readable regular PEM
files, not symlinks, and the key must be unencrypted. Ensure
`proxyma-config/tls` remains owned by numeric UID/GID `10001:10001`.

To use manual HTTP for development, set `server.scheme` to `http`; cookies then
switch automatically to the non-Secure development cookie. Set it back to
`https` and restart to use the selected TLS mode again.

Alternatively, run the example Compose deployment after creating the bind
mounts and administrator:

```bash
docker compose -f docker-compose.example.yml up -d --wait
```

Compose defaults to `ghcr.io/ascemama/proxyma:latest`. Override it with an exact
release, digest, or contributor build:

```bash
PROXYMA_IMAGE=ghcr.io/ascemama/proxyma:0.1.0 \
  docker compose -f docker-compose.example.yml up -d --wait

docker build -t proxyma:local .
PROXYMA_IMAGE=proxyma:local \
  docker compose -f docker-compose.example.yml up -d --wait
```

The Compose file also accepts `PROXYMA_BIND_ADDRESS` and
`PROXYMA_HOST_PORT`; their defaults are `0.0.0.0` and `8080`. Automated
installations persist those values in `proxyma-compose.env`, which should be
passed with `--env-file` for subsequent lifecycle commands.

Check logs or stop the service with the operation commands above.

To create the initial administrator manually, create
`proxyma-config/new-user.json` without putting the password in it:

```json
{
  "username": "admin",
  "name": "Proxyma Administrator",
  "admin": true,
  "theme": "light"
}
```

Then run:

```bash
read -rsp "Initial Proxyma admin password: " PROXYMA_PASSWORD
printf '\n'
printf '%s\n' "$PROXYMA_PASSWORD" | \
docker run --rm \
  -i \
  --entrypoint /usr/local/bin/proxymactl \
  -v "$PWD/proxyma-config:/etc/proxyma" \
  ghcr.io/ascemama/proxyma:latest \
  --user-dir /etc/proxyma/users \
  users create --file /etc/proxyma/new-user.json --password-stdin
unset PROXYMA_PASSWORD
rm proxyma-config/new-user.json
```

For frequent admin work, using the host `proxymactl` binary from the Linux
release artifact is usually simpler.

Rootless Docker and rootful Docker with `userns-remap` use subordinate UID/GID
mappings rather than the host UID `10001` assumed by these bind-mount
instructions. Use a standard rootful Docker daemon for this installer. A
rootless deployment needs a separate volume and maintenance strategy so the
rootless daemon owner can continue to inspect, back up, and restore protected
configuration and state.

## Completely Uninstall

Stopping with `docker compose down` preserves `proxyma-config`, `proxyma-data`,
`proxyma-compose.env`, and cached images. Complete uninstallation is
destructive: back up both bind mount directories first if they contain data you
want to keep.

Remove the Compose containers, networks, orphan containers, declared volumes,
configuration, and state with:

```bash
sudo bash ./scripts/uninstall-docker.sh --yes
```

This retains the Docker Engine, Docker Compose, cached images, source checkout,
and backups. To remove the image used by an existing Proxyma Compose container
as well, run:

```bash
sudo bash ./scripts/uninstall-docker.sh --yes --remove-image
```

The script captures the image ID before removing the container. If the container
has already been removed, supply the exact image reference:

```bash
sudo bash ./scripts/uninstall-docker.sh --yes --remove-image \
  --image ghcr.io/ascemama/proxyma:0.2.0
```

Docker refuses image removal if another container still uses it. The script also
rejects symbolic-link deployment directories so destructive cleanup cannot
escape the repository root.

## Container Image Provenance and License

Every tagged release is built by GitHub Actions from the corresponding
repository tag. The workflow publishes a single AMD64/ARM64 manifest with OCI
source, revision, version, description, and `AGPL-3.0-only` license metadata. It
also publishes a software bill of materials, maximum BuildKit provenance, and a
GitHub artifact attestation for the manifest digest.

The source code for each image is available from the matching Git tag at
`https://github.com/ascemama/proxyma`. Released AGPL versions remain available
under those terms. Verify a deployment by comparing its digest with the digest
on the GHCR package version page and, when the GitHub CLI supports attestations
for the account, with `gh attestation verify`.
