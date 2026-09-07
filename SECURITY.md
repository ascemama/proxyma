# Security Policy

## Supported versions

Security fixes are provided for:

| Version | Supported |
| --- | --- |
| `main` | Yes |
| Latest stable release | Yes |
| Older releases | Best effort only |

Users should upgrade to the latest stable release before reporting an issue.

## Reporting a vulnerability

Please do not report security vulnerabilities through public GitHub issues,
pull requests, or discussions.

Use GitHub's private vulnerability reporting feature:

<https://github.com/ascemama/proxyma/security/advisories/new>

If private reporting is unavailable, contact:

<antoine.scemama@yahoo.com>

Please include:

- A clear description of the vulnerability
- Affected versions or commit
- Reproduction steps or a proof of concept
- Potential impact
- Any suggested mitigation

Do not include real passwords, SSH private keys, JWT signing keys, production
hostnames, or other sensitive operational data in the report.

## Response expectations

We aim to:

- Acknowledge reports within 3 business days
- Triage the issue within 7 business days
- Provide status updates at least every 14 days while the issue is being investigated

These are targets rather than guarantees.

## Disclosure policy

We will coordinate disclosure with the reporter whenever practical.

After confirming the issue, we may:

1. Develop and test a fix
2. Release the fix and update affected documentation
3. Publish a security advisory
4. Credit the reporter if they agree

Please allow reasonable time for investigation and remediation before publicly
disclosing the issue.

## Scope

Security reports are especially welcome for:

- Authentication and authorization
- Agent JWT handling
- Password, credential, and private-key handling
- SSH and WinRM execution
- Command approval and policy enforcement
- TLS configuration
- Installer and release artifacts
- Web and API vulnerabilities
- GitHub Actions and supply-chain issues

Please avoid testing against systems that you do not own or have explicit
permission to access.
