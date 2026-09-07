# Docker Deployment

Use this mode when Proxyma should run as a Docker container. The official image
contains the server with its frontend embedded and the separate `proxymactl`
binary. Images are published at `ghcr.io/ascemama/proxyma` for Linux AMD64 and
ARM64 and can be pulled anonymously while the package is public.

This guide defines two installation methods:

1. Download the release installer and run the matching published image. This is
   the recommended production method and does not require a source checkout.
2. Clone the repository, build an image locally, and deploy that image with the
   same installer logic.

Both methods create a managed deployment under `/opt/proxyma`.

## Prerequisites and Image Versions

The installer requires a Linux host with root access, a standard rootful Docker
Engine, and Docker Compose v2 with support for `docker compose up --wait`.
Rootless Docker and rootful Docker with `userns-remap` are not supported because
their subordinate UID/GID mappings do not match the bind-mount ownership model.

Published image tags behave as follows:

- `latest` points to the newest final release.
- `1`, `1.2`, and `1.2.3` are stable version aliases.
- Prereleases such as `1.3.0-rc.1` exist only under their exact tag.
- A digest such as `ghcr.io/ascemama/proxyma@sha256:...` identifies an immutable
  manifest.

## Method 1: Install the Published Container

Download the installer from the latest stable GitHub Release and run it:

```bash
curl -fL -o install-proxyma-server-docker.sh \
  https://github.com/ascemama/proxyma/releases/latest/download/install-proxyma-server-docker.sh
sudo bash ./install-proxyma-server-docker.sh
```

The downloaded script retrieves `proxyma_docker.tar.gz` and `SHA256SUMS`,
verifies the bundle checksum, rejects unsafe archive entries, and runs the
bundled installer. Release metadata selects the exact image tag corresponding
to the downloaded release rather than the moving `latest` tag.

To install an exact stable release or prerelease, download its installer and
pass the same version:

```bash
version=v0.2.0
curl -fL -o install-proxyma-server-docker.sh \
  "https://github.com/ascemama/proxyma/releases/download/$version/install-proxyma-server-docker.sh"
sudo bash ./install-proxyma-server-docker.sh --version "$version"
```

`--version` accepts only `vMAJOR.MINOR.PATCH` and exact semantic prerelease
versions such as `v0.3.0-rc.1`. The `latest` selector never selects a
prerelease.

For maximum image reproducibility, override the matching tag with the manifest
digest shown by GHCR:

```bash
sudo bash ./install-proxyma-server-docker.sh \
  --version v0.2.0 \
  --image ghcr.io/ascemama/proxyma@sha256:<manifest-digest>
```

### Installer behavior and options

The installer:

1. Validates Linux, root access, Docker Engine, and Docker Compose v2.
2. Rejects rootless Docker and `userns-remap`.
3. Installs lifecycle and Compose files under `/opt/proxyma`.
4. Creates protected configuration and data directories owned by numeric
   UID/GID `10001:10001`.
5. Saves the host address, port, and selected image in
   `/opt/proxyma/proxyma-compose.env`.
6. Creates the initial HTTPS configuration only when it is missing.
7. Pulls the selected image and creates the first administrator interactively
   when no user exists.
8. Starts Proxyma and waits for the container health check.

Fresh installations use self-signed HTTPS on `0.0.0.0:8080`. Select a different
host endpoint and include the client-facing name in the generated certificate:

```bash
sudo bash ./install-proxyma-server-docker.sh \
  --bind-address 192.0.2.10 \
  --host-port 8443 \
  --tls-server-name proxyma.example.com
```

For unattended preparation, skip administrator creation:

```bash
sudo bash ./install-proxyma-server-docker.sh --no-bootstrap-admin
```

Install a CA-issued PEM certificate/full chain and matching unencrypted key:

```bash
sudo bash ./install-proxyma-server-docker.sh \
  --tls-cert /secure/proxyma-fullchain.pem \
  --tls-key /secure/proxyma-key.pem
```

