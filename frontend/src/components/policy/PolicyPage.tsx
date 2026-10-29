import { useMemo, useState } from 'react';
import { useMutation, useQuery } from '@tanstack/react-query';
import { Plus, RefreshCw } from 'lucide-react';
import type { UserSession, Policy, PolicyMutationInput, PolicyRuleImportResponse, PolicyRuleListDocument, Target } from '../../api/client';
import { assignPolicyTargets, createPolicy, deletePolicy, getLLMProfiles, importPolicyRuleList, updatePolicy } from '../../api/client';
import { PolicyDetail } from './PolicyDetail';
import { PolicyList } from './PolicyList';
import { PolicyRuleImportDialog } from './PolicyRuleImportDialog';

type PolicyPageProps = { session: UserSession; policies: Policy[]; targets: Target[]; error: Error | null; isLoading: boolean; onRefresh: () => void; onTargetsRefresh: () => void };

export function PolicyPage({ session, policies, targets, error, isLoading, onRefresh, onTargetsRefresh }: PolicyPageProps) {
  const [selectedID, setSelectedID] = useState<string | null>(policies[0]?.id ?? null);
  const [creating, setCreating] = useState(false);
  const [importOpen, setImportOpen] = useState(false);
  const selected = policies.find((policy) => policy.id === selectedID);
  const profilesQuery = useQuery({ queryKey: ['llm-profiles'], queryFn: getLLMProfiles });
  const ownedTargets = useMemo(() => session.user.admin ? targets : targets.filter((target) => target.user_ids.includes(session.user.id)), [session.user.admin, session.user.id, targets]);
  const saveMutation = useMutation({ mutationFn: async ({ id, input, targetIDs }: { id?: string; input: PolicyMutationInput; targetIDs: string[] }) => { const saved = id ? await updatePolicy(id, input, session.csrf_token) : await createPolicy(input, session.csrf_token); await assignPolicyTargets(saved.id, targetIDs, session.csrf_token); return saved; }, onSuccess: (saved) => { setCreating(false); setSelectedID(saved.id); void onRefresh(); void onTargetsRefresh(); } });
  const deleteMutation = useMutation({ mutationFn: (id: string) => deletePolicy(id, session.csrf_token), onSuccess: () => { setSelectedID(null); void onRefresh(); } });
  const importMutation = useMutation({ mutationFn: ({ id, document }: { id: string; document: PolicyRuleListDocument }) => importPolicyRuleList(id, document, session.csrf_token), onSuccess: () => { void onRefresh(); } });

  async function save(input: PolicyMutationInput, targetIDs: string[]) { await saveMutation.mutateAsync({ id: selected?.id, input, targetIDs }); }
  async function importRules(document: PolicyRuleListDocument): Promise<PolicyRuleImportResponse> { if (!selected) throw new Error('Save the policy before importing rules.'); return importMutation.mutateAsync({ id: selected.id, document }); }
  function startCreate() { setCreating(true); setSelectedID(null); }
  function cancelDetail() { setCreating(false); setSelectedID(policies[0]?.id ?? null); saveMutation.reset(); }
  function deleteSelected(policy: Policy) { if (window.confirm(`Delete policy “${policy.name}”?`)) void deleteMutation.mutateAsync(policy.id); }

  return <section className="policy-page"><div className="policy-page-toolbar"><div className="section-actions"><button className="ghost-button" type="button" onClick={onRefresh}><RefreshCw size={15} /> Refresh</button><button className="primary-action" type="button" onClick={startCreate}><Plus size={15} /> Add policy</button></div></div><PolicyList policies={policies} selectedID={creating ? undefined : selectedID ?? undefined} onSelect={(policy) => { setCreating(false); setSelectedID(policy.id); }} onDelete={deleteSelected} canModify={(policy) => session.user.admin || policy.owner_user_id === session.user.id} />{isLoading ? <div className="panel loading-state">Loading policies…</div> : null}{!isLoading && (creating || selected) ? <PolicyDetail policy={creating ? undefined : selected} policies={policies} targets={ownedTargets} llmProfiles={profilesQuery.data ?? []} readOnly={!creating && !!selected && !session.user.admin && selected.owner_user_id !== session.user.id} isSaving={saveMutation.isPending} error={saveMutation.error || importMutation.error || profilesQuery.error} onCancel={cancelDetail} onSave={save} onImport={() => setImportOpen(true)} /> : null}{importOpen && selected ? <PolicyRuleImportDialog error={importMutation.error} isSaving={importMutation.isPending} onClose={() => setImportOpen(false)} onImport={importRules} /> : null}{error ? <div className="form-error compact">{error.message}</div> : null}</section>;
}
