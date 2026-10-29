import { ChevronDown, FileUp, Plus, Trash2 } from 'lucide-react';
import { useRef } from 'react';
import type { PolicyRule } from '../../api/client';

export const TEMPLATE_TYPES = ['ipv4', 'ipv6', 'hostname', 'port', 'path', 'relative_path', 'service', 'username', 'integer'] as const;

type PolicyRuleEditorProps = {
  rules: PolicyRule[];
  onChange: (rules: PolicyRule[]) => void;
  onImport: () => void;
  canImport?: boolean;
  readOnly?: boolean;
};

export function PolicyRuleEditor({ rules, onChange, onImport, canImport = true, readOnly = false }: PolicyRuleEditorProps) {
  const addRuleMenu = useRef<HTMLDetailsElement>(null);

  function addRule(effect: PolicyRule['effect']) {
    onChange([...rules, { id: crypto.randomUUID(), effect, command_template: '', reason: '' }]);
    addRuleMenu.current?.removeAttribute('open');
  }

  function updateRule(id: string, patch: Partial<PolicyRule>) {
    onChange(rules.map((rule) => (rule.id === id ? { ...rule, ...patch } : rule)));
  }

  function removeRule(id: string) {
    onChange(rules.filter((rule) => rule.id !== id));
  }

  return <section className="policy-rule-section">
    <div className="policy-section-heading">
      <div><h3>Command rules</h3><p>Blacklist rules always block matching commands. Whitelist rules run matching commands without approval.</p></div>
      {!readOnly ? <div className="policy-section-actions">
        {canImport ? <button className="ghost-button" type="button" onClick={onImport}><FileUp size={15} /> Import JSON</button> : null}
        <details className="policy-add-rule-menu" ref={addRuleMenu}>
          <summary className="ghost-button"><Plus size={15} /> Add rule <ChevronDown size={14} /></summary>
          <div className="policy-add-rule-popover" role="menu" aria-label="Rule type">
            <button type="button" role="menuitem" onClick={() => addRule('deny')}><span className="policy-rule-type deny">Blacklist rule</span><small>Always block matching commands</small></button>
            <button type="button" role="menuitem" onClick={() => addRule('allow')}><span className="policy-rule-type allow">Whitelist rule</span><small>Run matching commands without approval</small></button>
          </div>
        </details>
      </div> : null}
    </div>
    {rules.length === 0 ? <div className="policy-empty-rules">No rules configured.</div> : <div className="policy-rule-table-wrap">
      <table className="policy-rule-table">
        <thead><tr><th scope="col">Type</th><th scope="col">Command template</th><th scope="col">Reason</th><th scope="col"><span className="sr-only">Actions</span></th></tr></thead>
        <tbody>{rules.map((rule) => {
          const label = rule.effect === 'deny' ? 'Blacklist' : 'Whitelist';
          return <tr key={rule.id}>
            <td><span className={`policy-rule-type ${rule.effect}`}>{label}</span></td>
            <td><label className="policy-inline-field"><span className="sr-only">Command template for {label.toLowerCase()} rule</span><input disabled={readOnly} value={rule.command_template ?? ''} onChange={(event) => updateRule(rule.id, { command_template: event.target.value })} placeholder="ping {ipv4}" /><small>Types: {TEMPLATE_TYPES.map((type) => `{${type}}`).join(', ')}</small></label></td>
            <td><label className="policy-inline-field"><span className="sr-only">Reason for {label.toLowerCase()} rule</span><input disabled={readOnly} value={rule.reason ?? ''} onChange={(event) => updateRule(rule.id, { reason: event.target.value })} placeholder="Why this rule exists" /></label></td>
            <td>{!readOnly ? <button className="icon-button bordered danger" type="button" aria-label={`Remove ${label.toLowerCase()} rule`} onClick={() => removeRule(rule.id)}><Trash2 size={15} /></button> : null}</td>
          </tr>;
        })}</tbody>
      </table>
    </div>}
  </section>;
}