Change the transport mode on an existing deployment by rerunning the retained
installer:

```bash
sudo bash /opt/proxyma/scripts/install-proxyma-server-docker.sh \
  --self-signed --tls-server-name proxyma.example.com
sudo bash /opt/proxyma/scripts/install-proxyma-server-docker.sh --http
sudo bash /opt/proxyma/scripts/install-proxyma-server-docker.sh --https
```

Plain HTTP is intended only for isolated development or diagnosis. Existing
configuration, users, and state are preserved on installer reruns. Endpoint
options update only the selected components; omitted address and port values
are read from the existing Compose environment file.

## Method 2: Build Locally and Install Automatically

Use this method when testing a source change or building a custom image. You
only need to build the image yourself; the installer still performs the full
deployment setup. Do not manually create `/opt/proxyma`, configuration or data
directories, ownership, or Compose files.

Clone the repository and build the image on the host where Proxyma will run:

```bash
git clone https://github.com/ascemama/proxyma.git
cd proxyma
docker build -t proxyma:local .
```

Pass the resulting image to the repository installer. `--no-pull` requires that
the selected image exists in the Docker daemon used by the installer and
prevents Docker from contacting a registry for it:

```bash
sudo bash ./scripts/install-proxyma-server-docker.sh \
  --image proxyma:local \
  --no-pull
```

This command follows the same installation flow as Method 1: it creates and
permissions the managed directories, installs the Compose and lifecycle files,
initializes configuration when needed, offers to create the first administrator,
and starts the service. All endpoint, TLS, and bootstrap options described for
the published installer also apply here.

The source checkout is used only to build the image and provide the current
deployment files. The installed deployment lives under `/opt/proxyma` and does
not depend on the checkout afterward.

After additional source changes, rebuild the same image and rerun the command:

```bash
docker build -t proxyma:local .
sudo bash ./scripts/install-proxyma-server-docker.sh \
  --image proxyma:local \
  --no-pull \
  --no-bootstrap-admin
```

## Operate and Verify

The deployment layout is:

- `/opt/proxyma/docker-compose.example.yml`: retained Compose definition.
- `/opt/proxyma/proxyma-compose.env`: host address, port, and image selection.
- `/opt/proxyma/proxyma-config`: configuration, policies, users, and secrets.
- `/opt/proxyma/proxyma-data`: durable runtime state.
- `/opt/proxyma/scripts`: retained installer, uninstaller, and helper.

Use the persisted environment file for every Compose operation:

```bash
sudo docker compose \
  --env-file /opt/proxyma/proxyma-compose.env \
  --project-directory /opt/proxyma \
  -f /opt/proxyma/docker-compose.example.yml ps

sudo docker compose \
  --env-file /opt/proxyma/proxyma-compose.env \
  --project-directory /opt/proxyma \
  -f /opt/proxyma/docker-compose.example.yml logs -f Proxyma
```

Stop and restart without deleting configuration or state:

```bash
sudo docker compose \
  --env-file /opt/proxyma/proxyma-compose.env \
  --project-directory /opt/proxyma \
  -f /opt/proxyma/docker-compose.example.yml down

sudo docker compose \
  --env-file /opt/proxyma/proxyma-compose.env \
  --project-directory /opt/proxyma \
  -f /opt/proxyma/docker-compose.example.yml up -d --wait
```

Open the GUI at `https://<server-ip>:<host-port>/`. Browsers reject the default
self-signed certificate until it is trusted. For a diagnostic check only:

```bash
curl --insecure https://127.0.0.1:8080/v1/health
```

Use normal certificate verification after installing a trusted certificate.
Publishing on `0.0.0.0` exposes the port on every host interface, so restrict it
with host and upstream firewalls where appropriate.

## Update and Roll Back

Back up configuration and state before changing versions:

```bash
sudo tar -C /opt/proxyma -czf proxyma-docker-backup.tgz \
  proxyma-config proxyma-data proxyma-compose.env
```

