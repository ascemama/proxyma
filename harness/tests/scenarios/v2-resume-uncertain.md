# Resume an uncertain submission

Given a v2 task whose operation phase is `submitting` or `submission_unknown`,
`proxyma-agent resume` resubmits the exact persisted request with the same operation ID.
Proxyma returns the original task ID and no duplicate command is created.
