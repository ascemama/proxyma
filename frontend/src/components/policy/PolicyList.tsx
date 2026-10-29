import { Pencil, Trash2 } from 'lucide-react';
import type { KeyboardEvent } from 'react';
import type { Policy } from '../../api/client';

type PolicyListProps = { policies: Policy[]; selectedID?: string; onSelect: (policy: Policy) => void; onDelete: (policy: Policy) => void; canModify: (policy: Policy) => boolean };

export function PolicyList({ policies, selectedID, onSelect, onDelete, canModify }: PolicyListProps) {
  function selectWithKeyboard(event: KeyboardEvent<HTMLTableRowElement>, policy: Policy) {
    if (event.key === 'Enter' || event.key === ' ') {
      event.preventDefault();
      onSelect(policy);
    }
  }

  return <section className="panel policy-list-panel">
    <div className="panel-heading policy-list-heading">
      <div className="policy-list-heading-content"><div><h2>All policies</h2><p>Select a policy to view or edit its definition below.</p></div><span className="panel-count">{policies.length}</span></div>
    </div>
    {policies.length === 0 ? <div className="empty-state">No policies are configured.</div> : <div className="policy-table-wrap">
      <table className="policy-table">
        <thead><tr><th scope="col">Policy</th><th scope="col">Owner</th><th scope="col">Blacklist</th><th scope="col">Whitelist</th><th scope="col">Targets</th><th scope="col">Status</th><th scope="col"><span className="sr-only">Actions</span></th></tr></thead>
        <tbody>{policies.map((policy) => <tr className={`policy-list-row ${selectedID === policy.id ? 'selected' : ''}`} key={policy.id} tabIndex={0} aria-selected={selectedID === policy.id} onClick={() => onSelect(policy)} onKeyDown={(event) => selectWithKeyboard(event, policy)}>
          <th scope="row"><button className="policy-table-link" type="button" onClick={(event) => { event.stopPropagation(); onSelect(policy); }}><strong>{policy.name}</strong><small>{policy.description || 'No description'}</small></button></th>
          <td className="policy-list-owner">{policy.owner_user_id}</td>
          <td className="policy-list-stat">{policy.blacklist_rule_count}</td>
          <td className="policy-list-stat">{policy.whitelist_rule_count}</td>
          <td className="policy-list-stat">{policy.assigned_target_ids.length}</td>
          <td><span className="policy-list-status"><span className={policy.enabled ? 'state-dot online' : 'state-dot'} />{policy.enabled ? 'Enabled' : 'Disabled'}</span></td>
          <td><div className="policy-list-actions">{canModify(policy) ? <><button className="icon-button bordered" type="button" aria-label={`Edit ${policy.name}`} onClick={(event) => { event.stopPropagation(); onSelect(policy); }}><Pencil size={15} /></button><button className="icon-button bordered danger" type="button" aria-label={`Delete ${policy.name}`} onClick={(event) => { event.stopPropagation(); onDelete(policy); }}><Trash2 size={15} /></button></> : null}</div></td>
        </tr>)}</tbody>
      </table>
    </div>}
  </section>;
}
