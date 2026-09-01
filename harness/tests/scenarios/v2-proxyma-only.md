# Proxyma-only target access

The adapter passes a remote payload to `proxyma-agent submit`. It never invokes SSH,
SCP, WinRM, credential lookup, or raw Proxyma HTTP. The CLI resolves the target and
constructs the canonical wrapper.
