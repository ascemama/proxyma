# Contributing to Proxyma

Thank you for your interest in Proxyma. Bug reports, design discussions, and
documentation feedback are welcome through GitHub issues and discussions.

## Contribution policy

Proxyma is distributed under the GNU Affero General Public License version 3
only (`AGPL-3.0-only`).

Proxyma is currently in an early stage of development. Bug reports, feature
suggestions, and other feedback are welcome. Please open an issue before
preparing a code or documentation contribution. The project is not currently
accepting unsolicited pull requests.

Automated dependency-update pull requests remain subject to maintainer review.

## Development checks

Maintainers working on a change should run the relevant checks from the
repository root:

```bash
go test ./...
cd frontend
npm ci
npm run build
```

Container changes should also build for both supported platforms:

```bash
docker buildx build --platform linux/amd64,linux/arm64 .
```
