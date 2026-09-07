import { FormEvent, useState } from 'react';
import { Eye, EyeOff, Lock, ShieldCheck, User } from 'lucide-react';
import { useMutation } from '@tanstack/react-query';
import { login, type UserSession } from '../api/client';

type LoginPageProps = {
  onLoggedIn: (session: UserSession) => void;
};

export function LoginPage({ onLoggedIn }: LoginPageProps) {
  const [username, setUsername] = useState('');
  const [password, setPassword] = useState('');
  const [showPassword, setShowPassword] = useState(false);
  const [validationError, setValidationError] = useState('');

  const loginMutation = useMutation({
    mutationFn: () => login(username.trim(), password),
    onSuccess: onLoggedIn
  });

  function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!username.trim() || !password) {
      setValidationError('Username and password are required.');
      return;
    }
    setValidationError('');
    loginMutation.mutate();
  }

  const error = validationError || loginMutation.error?.message;

  return (
    <main className="login-page">
      <section className="login-brand" aria-label="Proxyma">
        <div className="brand-mark large" aria-hidden="true" />
        <div>
          <h1>Proxyma</h1>
          <p>Agent Proxy Monitoring</p>
        </div>
      </section>

      <form className="login-card" onSubmit={submit}>
        <div className="login-card-header">
          <h2>Sign in to your account</h2>
          <p>Access the privileged access console</p>
        </div>

        <div className="field">
          <label htmlFor="username">Username</label>
          <div className="input-shell">
            <User size={18} aria-hidden="true" />
            <input
              autoComplete="username"
              id="username"
              name="username"
              onChange={(event) => setUsername(event.target.value)}
              placeholder="Enter your username"
              value={username}
            />
          </div>
        </div>

        <div className="field">
          <label htmlFor="password">Password</label>
          <div className="input-shell">
            <Lock size={18} aria-hidden="true" />
            <input
              autoComplete="current-password"
              id="password"
              name="password"
              onChange={(event) => setPassword(event.target.value)}
              placeholder="Enter your password"
              type={showPassword ? 'text' : 'password'}
              value={password}
            />
            <button
              aria-label={showPassword ? 'Hide password' : 'Show password'}
              className="icon-button"
              onClick={() => setShowPassword((visible) => !visible)}
              type="button"
            >
              {showPassword ? <EyeOff size={18} /> : <Eye size={18} />}
            </button>
          </div>
        </div>

        {error ? <p className="form-error">{error}</p> : null}

        <button className="primary-button" disabled={loginMutation.isPending} type="submit">
          {loginMutation.isPending ? 'Signing in...' : 'Sign In'}
        </button>

        <div className="separator">
          <span />
          <small>or</small>
          <span />
        </div>

        <a className="forgot-link" href="#help" onClick={(event) => event.preventDefault()}>
          Forgot your password?
        </a>

        <div className="login-footnote">
          <ShieldCheck size={28} aria-hidden="true" />
          <div>
            <strong>Privileged Access Console</strong>
            <p>Authorized users only. All access is monitored and recorded.</p>
          </div>
        </div>
      </form>
    </main>
  );
}
