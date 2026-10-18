import type { Target } from '../../api/client';

type PolicyAssignmentPanelProps = {
  targets: Target[];
  selectedTargetIDs: string[];
  assignedTargetIDs: string[];
  onChange: (targetIDs: string[]) => void;
  readOnly?: boolean;
};

export function PolicyAssignmentPanel({ targets, selectedTargetIDs, assignedTargetIDs, onChange, readOnly = false }: PolicyAssignmentPanelProps) {
  const selected = new Set(selectedTargetIDs);
  const initiallyAssigned = new Set(assignedTargetIDs);
  function toggle(id: string) {
    if (initiallyAssigned.has(id)) return;
    const next = new Set(selected);
    if (next.has(id)) next.delete(id); else next.add(id);
    onChange([...next]);
  }
  function selectAll() { onChange(targets.map((target) => target.id)); }

  return (
    <section className="policy-assignment-section">
      <div className="policy-section-heading"><div><h3>Assigned targets</h3><p>{readOnly ? 'Targets assigned to this shared policy.' : 'Selecting an unassigned target assigns this policy and replaces its current policy. Reassign an existing target from the target editor.'}</p></div>{!readOnly ? <div className="policy-section-actions"><button className="ghost-button" type="button" onClick={selectAll}>All current targets</button></div> : null}</div>
      {targets.length === 0 ? <div className="policy-empty-rules">No targets are available.</div> : <div className="policy-target-grid">{targets.map((target) => <label className="policy-target-option" key={target.id}><input type="checkbox" disabled={readOnly || initiallyAssigned.has(target.id)} checked={selected.has(target.id)} onChange={() => toggle(target.id)} /><span><strong>{target.hostname || target.ip || target.id}</strong><small>{target.description || target.transport}{initiallyAssigned.has(target.id) ? ' · assigned' : target.policy_id ? ` · currently assigned to ${target.policy_id}` : ''}</small></span></label>)}</div>}
    </section>
  );
}
