import { useEffect, useMemo, useRef, useState, type FormEvent } from 'react';
import type { Policy, PolicyMutationInput, PolicyRule, Target } from '../../api/client';
import { PolicyAssignmentPanel } from './PolicyAssignmentPanel';
import { PolicyRuleEditor } from './PolicyRuleEditor';

type PolicyDetailProps = { policy?: Policy; targets: Target[]; isSaving: boolean; error: Error | null; readOnly?: boolean; onCancel: () => void; onSave: (input: PolicyMutationInput, targetIDs: string[]) => Promise<void>; onImport: (effect: 'allow' | 'deny') => void };

export function PolicyDetail({ policy, targets, isSaving, error, readOnly = false, onCancel, onSave, onImport }: PolicyDetailProps) {
  const [name, setName] = useState('');
  const [description, setDescription] = useState('');
  const [enabled, setEnabled] = useState(true);
  const [rules, setRules] = useState<PolicyRule[]>([]);
  const [targetIDs, setTargetIDs] = useState<string[]>([]);
  const [localError, setLocalError] = useState('');
  const initialSnapshot = useRef('');

  function snapshot(values: { name: string; description: string; enabled: boolean; rules: PolicyRule[]; targetIDs: string[] }) {
    return JSON.stringify({ ...values, targetIDs: [...values.targetIDs].sort() });
  }

  useEffect(() => {
    const nextName = policy?.name ?? '';
    const nextDescription = policy?.description ?? '';
    const nextEnabled = policy?.enabled ?? true;
    const nextRules = policy?.rules ?? [];
    const nextTargetIDs = (policy?.assigned_target_ids ?? []).filter((id) => targets.some((target) => target.id === id));
    setName(nextName); setDescription(nextDescription); setEnabled(nextEnabled); setRules(nextRules); setTargetIDs(nextTargetIDs); setLocalError('');
    initialSnapshot.current = snapshot({ name: nextName, description: nextDescription, enabled: nextEnabled, rules: nextRules, targetIDs: nextTargetIDs });
  }, [policy?.id, targets]);

  const blacklist = useMemo(() => rules.filter((rule) => rule.effect === 'deny'), [rules]);
  const whitelist = useMemo(() => rules.filter((rule) => rule.effect === 'allow'), [rules]);
  const isDirty = initialSnapshot.current !== snapshot({ name, description, enabled, rules, targetIDs });
  function replaceEffect(effect: 'allow' | 'deny', next: PolicyRule[]) { setRules([...rules.filter((rule) => rule.effect !== effect), ...next]); }

  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault(); setLocalError('');
    if (!name.trim()) { setLocalError('Policy name is required.'); return; }
    if (rules.some((rule) => !rule.command_template?.trim())) { setLocalError('Every rule needs a command template.'); return; }
    try {
      const input = { name: name.trim(), description: description.trim(), enabled, rules };
      await onSave(input, targetIDs);
      initialSnapshot.current = snapshot({ name: input.name ?? '', description: input.description ?? '', enabled: input.enabled ?? true, rules: input.rules ?? [], targetIDs });
    } catch { /* parent error is rendered */ }
  }

  return <section className="panel policy-detail-panel"><div className="panel-heading"><div><h2>{policy ? 'Policy details' : 'New policy'}</h2><small>{policy ? `Owned by ${policy.owner_user_id}` : 'The policy will be owned by you.'}</small></div></div><form onSubmit={(event) => void submit(event)}><div className="dialog-grid policy-metadata"><label className="dialog-field"><span>Name</span><input disabled={readOnly} value={name} onChange={(event) => setName(event.target.value)} placeholder="Safe diagnostics" /></label><label className="toggle-field"><input disabled={readOnly} type="checkbox" checked={enabled} onChange={(event) => setEnabled(event.target.checked)} /><span>Enabled</span></label><label className="dialog-field wide"><span>Description</span><input disabled={readOnly} value={description} onChange={(event) => setDescription(event.target.value)} placeholder="Read-only diagnostic commands" /></label></div><PolicyRuleEditor title="Blacklist" effect="deny" rules={blacklist} onChange={(next) => replaceEffect('deny', next)} onImport={() => onImport('deny')} canImport={!!policy} readOnly={readOnly} /><PolicyRuleEditor title="Whitelist" effect="allow" rules={whitelist} onChange={(next) => replaceEffect('allow', next)} onImport={() => onImport('allow')} canImport={!!policy} readOnly={readOnly} /><PolicyAssignmentPanel targets={targets} selectedTargetIDs={targetIDs} assignedTargetIDs={policy?.assigned_target_ids ?? []} onChange={setTargetIDs} readOnly={readOnly} />{localError || error ? <div className="form-error compact">{localError || error?.message}</div> : null}<div className="dialog-actions"><button className="ghost-button" type="button" onClick={onCancel}>{readOnly ? 'Close' : 'Cancel'}</button>{!readOnly ? <button className="primary-action" type="submit" disabled={!isDirty || isSaving}>{isSaving ? 'Saving…' : 'Save policy'}</button> : null}</div></form></section>;
}
