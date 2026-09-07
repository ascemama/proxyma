import { FormEvent, useState } from 'react';
import { Eye, EyeOff, Lock, ShieldCheck, User } from 'lucide-react';
import { useMutation, useQuery } from '@tanstack/react-query';
import { completeActivation, getActivation } from '../api/client';

export function ActivationPage({ token, onReturnToSignIn }: { token: string; onReturnToSignIn: () => void }) {
  const [password, setPassword] = useState('');
  const [confirmation, setConfirmation] = useState('');
  const [showPassword, setShowPassword] = useState(false);
  const [showConfirmation, setShowConfirmation] = useState(false);
  const [localError, setLocalError] = useState('');
  const context = useQuery({ queryKey: ['activation', token], queryFn: () => getActivation(token), retry: false });
  const activation = useMutation({ mutationFn: () => completeActivation(token, password) });
  const isReset = context.data?.purpose === 'password_reset';

  function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!password || !confirmation) return setLocalError('Both password fields are required.');
    if (password !== confirmation) return setLocalError('Passwords must match.');
    setLocalError('');
    activation.mutate();
  }

  if (activation.isSuccess) {
    return <ActivationShell><div className="activation-success"><ShieldCheck size={42} /><h2>{isReset ? 'Password reset' : 'Account activated'}</h2><p>Your password has been saved. Sign in to continue to Proxyma.</p><button className="primary-button" onClick={onReturnToSignIn}>Return to sign in</button></div></ActivationShell>;
  }

  return <ActivationShell><form className="login-card activation-card" onSubmit={submit}>
    <div className="login-card-header"><h2>{isReset ? 'Reset your password' : 'Activate your account'}</h2><p>{isReset ? 'Choose a new password for your Proxyma account' : 'Choose a password to complete your Proxyma registration'}</p></div>
    {context.data ? <div className="activation-account"><div className="activation-avatar"><User size={18} /></div><div><small>{isReset ? 'Resetting password' : 'Activating account'}</small><strong>{context.data.name} · {context.data.username}</strong></div></div> : null}
    {context.isLoading ? <p className="activation-loading">Validating activation link...</p> : null}
    {context.error ? <p className="form-error">{context.error.message}</p> : null}
    {context.data ? <>
      <PasswordField id="activation-password" label="New password" value={password} setValue={setPassword} visible={showPassword} setVisible={setShowPassword} />
      <PasswordField id="activation-confirmation" label="Confirm new password" value={confirmation} setValue={setConfirmation} visible={showConfirmation} setVisible={setShowConfirmation} />
      <div className="activation-expiry">This {isReset ? 'password reset' : 'activation'} link expires {formatExpiry(context.data.expires_at)}.</div>
      {localError || activation.error ? <p className="form-error">{localError || activation.error?.message}</p> : null}
      <button className="primary-button" disabled={activation.isPending} type="submit">{activation.isPending ? (isReset ? 'Resetting...' : 'Activating...') : (isReset ? 'Reset password' : 'Activate account')}</button>
    </> : null}
    <div className="login-footnote"><ShieldCheck size={28} /><div><strong>{isReset ? 'Secure password reset' : 'Secure account activation'}</strong><p>This link works once. Proxyma never shares your password with an administrator.</p></div></div>
  </form></ActivationShell>;
}

function ActivationShell({ children }: { children: React.ReactNode }) {
  return <main className="login-page"><section className="login-brand" aria-label="Proxyma"><div className="brand-mark large" /><div><h1>Proxyma</h1><p>Agent Proxy Monitoring</p></div></section>{children}</main>;
}

function PasswordField({ id, label, value, setValue, visible, setVisible }: { id: string; label: string; value: string; setValue: (value: string) => void; visible: boolean; setVisible: (value: boolean) => void }) {
  return <div className="field"><label htmlFor={id}>{label}</label><div className="input-shell"><Lock size={18} /><input autoComplete="new-password" id={id} type={visible ? 'text' : 'password'} value={value} onChange={(e) => setValue(e.target.value)} /><button className="icon-button" type="button" aria-label={visible ? `Hide ${label.toLowerCase()}` : `Show ${label.toLowerCase()}`} onClick={() => setVisible(!visible)}>{visible ? <EyeOff size={18} /> : <Eye size={18} />}</button></div></div>;
}

function formatExpiry(value: string) {
  const date = new Date(value);
  const remaining = date.getTime() - Date.now();
  if (remaining <= 0) return 'now';
  const hours = Math.floor(remaining / 3_600_000);
  const minutes = Math.max(1, Math.floor((remaining % 3_600_000) / 60_000));
  return hours ? `in ${hours}h ${minutes}m` : `in ${minutes}m`;
}
