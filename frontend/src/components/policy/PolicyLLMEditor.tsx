import type { LLMProfile } from '../../api/client';

type PolicyLLMEditorProps = {
  enabled: boolean;
  profileID: string;
  prompt: string;
  profiles: LLMProfile[];
  readOnly?: boolean;
  onEnabledChange: (value: boolean) => void;
  onProfileChange: (value: string) => void;
  onPromptChange: (value: string) => void;
};

export function PolicyLLMEditor({ enabled, profileID, prompt, profiles, readOnly = false, onEnabledChange, onProfileChange, onPromptChange }: PolicyLLMEditorProps) {
  return <section className="policy-llm-section">
    <div className="policy-section-heading"><div><h3>LLM fallback</h3><p>Only commands that match neither list are sent to the selected provider. Invalid or unavailable answers require approval.</p></div></div>
    <label className="toggle-field"><input disabled={readOnly} type="checkbox" checked={enabled} onChange={(event) => onEnabledChange(event.target.checked)} /><span>Enable LLM fallback</span></label>
    {enabled ? <div className="dialog-grid policy-llm-grid"><label className="dialog-field"><span>LLM profile</span><select disabled={readOnly} value={profileID} onChange={(event) => onProfileChange(event.target.value)}><option value="">Select a profile</option>{profiles.map((profile) => <option disabled={!profile.enabled || !profile.has_api_key} key={profile.id} value={profile.id}>{profile.name} · {profile.provider} / {profile.model}</option>)}</select></label><label className="dialog-field"><span>Policy prompt</span><textarea disabled={readOnly} value={prompt} onChange={(event) => onPromptChange(event.target.value)} placeholder="Approve only commands that are clearly read-only with very high certainty." rows={4} /></label></div> : null}
  </section>;
}