Download the target release installer and run it with the exact version. This
updates the retained deployment files and image selection while preserving
configuration and data:

```bash
version=v0.3.0
curl -fL -o install-proxyma-server-docker.sh \
  "https://github.com/ascemama/proxyma/releases/download/$version/install-proxyma-server-docker.sh"
sudo bash ./install-proxyma-server-docker.sh --version "$version" --no-bootstrap-admin
```

Roll back by repeating the command for the previous exact version. Restore the
matching backup first if a newer version introduced an incompatible data
migration.

## Migrate an Existing Checkout-Based Deployment

The installer does not move an existing repository-root deployment
automatically. Automatic movement of live configuration and state would risk
starting two deployments or losing local ownership and permissions.

From the old checkout, stop the existing deployment and create a backup:

```bash
sudo docker compose \
  --env-file proxyma-compose.env \
  -f docker-compose.example.yml down
sudo tar -czf proxyma-docker-pre-migration.tgz \
  proxyma-config proxyma-data proxyma-compose.env
```

Copy the preserved files into the new managed root:

```bash
sudo install -d -m 0755 /opt/proxyma
sudo cp -a proxyma-config proxyma-data /opt/proxyma/
sudo cp -a proxyma-compose.env /opt/proxyma/
```

Then download and run the release installer as described in Method 1. It will
recognize the copied configuration and state, install the new lifecycle files,
apply the selected release image, and start the deployment. Verify the service
before removing the old checkout or migration backup.

## Troubleshooting and Manual Control

Confirm that Docker and Compose are available and inspect the deployment:

```bash
docker info
docker compose version
sudo docker compose \
  --env-file /opt/proxyma/proxyma-compose.env \
  --project-directory /opt/proxyma \
  -f /opt/proxyma/docker-compose.example.yml ps
```

If startup fails with an ownership error under `/var/lib/proxyma`, restore the
numeric container ownership:

```bash
sudo chown -R 10001:10001 \
  /opt/proxyma/proxyma-config \
  /opt/proxyma/proxyma-data
sudo chmod -R u=rwX,g=,o= \
  /opt/proxyma/proxyma-config \
  /opt/proxyma/proxyma-data
```

The host does not need a user named `proxyma`; bind-mount ownership is checked
by numeric UID/GID. A checksum failure means the Docker deployment bundle must
not be used. Remove the failed download and retry. For additional provenance
verification, use the GitHub CLI against an exact release asset:

```bash
gh attestation verify proxyma_docker.tar.gz --repo ascemama/proxyma
```

## Completely Uninstall

Complete uninstallation permanently removes Proxyma configuration and state.
Back up `/opt/proxyma/proxyma-config` and `/opt/proxyma/proxyma-data` first.

Remove containers, networks, configuration, state, and retained deployment
files while keeping cached images:

```bash
sudo bash /opt/proxyma/scripts/uninstall-proxyma-server-docker.sh --yes
```

Also remove the image used by the existing container:

```bash
sudo bash /opt/proxyma/scripts/uninstall-proxyma-server-docker.sh \
  --yes --remove-image
```

If the container has already been removed, identify the image explicitly:

```bash
sudo bash /opt/proxyma/scripts/uninstall-proxyma-server-docker.sh \
  --yes --remove-image \
  --image ghcr.io/ascemama/proxyma:0.2.0
```

Docker Engine, Docker Compose, backups, and images not explicitly selected for
removal are retained.

## Container Provenance and License

Every tagged image is built by GitHub Actions from the corresponding repository
tag as a combined AMD64/ARM64 manifest. The workflow publishes OCI metadata, an
SBOM, maximum BuildKit provenance, and a GitHub artifact attestation for the
manifest digest. Docker deployment bundles and installers are checksum-listed
and attested with the other GitHub Release assets.

The corresponding source is available from the matching Git tag at
`https://github.com/ascemama/proxyma` under `AGPL-3.0-only`.
