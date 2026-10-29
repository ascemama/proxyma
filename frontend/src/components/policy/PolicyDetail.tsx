import { useEffect, useRef, useState, type FormEvent } from 'react';
import { Save, ShieldCheck } from 'lucide-react';
import type { LLMProfile, Policy, PolicyMutationInput, PolicyRule, Target } from '../../api/client';
import { PolicyAssignmentPanel } from './PolicyAssignmentPanel';
import { PolicyLLMEditor } from './PolicyLLMEditor';
import { PolicyRuleEditor } from './PolicyRuleEditor';

type PolicyDetailProps = { policy?: Policy; policies: Policy[]; targets: Target[]; llmProfiles: LLMProfile[]; isSaving: boolean; error: Error | null; readOnly?: boolean; onCancel: () => void; onSave: (input: PolicyMutationInput, targetIDs: string[]) => Promise<void>; onImport: () => void };

export function PolicyDetail({ policy, policies, targets, llmProfiles, isSaving, error, readOnly = false, onCancel, onSave, onImport }: PolicyDetailProps) {
  const [name, setName] = useState('');
  const [description, setDescription] = useState('');
  const [enabled, setEnabled] = useState(true);
  const [rules, setRules] = useState<PolicyRule[]>([]);
  const [targetIDs, setTargetIDs] = useState<string[]>([]);
  const [llmEnabled, setLlmEnabled] = useState(false);
  const [llmProfileID, setLlmProfileID] = useState('');
  const [llmPrompt, setLlmPrompt] = useState('');
  const [localError, setLocalError] = useState('');
  const initialSnapshot = useRef('');

  function snapshot(values: { name: string; description: string; enabled: boolean; rules: PolicyRule[]; targetIDs: string[]; llmEnabled: boolean; llmProfileID: string; llmPrompt: string }) {
    return JSON.stringify({ ...values, targetIDs: [...values.targetIDs].sort() });
  }

  useEffect(() => {
    const nextName = policy?.name ?? '';
    const nextDescription = policy?.description ?? '';
    const nextEnabled = policy?.enabled ?? true;
    const nextRules = policy?.rules ?? [];
    const nextTargetIDs = (policy?.assigned_target_ids ?? []).filter((id) => targets.some((target) => target.id === id));
    const nextLLMEnabled = policy?.llm?.enabled ?? false;
    const nextLLMProfileID = policy?.llm?.profile_id ?? '';
    const nextLLMPrompt = policy?.llm?.prompt ?? '';
    setName(nextName); setDescription(nextDescription); setEnabled(nextEnabled); setRules(nextRules); setTargetIDs(nextTargetIDs); setLlmEnabled(nextLLMEnabled); setLlmProfileID(nextLLMProfileID); setLlmPrompt(nextLLMPrompt); setLocalError('');
    initialSnapshot.current = snapshot({ name: nextName, description: nextDescription, enabled: nextEnabled, rules: nextRules, targetIDs: nextTargetIDs, llmEnabled: nextLLMEnabled, llmProfileID: nextLLMProfileID, llmPrompt: nextLLMPrompt });
  }, [policy?.id, targets]);

  const isDirty = initialSnapshot.current !== snapshot({ name, description, enabled, rules, targetIDs, llmEnabled, llmProfileID, llmPrompt });

  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault(); setLocalError('');
    if (!name.trim()) { setLocalError('Policy name is required.'); return; }
    if (rules.some((rule) => !rule.command_template?.trim())) { setLocalError('Every rule needs a command template.'); return; }
    try {
      const input = { name: name.trim(), description: description.trim(), enabled, rules, llm: { enabled: llmEnabled, profile_id: llmProfileID.trim(), prompt: llmPrompt.trim() } };
      await onSave(input, targetIDs);
      initialSnapshot.current = snapshot({ name: input.name ?? '', description: input.description ?? '', enabled: input.enabled ?? true, rules: input.rules ?? [], targetIDs, llmEnabled, llmProfileID, llmPrompt });
    } catch { /* parent error is rendered */ }
  }

  return <section className="panel policy-detail-panel"><div className="panel-heading policy-detail-heading"><div className="policy-detail-title"><ShieldCheck size={17} aria-hidden="true" /><div><h2>{policy ? name || 'Policy details' : 'New policy'}</h2><small>{policy ? `Policy details · owned by ${policy.owner_user_id}` : 'The policy will be owned by you.'}</small></div></div><div className="section-actions policy-detail-actions"><label className="toggle-field"><input disabled={readOnly} type="checkbox" checked={enabled} onChange={(event) => setEnabled(event.target.checked)} /><span>Enabled</span></label><button aria-label={readOnly ? 'Close policy details' : 'Cancel policy edit'} className="ghost-button" type="button" onClick={onCancel}>{readOnly ? 'Close' : 'Cancel'}</button>{!readOnly ? <button aria-label={isSaving ? 'Saving policy' : 'Save policy from toolbar'} className="primary-action" type="submit" form="policy-detail-form" disabled={!isDirty || isSaving}><Save size={15} />{isSaving ? 'Saving…' : 'Save policy'}</button> : null}</div></div><form id="policy-detail-form" onSubmit={(event) => void submit(event)}><div className="dialog-grid policy-metadata"><label className="dialog-field"><span>Name</span><input disabled={readOnly} value={name} onChange={(event) => setName(event.target.value)} placeholder="Safe diagnostics" /></label><label className="dialog-field wide"><span>Description</span><input disabled={readOnly} value={description} onChange={(event) => setDescription(event.target.value)} placeholder="Read-only diagnostic commands" /></label></div><PolicyRuleEditor rules={rules} onChange={setRules} onImport={onImport} canImport={!!policy} readOnly={readOnly} /><PolicyLLMEditor enabled={llmEnabled} profileID={llmProfileID} prompt={llmPrompt} profiles={llmProfiles} readOnly={readOnly} onEnabledChange={setLlmEnabled} onProfileChange={setLlmProfileID} onPromptChange={setLlmPrompt} /><PolicyAssignmentPanel policies={policies} targets={targets} selectedTargetIDs={targetIDs} assignedTargetIDs={policy?.assigned_target_ids ?? []} onChange={setTargetIDs} readOnly={readOnly} />{localError || error ? <div className="form-error compact">{localError || error?.message}</div> : null}<div className="dialog-actions"><button className="ghost-button" type="button" onClick={onCancel}>{readOnly ? 'Close' : 'Cancel'}</button>{!readOnly ? <button className="primary-action" type="submit" disabled={!isDirty || isSaving}>{isSaving ? 'Saving…' : 'Save policy'}</button> : null}</div></form></section>;
}
