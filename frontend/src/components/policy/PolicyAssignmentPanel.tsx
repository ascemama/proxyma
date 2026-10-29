import type { Policy, Target } from '../../api/client';

type PolicyAssignmentPanelProps = {
  policies: Policy[];
  targets: Target[];
  selectedTargetIDs: string[];
  assignedTargetIDs: string[];
  onChange: (targetIDs: string[]) => void;
  readOnly?: boolean;
};

export function PolicyAssignmentPanel({ policies, targets, selectedTargetIDs, assignedTargetIDs, onChange, readOnly = false }: PolicyAssignmentPanelProps) {
  const selected = new Set(selectedTargetIDs);
  const initiallyAssigned = new Set(assignedTargetIDs);

  function targetLabel(target: Target) {
    return target.hostname || target.ip || target.id;
  }

  function toggle(id: string) {
    if (initiallyAssigned.has(id)) return;
    const next = new Set(selected);
    if (next.has(id)) next.delete(id); else next.add(id);
    onChange([...next]);
  }

  function selectAll() {
    onChange(targets.map((target) => target.id));
  }

  function currentPolicyName(target: Target) {
    if (!target.policy_id) return 'Unassigned';
    return policies.find((policy) => policy.id === target.policy_id)?.name ?? target.policy_id;
  }

  return <section className="policy-assignment-section">
    <div className="policy-section-heading"><div><h3>Assigned targets</h3><p>{readOnly ? 'Targets assigned to this shared policy.' : 'Selecting an unassigned target assigns this policy and replaces its current policy. Reassign an existing target from the target editor.'}</p></div>{!readOnly ? <div className="policy-section-actions"><button className="ghost-button" type="button" onClick={selectAll}>All current targets</button></div> : null}</div>
    {targets.length === 0 ? <div className="policy-empty-rules">No targets are available.</div> : <div className="policy-assignment-table-wrap">
      <table className="policy-assignment-table">
        <thead><tr><th scope="col">Target</th><th scope="col">Transport</th><th scope="col">Current policy</th><th scope="col">Assignment</th></tr></thead>
        <tbody>{targets.map((target) => {
          const assigned = initiallyAssigned.has(target.id);
          const checked = selected.has(target.id);
          return <tr key={target.id} className={assigned ? 'assigned' : ''}>
            <th scope="row"><strong>{targetLabel(target)}</strong><small>{target.description || target.id}</small></th>
            <td>{target.transport.toUpperCase()}</td>
            <td>{currentPolicyName(target)}</td>
            <td><label className={`policy-assignment-control ${assigned ? 'assigned' : ''}`}><input type="checkbox" disabled={readOnly || assigned} checked={checked} aria-label={`${assigned ? 'Assigned' : 'Assign'} ${targetLabel(target)}`} onChange={() => toggle(target.id)} /><span>{assigned ? 'Assigned' : checked ? 'Assign on save' : 'Assign'}</span></label></td>
          </tr>;
        })}</tbody>
      </table>
    </div>}
  </section>;
}
