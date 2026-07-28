import { useEffect, useState } from 'react';
import { useQuery, useQueryClient } from '@tanstack/react-query';
import { getSession, type UserSession } from './api/client';
import { Dashboard } from './components/Dashboard';
import { LoginPage } from './components/LoginPage';
import { ActivationPage } from './components/ActivationPage';
import { clearAuthenticatedTheme, normalizeTheme, setAuthenticatedTheme } from './theme';

function normalizeSession(session: UserSession): UserSession {
  return {
    ...session,
    user: {
      ...session.user,
      theme: normalizeTheme(session.user.theme)
    }
  };
}

export function App() {
  const queryClient = useQueryClient();
  const [session, setSession] = useState<UserSession | null>(null);
  const [activationToken, setActivationToken] = useState(() => activationTokenFromHash());
  const [shouldCheckSession] = useState(() => window.sessionStorage.getItem('proxyma_has_session') === 'true');
  const sessionQuery = useQuery({
    queryKey: ['session'],
    queryFn: getSession,
    enabled: shouldCheckSession
  });

  useEffect(() => {
    if (sessionQuery.data) {
      const nextSession = normalizeSession(sessionQuery.data);
      setAuthenticatedTheme(nextSession.user.theme);
      setSession(nextSession);
      queryClient.setQueryData(['session'], nextSession);
    }
  }, [sessionQuery.data]);

  useEffect(() => {
    if (sessionQuery.isError) {
      window.sessionStorage.removeItem('proxyma_has_session');
      clearAuthenticatedTheme();
    }
  }, [sessionQuery.isError]);

  useEffect(() => {
    if (activationToken) window.history.replaceState(null, '', '/#/activate');
  }, [activationToken]);

  useEffect(() => {
    const handleHashChange = () => setActivationToken(activationTokenFromHash());
    window.addEventListener('hashchange', handleHashChange);
    return () => window.removeEventListener('hashchange', handleHashChange);
  }, []);

  if (activationToken) {
    return <ActivationPage token={activationToken} onReturnToSignIn={() => { window.history.replaceState(null, '', '/'); setActivationToken(null); }} />;
  }

  if (sessionQuery.isLoading && shouldCheckSession && !session) {
    return (
      <main className="loading-screen">
        <div className="brand-mark" aria-hidden="true" />
        <p>Loading Proxyma...</p>
      </main>
    );
  }

  if (!session) {
    return (
      <LoginPage
        onLoggedIn={(nextSession) => {
          nextSession = normalizeSession(nextSession);
          window.sessionStorage.setItem('proxyma_has_session', 'true');
          setAuthenticatedTheme(nextSession.user.theme);
          setSession(nextSession);
          queryClient.setQueryData(['session'], nextSession);
        }}
      />
    );
  }

  return (
    <Dashboard
      session={session}
      onUserUpdated={(user) => {
        const nextSession = { ...session, user };
        setAuthenticatedTheme(user.theme);
        setSession(nextSession);
        queryClient.setQueryData(['session'], nextSession);
      }}
      onLoggedOut={() => {
        window.sessionStorage.removeItem('proxyma_has_session');
        clearAuthenticatedTheme();
        setSession(null);
        queryClient.clear();
      }}
    />
  );
}

function activationTokenFromHash() {
  if (!window.location.hash.startsWith('#/activate?')) return null;
  return new URLSearchParams(window.location.hash.slice(window.location.hash.indexOf('?') + 1)).get('token');
}
