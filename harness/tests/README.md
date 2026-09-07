# Proxyma Harness v2 Validation

Run from the repository root:

```powershell
powershell.exe -ExecutionPolicy Bypass -File .\harness\tests\validate-harness-assets.ps1
```

The validator checks v2 configuration and task examples, the shared CLI-only safety
contract, Codex and Claude Code adapter coverage, scenario presence, forbidden raw
authentication/API instructions, and the 4,000-character adapter and 10,000-character
loaded-context budgets.

Go tests provide integration coverage for server idempotency, JWT authentication,
target resolution, uncertain-submission replay, polling, revisions, journaling, and
summary generation. An authenticated Claude Code smoke run is intentionally opt-in so
normal CI requires no model credentials.
