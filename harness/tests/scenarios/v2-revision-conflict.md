# Reject stale task mutation

Given a task at revision 7, a mutation with `expected_revision: 6` fails with
`state_conflict` and does not alter JSON or Markdown state.
