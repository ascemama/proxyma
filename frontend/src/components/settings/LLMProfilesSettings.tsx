import { useEffect, useState, type FormEvent } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { CheckCircle2, Pencil, Plus, RefreshCw, Trash2, X } from 'lucide-react';
import {
  createLLMProfile,
  deleteLLMProfile,
  getLLMProfiles,
  testLLMProfile,
  updateLLMProfile,
  type LLMProfile,
  type LLMProfileMutationInput,
  type LLMProvider,
  type UserProfile
} from '../../api/client';

type LLMProfilesSettingsProps = { csrfToken: string; user: UserProfile };

const blankProfile: LLMProfileMutationInput = { name: '', provider: 'openai', model: 'gpt-4o-mini', base_url: '', api_key: '', timeout_seconds: 30, enabled: true };

export function LLMProfilesSettings({ csrfToken, user }: LLMProfilesSettingsProps) {
  const queryClient = useQueryClient();
  const profilesQuery = useQuery({ queryKey: ['llm-profiles'], queryFn: getLLMProfiles });
  const [editing, setEditing] = useState<LLMProfile | null | undefined>(undefined);
  const [draft, setDraft] = useState<LLMProfileMutationInput>(blankProfile);
  const [localError, setLocalError] = useState('');
  const [tested, setTested] = useState(false);
  const saveMutation = useMutation({
    mutationFn: () => editing ? updateLLMProfile(editing.id, draft, csrfToken) : createLLMProfile(draft, csrfToken),
    onSuccess: () => { void queryClient.invalidateQueries({ queryKey: ['llm-profiles'] }); setEditing(undefined); setDraft(blankProfile); }
  });
  const deleteMutation = useMutation({
    mutationFn: (id: string) => deleteLLMProfile(id, csrfToken),
    onSuccess: () => void queryClient.invalidateQueries({ queryKey: ['llm-profiles'] })
  });
  const testMutation = useMutation({ mutationFn: () => testLLMProfile({
    name: draft.name,
    provider: draft.provider,
    model: draft.model,
    base_url: draft.base_url,
    api_key: draft.api_key ?? '',
    timeout_seconds: draft.timeout_seconds
  }, csrfToken), onSuccess: () => setTested(true) });

  useEffect(() => {
    if (!draft.base_url) setDraft((value) => ({ ...value, base_url: defaultBaseURL(draft.provider) }));
  }, [draft.provider, draft.base_url]);

  function edit(profile?: LLMProfile) {
    setLocalError(''); setTested(false); setEditing(profile ?? null);
    setDraft(profile ? { name: profile.name, provider: profile.provider, model: profile.model, base_url: profile.base_url, api_key: '', timeout_seconds: profile.timeout_seconds, enabled: profile.enabled } : { ...blankProfile });
  }

  function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault(); setLocalError(''); setTested(false);
    if (!draft.name.trim() || !draft.model.trim()) { setLocalError('Name and model are required.'); return; }
    if (!editing && !draft.api_key?.trim()) { setLocalError('An API key is required for a new profile.'); return; }
    void saveMutation.mutateAsync().catch(() => undefined);
  }

  function update<K extends keyof LLMProfileMutationInput>(key: K, value: LLMProfileMutationInput[K]) { setDraft((current) => ({ ...current, [key]: value })); setLocalError(''); setTested(false); }

  return <div className="llm-settings-content">
    <div className="settings-subheading"><div><h3>LLM profiles</h3><p>Store provider credentials on the server and reuse a profile from a policy.</p></div><button className="ghost-button" type="button" onClick={() => void profilesQuery.refetch()}><RefreshCw size={15} /> Refresh</button></div>
    {profilesQuery.isLoading ? <div className="loading-state">Loading LLM profiles…</div> : null}
    {profilesQuery.error ? <div className="form-error compact">{profilesQuery.error.message}</div> : null}
    {!profilesQuery.isLoading && (profilesQuery.data ?? []).length === 0 ? <div className="policy-empty-rules">No LLM profiles configured.</div> : null}
    <div className="llm-profile-list">{(profilesQuery.data ?? []).map((profile) => <div className="llm-profile-row" key={profile.id}><div><strong>{profile.name}</strong><small>{profile.provider} · {profile.model} · {profile.has_api_key ? 'key configured' : 'missing key'}</small></div><span className={`state-dot ${profile.enabled ? 'online' : ''}`} />{user.admin || profile.owner_user_id === user.id ? <><button className="icon-button" type="button" aria-label={`Edit ${profile.name}`} onClick={() => edit(profile)}><Pencil size={15} /></button><button className="icon-button danger" type="button" aria-label={`Delete ${profile.name}`} onClick={() => { if (window.confirm(`Delete LLM profile “${profile.name}”?`)) void deleteMutation.mutateAsync(profile.id); }}><Trash2 size={15} /></button></> : null}</div>)}</div>
    {editing === undefined ? <button className="ghost-button" type="button" onClick={() => edit()}><Plus size={15} /> Add profile</button> : null}
    {editing !== undefined ? <form className="llm-profile-form" onSubmit={submit}><div className="dialog-grid"><label className="dialog-field"><span>Name</span><input value={draft.name} onChange={(event) => update('name', event.target.value)} placeholder="OpenAI safety reviewer" /></label><label className="dialog-field"><span>Provider</span><select value={draft.provider} onChange={(event) => { const provider = event.target.value as LLMProvider; update('provider', provider); update('base_url', defaultBaseURL(provider)); }}><option value="openai">OpenAI</option><option value="openrouter">OpenRouter</option></select></label><label className="dialog-field"><span>Model</span><input value={draft.model} onChange={(event) => update('model', event.target.value)} placeholder="gpt-4o-mini" /></label><label className="dialog-field"><span>Timeout (seconds)</span><input min={1} max={120} type="number" value={draft.timeout_seconds} onChange={(event) => update('timeout_seconds', Number(event.target.value))} /></label><label className="dialog-field wide"><span>HTTPS endpoint</span><input value={draft.base_url ?? ''} onChange={(event) => update('base_url', event.target.value)} placeholder="Provider default" /></label><label className="dialog-field wide"><span>API key {editing ? '(leave blank to keep current)' : ''}</span><input type="password" value={draft.api_key ?? ''} onChange={(event) => update('api_key', event.target.value)} autoComplete="new-password" /></label></div><label className="toggle-field"><input type="checkbox" checked={draft.enabled ?? true} onChange={(event) => update('enabled', event.target.checked)} /><span>Enabled</span></label>{localError || saveMutation.error?.message || testMutation.error?.message ? <div className="form-error compact">{localError || saveMutation.error?.message || testMutation.error?.message}</div> : null}{tested ? <div className="form-success" role="status"><CheckCircle2 size={17} /> Connection test succeeded.</div> : null}<div className="dialog-actions"><button className="ghost-button" type="button" onClick={() => { setEditing(undefined); setDraft(blankProfile); }}><X size={15} /> Cancel</button><button className="ghost-button" type="button" disabled={testMutation.isPending || !draft.api_key} onClick={() => void testMutation.mutateAsync()}>{testMutation.isPending ? 'Testing…' : 'Test connection'}</button><button className="primary-action" type="submit" disabled={saveMutation.isPending}>{saveMutation.isPending ? 'Saving…' : 'Save profile'}</button></div></form> : null}
  </div>;
}

function defaultBaseURL(provider: LLMProvider) {
  if (provider === 'openrouter') return 'https://openrouter.ai/api/v1';
  if (provider === 'claude') return 'https://api.anthropic.com/v1';
  return 'https://api.openai.com/v1';
}
