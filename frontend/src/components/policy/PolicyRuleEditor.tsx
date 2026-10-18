import { Plus, Trash2 } from 'lucide-react';
import type { PolicyRule } from '../../api/client';

export const TEMPLATE_TYPES = ['ipv4', 'ipv6', 'hostname', 'port', 'path', 'relative_path', 'service', 'username', 'integer'] as const;

type PolicyRuleEditorProps = {
  title: string;
  effect: 'allow' | 'deny';
  rules: PolicyRule[];
  onChange: (rules: PolicyRule[]) => void;
  onImport: () => void;
  canImport?: boolean;
  readOnly?: boolean;
};

export function PolicyRuleEditor({ title, effect, rules, onChange, onImport, canImport = true, readOnly = false }: PolicyRuleEditorProps) {
  function addRule() {
    onChange([...rules, { id: crypto.randomUUID(), effect, command_template: '', reason: '' }]);
  }

  function updateRule(id: string, patch: Partial<PolicyRule>) {
    onChange(rules.map((rule) => (rule.id === id ? { ...rule, ...patch } : rule)));
  }

  function removeRule(id: string) {
    onChange(rules.filter((rule) => rule.id !== id));
  }

  return (
    <section className="policy-rule-section">
      <div className="policy-section-heading">
        <div>
          <h3>{title}</h3>
          <p>{effect === 'deny' ? 'Commands matching these templates are always blocked.' : 'Commands matching these templates run without approval unless another policy denies them.'}</p>
        </div>
        {!readOnly ? <div className="policy-section-actions">{canImport ? <button className="ghost-button" type="button" onClick={onImport}>Import JSON</button> : null}<button className="ghost-button" type="button" onClick={addRule}><Plus size={15} /> Add rule</button></div> : null}
      </div>
      {rules.length === 0 ? <div className="policy-empty-rules">No rules configured.</div> : null}
      {rules.map((rule) => (
        <div className="policy-rule-row" key={rule.id}>
          <label className="dialog-field policy-template-field">
            <span>Command template</span>
            <input disabled={readOnly} value={rule.command_template ?? ''} onChange={(event) => updateRule(rule.id, { command_template: event.target.value })} placeholder="ping {ipv4}" />
            <small>Types: {TEMPLATE_TYPES.map((type) => `{${type}}`).join(', ')}</small>
          </label>
          <label className="dialog-field policy-reason-field">
            <span>Reason</span>
            <input disabled={readOnly} value={rule.reason ?? ''} onChange={(event) => updateRule(rule.id, { reason: event.target.value })} placeholder="Why this rule exists" />
          </label>
          {!readOnly ? <button className="icon-button bordered danger" type="button" aria-label="Remove rule" onClick={() => removeRule(rule.id)}><Trash2 size={15} /></button> : null}
        </div>
      ))}
    </section>
  );
}
