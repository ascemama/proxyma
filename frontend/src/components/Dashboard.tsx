import { useEffect, useMemo, useRef, useState, type ChangeEvent, type FormEvent, type KeyboardEvent as ReactKeyboardEvent, type PointerEvent as ReactPointerEvent, type ReactNode } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import {
  AlertTriangle,
  Bot,
  Calendar,
  CheckCircle2,
  ChevronDown,
  ChevronsLeft,
  ClipboardList,
  Copy,
  Eye,
  EyeOff,
  FileClock,
  Filter,
  Gauge,
  History,
  KeyRound,
  ListChecks,
  Loader2,
  LogOut,
  Menu,
  MoreHorizontal,
  Network,
  Palette,
  Pencil,
  PlugZap,
  Plus,
  RefreshCw,
  Search,
  ScrollText,
  ShieldCheck,
  Trash2,
  Upload,
  Users,
  X
} from 'lucide-react';
import {
  changePassword,
  createAgent,
  createInteraction,
  createTarget,
  createUser,
  createPasswordResetLink,
  deleteAgent,
  deleteInteraction,
  deleteTarget,
  deleteUser,
  getAgents,
  getActivationPolicy,
  getHistory,
  getInteractions,
  getPolicies,
  getTargets,
  getUsers,
  regenerateActivationLink,
  logout,
  testTargetCredentials,
	APIError,
  type ChangePasswordInput,
  type ActivationPolicy,
  type Policy,
  updateAgent,
  updateInteraction,
  updateTarget,
  updateUser,
  updateUserSettings,
  type Agent,
  type AgentMutationInput,
  type CommandHistoryItem,
  type HistoryItem,
  type HistoryQuery,
  type InteractionInstance,
  type InteractionMutationInput,
  type Target,
  type TargetCredentialTestInput,
  type TargetMutationInput,
	type SSHHostKeyInfo,
  type UserMutationInput,
  type UserProfile,
  type UserSession
} from '../api/client';
import { applyTheme, type Theme } from '../theme';
import { showLocalInteraction } from '../environment';
import { ApplicationFooter } from './ApplicationFooter';
import { PolicyPage as PolicyManagementPage } from './policy/PolicyPage';
import { LLMProfilesSettings } from './settings/LLMProfilesSettings';

type DashboardProps = {
  session: UserSession;
  onLoggedOut: () => void;
  onUserUpdated: (user: UserProfile) => void;
};

type PageKey = 'dashboard' | 'target' | 'agent' | 'interaction' | 'user' | 'policy' | 'history';

type CredentialTestStatus = 'idle' | 'testing' | 'success' | 'error';

type NavItem = {
  label: string;
  page?: PageKey;
  icon: typeof Gauge;
  disabled?: boolean;
  adminOnly?: boolean;
};

type TargetDialogState =
  | {
      mode: 'create';
      target?: undefined;
    }
  | {
      mode: 'edit';
      target: Target;
    };

type AgentDialogState =
  | {
      mode: 'create';
      agent?: undefined;
    }
  | {
      mode: 'edit';
      agent: Agent;
    };

type InteractionDialogState =
  | {
      mode: 'choose';
      interaction?: undefined;
    }
  | {
      mode: 'create';
      interaction?: undefined;
    }
  | {
      mode: 'edit';
      interaction: InteractionInstance;
    };

type UserDialogState =
  | {
      mode: 'create';
      user?: undefined;
    }
  | {
      mode: 'edit';
      user: UserProfile;
    };

type TargetCreationPrerequisites = {
  status: 'loading' | 'error' | 'missing' | 'ready';
  message: string;
  missingAgent: boolean;
  missingPolicy: boolean;
};

type AgentCreationPrerequisites = {
  status: 'loading' | 'error' | 'missing' | 'ready';
  message: string;
};

const navItems: NavItem[] = [
  { label: 'Dashboard', page: 'dashboard', icon: Gauge },
  { label: 'Targets', page: 'target', icon: Network },
  { label: 'Agents', page: 'agent', icon: Bot },
  { label: 'Interactions', page: 'interaction', icon: ListChecks },
  { label: 'Policies', page: 'policy', icon: ShieldCheck },
  { label: 'History', page: 'history', icon: FileClock },
  { label: 'Users', page: 'user', icon: Users, adminOnly: true },
  { label: 'Logs', icon: ScrollText, disabled: true }
];

export function Dashboard({ session, onLoggedOut, onUserUpdated }: DashboardProps) {
  const [collapsed, setCollapsed] = useState(false);
  const [activePage, setActivePage] = useState<PageKey>('dashboard');
  const [targetDialog, setTargetDialog] = useState<TargetDialogState | null>(null);
  const [agentDialog, setAgentDialog] = useState<AgentDialogState | null>(null);
  const [interactionDialog, setInteractionDialog] = useState<InteractionDialogState | null>(null);
  const [userDialog, setUserDialog] = useState<UserDialogState | null>(null);
  const [settingsOpen, setSettingsOpen] = useState(false);
  const [deleteCandidate, setDeleteCandidate] = useState<Target | null>(null);
  const [deleteAgentCandidate, setDeleteAgentCandidate] = useState<Agent | null>(null);
  const [deleteInteractionCandidate, setDeleteInteractionCandidate] = useState<InteractionInstance | null>(null);
  const [deleteUserCandidate, setDeleteUserCandidate] = useState<UserProfile | null>(null);
  const [regenerateUserCandidate, setRegenerateUserCandidate] = useState<UserProfile | null>(null);
  const [resetUserCandidate, setResetUserCandidate] = useState<UserProfile | null>(null);
  const queryClient = useQueryClient();

  const usersQuery = useQuery({ queryKey: ['users'], queryFn: getUsers, enabled: session.user.admin });
  const agentsQuery = useQuery({ queryKey: ['agents'], queryFn: getAgents });
  const targetsQuery = useQuery({ queryKey: ['targets'], queryFn: getTargets });
  const interactionsQuery = useQuery({ queryKey: ['interactions'], queryFn: getInteractions });
  const policiesQuery = useQuery({ queryKey: ['policies'], queryFn: getPolicies });
  const historyQuery = useQuery({ queryKey: ['history'], queryFn: () => getHistory({ limit: 100 }) });
  const activationPolicyQuery = useQuery({ queryKey: ['activation-policy'], queryFn: () => getActivationPolicy(session.csrf_token), enabled: session.user.admin });

  const createTargetMutation = useMutation({
    mutationFn: (input: TargetMutationInput) => createTarget(input, session.csrf_token),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ['targets'] })
  });
  const updateTargetMutation = useMutation({
    mutationFn: ({ id, input }: { id: string; input: TargetMutationInput }) => updateTarget(id, input, session.csrf_token),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ['targets'] })
  });
  const testTargetCredentialsMutation = useMutation({
    mutationFn: (input: TargetCredentialTestInput) => testTargetCredentials(input, session.csrf_token)
  });
  const deleteTargetMutation = useMutation({
    mutationFn: (id: string) => deleteTarget(id, session.csrf_token),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ['targets'] })
  });
  const createAgentMutation = useMutation({
    mutationFn: (input: AgentMutationInput) => createAgent(input, session.csrf_token),
    onSuccess: async () => {
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: ['agents'] }),
        queryClient.invalidateQueries({ queryKey: ['interactions'] })
      ]);
    }
  });
  const updateAgentMutation = useMutation({
    mutationFn: ({ id, input }: { id: string; input: AgentMutationInput }) => updateAgent(id, input, session.csrf_token),
    onSuccess: async () => {
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: ['agents'] }),
        queryClient.invalidateQueries({ queryKey: ['interactions'] })
      ]);
    }
  });
  const deleteAgentMutation = useMutation({
    mutationFn: (id: string) => deleteAgent(id, session.csrf_token),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ['agents'] })
  });
  const createInteractionMutation = useMutation({
    mutationFn: (input: InteractionMutationInput) => createInteraction(input, session.csrf_token),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ['interactions'] })
  });
  const updateInteractionMutation = useMutation({
    mutationFn: ({ id, input }: { id: string; input: InteractionMutationInput }) => updateInteraction(id, input, session.csrf_token),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ['interactions'] })
  });
  const deleteInteractionMutation = useMutation({
    mutationFn: (id: string) => deleteInteraction(id, session.csrf_token),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ['interactions'] })
  });
  const createUserMutation = useMutation({
    mutationFn: (input: UserMutationInput) => createUser(input, session.csrf_token),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ['users'] })
  });
  const updateUserMutation = useMutation({
    mutationFn: ({ id, input }: { id: string; input: UserMutationInput }) => updateUser(id, input, session.csrf_token),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ['users'] })
  });
  const deleteUserMutation = useMutation({
    mutationFn: (id: string) => deleteUser(id, session.csrf_token),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ['users'] })
  });
  const regenerateActivationMutation = useMutation({
    mutationFn: ({ id, seconds }: { id: string; seconds?: number }) => regenerateActivationLink(id, seconds, session.csrf_token)
  });
  const passwordResetMutation = useMutation({
    mutationFn: ({ id, seconds }: { id: string; seconds?: number }) => createPasswordResetLink(id, seconds, session.csrf_token),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ['users'] })
  });
  const changePasswordMutation = useMutation({
    mutationFn: (input: ChangePasswordInput) => changePassword(input, session.csrf_token)
  });
  const updateSettingsMutation = useMutation({
    mutationFn: (theme: Theme) => updateUserSettings({ theme }, session.csrf_token)
  });

  const managedUsers = usersQuery.data ?? [];
  const agents = agentsQuery.data ?? [];
  const targets = targetsQuery.data ?? [];
  const interactions = interactionsQuery.data ?? [];
  const policies = policiesQuery.data ?? [];
  const commands = historyQuery.data?.items ?? [];
  const targetCreationPrerequisites = getTargetCreationPrerequisites(
    agents,
    policies,
    agentsQuery.isLoading || policiesQuery.isLoading,
    Boolean(agentsQuery.error || policiesQuery.error)
  );
  const agentCreationPrerequisites = getAgentCreationPrerequisites(
    interactions,
    session.user.id,
    interactionsQuery.isLoading,
    Boolean(interactionsQuery.error)
  );

  async function handleLogout() {
    await logout(session.csrf_token);
    onLoggedOut();
  }

  async function handleChangePassword(input: ChangePasswordInput) {
    await changePasswordMutation.mutateAsync(input);
  }

  async function handleSaveTheme(theme: Theme) {
    const user = await updateSettingsMutation.mutateAsync(theme);
    onUserUpdated(user);
  }

  function openCreateAgent() {
    if (agentCreationPrerequisites.status === 'ready') {
      setAgentDialog({ mode: 'create' });
    }
  }

  function openCreateInteraction() {
    setInteractionDialog({ mode: 'choose' });
  }

  async function handleSaveTarget(input: TargetMutationInput) {
    if (targetDialog?.mode === 'edit') {
      await updateTargetMutation.mutateAsync({ id: targetDialog.target.id, input });
    } else {
      await createTargetMutation.mutateAsync(input);
    }
    closeTargetDialog();
  }

  async function handleSaveAgent(input: AgentMutationInput): Promise<Agent> {
    if (agentDialog?.mode === 'edit') {
      const updated = await updateAgentMutation.mutateAsync({ id: agentDialog.agent.id, input });
      closeAgentDialog();
      return updated;
    }
    return createAgentMutation.mutateAsync(input);
  }

  async function handleSaveInteraction(input: InteractionMutationInput) {
    if (interactionDialog?.mode === 'edit') {
      await updateInteractionMutation.mutateAsync({ id: interactionDialog.interaction.id, input });
    } else {
      await createInteractionMutation.mutateAsync(input);
    }
    closeInteractionDialog();
  }

  async function handleSaveUser(input: UserMutationInput) {
    if (userDialog?.mode === 'edit') {
      const updated = await updateUserMutation.mutateAsync({ id: userDialog.user.id, input });
      closeUserDialog();
      return updated;
    } else {
      return createUserMutation.mutateAsync(input);
    }
  }


  async function handleConfirmDelete() {
    if (!deleteCandidate) {
      return;
    }
    await deleteTargetMutation.mutateAsync(deleteCandidate.id);
    closeDeleteDialog();
  }

  async function handleConfirmDeleteAgent() {
    if (!deleteAgentCandidate) {
      return;
    }
    await deleteAgentMutation.mutateAsync(deleteAgentCandidate.id);
    closeDeleteAgentDialog();
  }

  async function handleConfirmDeleteInteraction() {
    if (!deleteInteractionCandidate) {
      return;
    }
    await deleteInteractionMutation.mutateAsync(deleteInteractionCandidate.id);
    closeDeleteInteractionDialog();
  }

  async function handleConfirmDeleteUser() {
    if (!deleteUserCandidate) {
      return;
    }
    await deleteUserMutation.mutateAsync(deleteUserCandidate.id);
    closeDeleteUserDialog();
  }


  const pageTitle =
    activePage === 'target'
      ? 'Targets'
      : activePage === 'agent'
        ? 'Agents'
        : activePage === 'interaction'
          ? 'Interactions'
          : activePage === 'user'
            ? 'Users'
            : activePage === 'policy'
              ? 'Policies'
              : activePage === 'history'
                ? 'History'
                : 'Dashboard';
  const isSavingTarget = createTargetMutation.isPending || updateTargetMutation.isPending;
  const isSavingAgent = createAgentMutation.isPending || updateAgentMutation.isPending;
  const isSavingInteraction = createInteractionMutation.isPending || updateInteractionMutation.isPending;
  const isSavingUser = createUserMutation.isPending || updateUserMutation.isPending;

  function closeTargetDialog() {
    createTargetMutation.reset();
    updateTargetMutation.reset();
    testTargetCredentialsMutation.reset();
    setTargetDialog(null);
  }

  function closeAgentDialog() {
    createAgentMutation.reset();
    updateAgentMutation.reset();
    setAgentDialog(null);
  }

  function closeInteractionDialog() {
    createInteractionMutation.reset();
    updateInteractionMutation.reset();
    setInteractionDialog(null);
  }

  function closeUserDialog() {
    createUserMutation.reset();
    updateUserMutation.reset();
    setUserDialog(null);
  }

  function closeSettingsDialog() {
    changePasswordMutation.reset();
    updateSettingsMutation.reset();
    applyTheme(session.user.theme);
    setSettingsOpen(false);
  }

  function closeDeleteDialog() {
    deleteTargetMutation.reset();
    setDeleteCandidate(null);
  }

  function closeDeleteAgentDialog() {
    deleteAgentMutation.reset();
    setDeleteAgentCandidate(null);
  }

  function closeDeleteInteractionDialog() {
    deleteInteractionMutation.reset();
    setDeleteInteractionCandidate(null);
  }

  function closeDeleteUserDialog() {
    deleteUserMutation.reset();
    setDeleteUserCandidate(null);
  }

  return (
    <div className="app-shell">
      <aside className={`sidebar ${collapsed ? 'collapsed' : ''}`} aria-label="Application navigation">
        <div className="sidebar-brand">
          <button className="icon-button" type="button" onClick={() => setCollapsed((value) => !value)} aria-label="Toggle navigation">
            <Menu size={22} />
          </button>
          <div className="brand-mark" aria-hidden="true" />
          {!collapsed ? <strong>Proxyma</strong> : null}
        </div>

        <nav className="nav-list">
          {navItems
            .filter((item) => !item.adminOnly || session.user.admin)
            .map((item) => (
            <NavButton key={item.label} activePage={activePage} collapsed={collapsed} item={item} onSelect={setActivePage} />
          ))}
        </nav>

        <button className="collapse-button" type="button" onClick={() => setCollapsed((value) => !value)}>
          <ChevronsLeft size={18} />
          {!collapsed ? <span>Collapse</span> : null}
        </button>
        {!collapsed ? <ApplicationFooter version={session.build.version} /> : null}
      </aside>

      <main className="workspace">
        <header className="topbar">
          <h1>{pageTitle}</h1>
          <div className="user-menu">
            <button
              aria-label={`Open settings for ${session.user.name || session.user.username}`}
              className="user-settings-trigger"
              type="button"
              onClick={() => setSettingsOpen(true)}
            >
              <span className="avatar">{initials(session.user.name || session.user.username)}</span>
              <span>
                <strong>{session.user.name || session.user.username}</strong>
                <small>{session.user.admin ? 'Administrator' : 'User'}</small>
              </span>
              <ChevronDown size={16} aria-hidden="true" />
            </button>
            <button className="ghost-button" type="button" onClick={handleLogout}>
              <LogOut size={16} />
              Logout
            </button>
          </div>
        </header>

        {activePage === 'target' ? (
          <TargetPage
            agentCreationPrerequisites={agentCreationPrerequisites}
            commands={commands}
            error={targetsQuery.error}
            isLoading={targetsQuery.isLoading}
            managedUsers={managedUsers}
            onAdd={() => setTargetDialog({ mode: 'create' })}
            onAddAgent={openCreateAgent}
            onAddInteraction={openCreateInteraction}
            onAddPolicy={() => setActivePage('policy')}
            onDelete={setDeleteCandidate}
            onEdit={(target) => setTargetDialog({ mode: 'edit', target })}
            onRefresh={() => void targetsQuery.refetch()}
            onRetryAgentPrerequisites={() => void interactionsQuery.refetch()}
            onRetryPrerequisites={() => void Promise.all([agentsQuery.refetch(), policiesQuery.refetch()])}
            showOwners={session.user.admin}
            targetCreationPrerequisites={targetCreationPrerequisites}
            targets={targets}
          />
        ) : activePage === 'agent' ? (
          <AgentPage
            agentCreationPrerequisites={agentCreationPrerequisites}
            agents={agents}
            commands={commands}
            error={agentsQuery.error}
            isLoading={agentsQuery.isLoading}
            managedUsers={managedUsers}
            onAdd={openCreateAgent}
            onAddInteraction={openCreateInteraction}
            onDelete={setDeleteAgentCandidate}
            onEdit={(agent) => setAgentDialog({ mode: 'edit', agent })}
            onRefresh={() => void agentsQuery.refetch()}
            onRetryPrerequisites={() => void interactionsQuery.refetch()}
            showOwners={session.user.admin}
            targets={targets}
          />
        ) : activePage === 'interaction' ? (
          <InteractionPage
            agents={agents}
            error={interactionsQuery.error}
            interactions={interactions}
            isLoading={interactionsQuery.isLoading}
            managedUsers={managedUsers}
            onAdd={openCreateInteraction}
            onDelete={setDeleteInteractionCandidate}
            onEdit={(interaction) => setInteractionDialog({ mode: 'edit', interaction })}
            onRefresh={() => void interactionsQuery.refetch()}
            showOwners={session.user.admin}
          />
        ) : activePage === 'user' ? (
          <UserPage
            currentUserID={session.user.id}
            error={usersQuery.error}
            isLoading={usersQuery.isLoading}
            onAdd={() => setUserDialog({ mode: 'create' })}
            onDelete={setDeleteUserCandidate}
            onEdit={(user) => setUserDialog({ mode: 'edit', user })}
            onRegenerate={async (user) => setRegenerateUserCandidate(user)}
            onReset={async (user) => setResetUserCandidate(user)}
            onRefresh={() => void usersQuery.refetch()}
            users={managedUsers}
          />
        ) : activePage === 'history' ? (
          <HistoryPage
            agents={agents}
            interactions={interactions}
            managedUsers={managedUsers}
            session={session}
          />
        ) : activePage === 'policy' ? (
          <PolicyManagementPage
            error={policiesQuery.error}
            isLoading={policiesQuery.isLoading}
            onRefresh={() => void policiesQuery.refetch()}
            onTargetsRefresh={() => void targetsQuery.refetch()}
            policies={policies}
            session={session}
            targets={targets}
          />
        ) : (
          <DashboardPage
            agentCreationPrerequisites={agentCreationPrerequisites}
            agents={agents}
            agentsError={agentsQuery.error}
            agentsLoading={agentsQuery.isLoading}
            commands={commands}
            historyError={historyQuery.error}
            historyLoading={historyQuery.isLoading}
            managedUsers={managedUsers}
            onRefreshHistory={() => void historyQuery.refetch()}
            onViewCommandHistory={() => setActivePage('history')}
            onAddAgent={openCreateAgent}
            onAddInteraction={openCreateInteraction}
            onAddPolicy={() => setActivePage('policy')}
            onAddTarget={() => setTargetDialog({ mode: 'create' })}
            onDeleteAgent={setDeleteAgentCandidate}
            onDeleteTarget={setDeleteCandidate}
            onEditAgent={(agent) => setAgentDialog({ mode: 'edit', agent })}
            onEditTarget={(target) => setTargetDialog({ mode: 'edit', target })}
            onRefreshAgents={() => void agentsQuery.refetch()}
            onRefreshTargets={() => void targetsQuery.refetch()}
            onRetryAgentPrerequisites={() => void interactionsQuery.refetch()}
            onRetryTargetPrerequisites={() => void Promise.all([agentsQuery.refetch(), policiesQuery.refetch()])}
            showOwners={session.user.admin}
            targetCreationPrerequisites={targetCreationPrerequisites}
            targets={targets}
            targetsError={targetsQuery.error}
            targetsLoading={targetsQuery.isLoading}
          />
        )}
      </main>

      {settingsOpen ? (
        <SettingsModal
          error={changePasswordMutation.error}
          appearanceError={updateSettingsMutation.error}
          isSaving={changePasswordMutation.isPending}
          isSavingAppearance={updateSettingsMutation.isPending}
          onClose={closeSettingsDialog}
          onPreviewTheme={applyTheme}
          onSaveTheme={handleSaveTheme}
          onSubmit={handleChangePassword}
          csrfToken={session.csrf_token}
          theme={session.user.theme}
          user={session.user}
        />
      ) : null}

      {targetDialog ? (
        <TargetFormModal
          agents={agents}
          error={createTargetMutation.error || updateTargetMutation.error}
          isSaving={isSavingTarget}
          isTestingCredentials={testTargetCredentialsMutation.isPending}
          mode={targetDialog.mode}
          onClose={closeTargetDialog}
          onSubmit={handleSaveTarget}
          onTestCredentials={(input) => testTargetCredentialsMutation.mutateAsync(input)}
          policies={policies}
          target={targetDialog.target}
        />
      ) : null}

      {agentDialog ? (
        <AgentFormModal
          agent={agentDialog.agent}
          currentUserID={session.user.id}
          error={createAgentMutation.error || updateAgentMutation.error}
          interactionError={interactionsQuery.error}
          interactions={interactions}
          interactionsLoading={interactionsQuery.isLoading}
          isSaving={isSavingAgent}
          mode={agentDialog.mode}
          onClose={closeAgentDialog}
          onSubmit={handleSaveAgent}
        />
      ) : null}

      {interactionDialog?.mode === 'choose' ? (
        <InteractionTypeModal onClose={closeInteractionDialog} onSelectNtfy={() => setInteractionDialog({ mode: 'create' })} />
      ) : null}

      {interactionDialog?.mode === 'create' || interactionDialog?.mode === 'edit' ? (
        <InteractionFormModal
          error={createInteractionMutation.error || updateInteractionMutation.error}
          interaction={interactionDialog.interaction}
          isSaving={isSavingInteraction}
          managedUsers={managedUsers}
          mode={interactionDialog.mode}
          onClose={closeInteractionDialog}
          onSubmit={handleSaveInteraction}
          showOwnerSelector={session.user.admin}
        />
      ) : null}

      {userDialog ? (
        <UserFormModal
          activationPolicy={activationPolicyQuery.data}
          error={createUserMutation.error || updateUserMutation.error}
          isSaving={isSavingUser}
          mode={userDialog.mode}
          onClose={closeUserDialog}
          onSubmit={handleSaveUser}
          user={userDialog.user}
        />
      ) : null}

      {regenerateUserCandidate ? <RegenerateActivationModal
        activationPolicy={activationPolicyQuery.data}
        error={regenerateActivationMutation.error}
        isSaving={regenerateActivationMutation.isPending}
        onClose={() => { regenerateActivationMutation.reset(); setRegenerateUserCandidate(null); }}
        onSubmit={(seconds) => regenerateActivationMutation.mutateAsync({ id: regenerateUserCandidate.id, seconds })}
        purpose="activation"
        user={regenerateUserCandidate}
      /> : null}

      {resetUserCandidate ? <RegenerateActivationModal
        activationPolicy={activationPolicyQuery.data}
        error={passwordResetMutation.error}
        isSaving={passwordResetMutation.isPending}
        onClose={() => { passwordResetMutation.reset(); setResetUserCandidate(null); }}
        onSubmit={(seconds) => passwordResetMutation.mutateAsync({ id: resetUserCandidate.id, seconds })}
        purpose="password_reset"
        user={resetUserCandidate}
      /> : null}

      {deleteCandidate ? (
        <DeleteTargetModal
          error={deleteTargetMutation.error}
          isDeleting={deleteTargetMutation.isPending}
          onClose={closeDeleteDialog}
          onConfirm={handleConfirmDelete}
          target={deleteCandidate}
        />
      ) : null}

      {deleteUserCandidate ? (
        <DeleteUserModal
          error={deleteUserMutation.error}
          isDeleting={deleteUserMutation.isPending}
          onClose={closeDeleteUserDialog}
          onConfirm={handleConfirmDeleteUser}
          user={deleteUserCandidate}
        />
      ) : null}

      {deleteAgentCandidate ? (
        <DeleteAgentModal
          agent={deleteAgentCandidate}
          error={deleteAgentMutation.error}
          isDeleting={deleteAgentMutation.isPending}
          onClose={closeDeleteAgentDialog}
          onConfirm={handleConfirmDeleteAgent}
        />
      ) : null}

      {deleteInteractionCandidate ? (
        <DeleteInteractionModal
          error={deleteInteractionMutation.error}
          interaction={deleteInteractionCandidate}
          isDeleting={deleteInteractionMutation.isPending}
          onClose={closeDeleteInteractionDialog}
          onConfirm={handleConfirmDeleteInteraction}
        />
      ) : null}

    </div>
  );
}

function NavButton({
  activePage,
  collapsed,
  item,
  onSelect
}: {
  activePage: PageKey;
  collapsed: boolean;
  item: NavItem;
  onSelect: (page: PageKey) => void;
}) {
  const Icon = item.icon;
  const active = item.page === activePage;
  return (
    <button
      aria-disabled={item.disabled ? 'true' : undefined}
      className={`nav-button ${active ? 'active' : ''}`}
      disabled={item.disabled}
      onClick={() => {
        if (item.page) {
          onSelect(item.page);
        }
      }}
      title={collapsed ? item.label : undefined}
      type="button"
    >
      <Icon size={20} />
      {!collapsed ? <span>{item.label}</span> : null}
    </button>
  );
}

function DashboardPage({
  agentCreationPrerequisites,
  agents,
  agentsError,
  agentsLoading,
  commands,
  historyError,
  historyLoading,
  managedUsers,
  onRefreshHistory,
  onViewCommandHistory,
  onAddAgent,
  onAddInteraction,
  onAddPolicy,
  onAddTarget,
  onDeleteAgent,
  onDeleteTarget,
  onEditAgent,
  onEditTarget,
  onRefreshAgents,
  onRefreshTargets,
  onRetryAgentPrerequisites,
  onRetryTargetPrerequisites,
  showOwners,
  targetCreationPrerequisites,
  targets,
  targetsError,
  targetsLoading
}: {
  agentCreationPrerequisites: AgentCreationPrerequisites;
  agents: Agent[];
  agentsError: Error | null;
  agentsLoading: boolean;
  commands: CommandHistoryItem[];
  historyError: Error | null;
  historyLoading: boolean;
  managedUsers: UserProfile[];
  onRefreshHistory: () => void;
  onViewCommandHistory: () => void;
  onAddAgent: () => void;
  onAddInteraction: () => void;
  onAddPolicy: () => void;
  onAddTarget: () => void;
  onDeleteAgent: (agent: Agent) => void;
  onDeleteTarget: (target: Target) => void;
  onEditAgent: (agent: Agent) => void;
  onEditTarget: (target: Target) => void;
  onRefreshAgents: () => void;
  onRefreshTargets: () => void;
  onRetryAgentPrerequisites: () => void;
  onRetryTargetPrerequisites: () => void;
  showOwners: boolean;
  targetCreationPrerequisites: TargetCreationPrerequisites;
  targets: Target[];
  targetsError: Error | null;
  targetsLoading: boolean;
}) {
  return (
    <section className="dashboard-stack">
      <CommandHistorySection
        commands={commands}
        error={historyError}
        isLoading={historyLoading}
        onRefresh={onRefreshHistory}
        onViewAll={onViewCommandHistory}
      />
      <TargetsSection
        agentCreationPrerequisites={agentCreationPrerequisites}
        commands={commands}
        error={targetsError}
        isLoading={targetsLoading}
        onAdd={onAddTarget}
        onAddAgent={onAddAgent}
        onAddInteraction={onAddInteraction}
        onAddPolicy={onAddPolicy}
        onDelete={onDeleteTarget}
        onEdit={onEditTarget}
        onRefresh={onRefreshTargets}
        onRetryAgentPrerequisites={onRetryAgentPrerequisites}
        onRetryPrerequisites={onRetryTargetPrerequisites}
        managedUsers={managedUsers}
        showOwners={showOwners}
        targetCreationPrerequisites={targetCreationPrerequisites}
        targets={targets}
        title={showOwners ? 'Targets' : 'Owned Targets'}
      />
      <AgentsSection
        agentCreationPrerequisites={agentCreationPrerequisites}
        agents={agents}
        commands={commands}
        error={agentsError}
        isLoading={agentsLoading}
        onAdd={onAddAgent}
        onAddInteraction={onAddInteraction}
        onDelete={onDeleteAgent}
        onEdit={onEditAgent}
        onRefresh={onRefreshAgents}
        onRetryPrerequisites={onRetryAgentPrerequisites}
        managedUsers={managedUsers}
        showOwners={showOwners}
        targets={targets}
      />
    </section>
  );
}

function UserPage({
  currentUserID,
  error,
  isLoading,
  onAdd,
  onDelete,
  onEdit,
  onRegenerate,
  onReset,
  onRefresh,
  users
}: {
  currentUserID: string;
  error: Error | null;
  isLoading: boolean;
  onAdd: () => void;
  onDelete: (user: UserProfile) => void;
  onEdit: (user: UserProfile) => void;
  onRegenerate: (user: UserProfile) => Promise<void>;
  onReset: (user: UserProfile) => Promise<void>;
  onRefresh: () => void;
  users: UserProfile[];
}) {
  return (
    <section className="dashboard-stack">
      <UsersSection currentUserID={currentUserID} error={error} isLoading={isLoading} onAdd={onAdd} onDelete={onDelete} onEdit={onEdit} onRegenerate={onRegenerate} onReset={onReset} onRefresh={onRefresh} users={users} />
    </section>
  );
}

function UsersSection({
  currentUserID,
  error,
  isLoading,
  onAdd,
  onDelete,
  onEdit,
  onRegenerate,
  onReset,
  onRefresh,
  users
}: {
  currentUserID: string;
  error: Error | null;
  isLoading: boolean;
  onAdd: () => void;
  onDelete: (user: UserProfile) => void;
  onEdit: (user: UserProfile) => void;
  onRegenerate: (user: UserProfile) => Promise<void>;
  onReset: (user: UserProfile) => Promise<void>;
  onRefresh: () => void;
  users: UserProfile[];
}) {
  return (
    <Panel
      action={<SectionActions label="Add User" onAdd={onAdd} onRefresh={onRefresh} />}
      count={users.length}
      error={error}
      icon={<Users size={18} />}
      isLoading={isLoading}
      title="Users"
    >
      {users.length === 0 ? (
        <EmptyState message="No users are configured." />
      ) : (
        <table>
          <thead>
            <tr>
              <th>Username</th>
              <th>Name</th>
              <th>Role</th>
              <th>Status</th>
              <th className="actions-cell">Actions</th>
            </tr>
          </thead>
          <tbody>
            {users.map((user) => (
              <tr key={user.id}>
                <td>{user.username}</td>
                <td>{user.name}</td>
                <td>
                  <RoleBadge admin={user.admin} />
                </td>
                <td><span className={`result-badge ${user.status === 'active' ? 'success' : 'waiting'}`}>{statusLabel(user.status)}</span></td>
                <td className="actions-cell">
                  <div className="row-actions">
                    {user.status === 'pending_activation' ? <button className="icon-button bordered" type="button" aria-label={`Regenerate activation link for ${userLabel(user)}`} onClick={() => void onRegenerate(user)}><RefreshCw size={15} /></button> : null}
                    {user.id !== currentUserID && (user.status === 'active' || user.status === 'password_reset_pending') ? <button className="icon-button bordered" type="button" aria-label={`${user.status === 'active' ? 'Reset password' : 'Regenerate password reset link'} for ${userLabel(user)}`} onClick={() => void onReset(user)}>{user.status === 'active' ? <KeyRound size={15} /> : <RefreshCw size={15} />}</button> : null}
                    <RowActions onDelete={() => onDelete(user)} onEdit={() => onEdit(user)} targetName={userLabel(user)} />
                  </div>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
    </Panel>
  );
}

function AgentPage({
  agentCreationPrerequisites,
  agents,
  commands,
  error,
  isLoading,
  managedUsers,
  onAdd,
  onAddInteraction,
  onDelete,
  onEdit,
  onRefresh,
  onRetryPrerequisites,
  showOwners,
  targets
}: {
  agentCreationPrerequisites: AgentCreationPrerequisites;
  agents: Agent[];
  commands: CommandHistoryItem[];
  error: Error | null;
  isLoading: boolean;
  managedUsers: UserProfile[];
  onAdd: () => void;
  onAddInteraction: () => void;
  onDelete: (agent: Agent) => void;
  onEdit: (agent: Agent) => void;
  onRefresh: () => void;
  onRetryPrerequisites: () => void;
  showOwners: boolean;
  targets: Target[];
}) {
  return (
    <section className="dashboard-stack">
      <AgentsSection
        agentCreationPrerequisites={agentCreationPrerequisites}
        agents={agents}
        commands={commands}
        error={error}
        isLoading={isLoading}
        onAdd={onAdd}
        onAddInteraction={onAddInteraction}
        onDelete={onDelete}
        onEdit={onEdit}
        onRefresh={onRefresh}
        onRetryPrerequisites={onRetryPrerequisites}
        managedUsers={managedUsers}
        showOwners={showOwners}
        targets={targets}
      />
    </section>
  );
}

function TargetPage({
  agentCreationPrerequisites,
  commands,
  error,
  isLoading,
  managedUsers,
  onAdd,
  onAddAgent,
  onAddInteraction,
  onAddPolicy,
  onDelete,
  onEdit,
  onRefresh,
  onRetryAgentPrerequisites,
  onRetryPrerequisites,
  showOwners,
  targetCreationPrerequisites,
  targets
}: {
  agentCreationPrerequisites: AgentCreationPrerequisites;
  commands: CommandHistoryItem[];
  error: Error | null;
  isLoading: boolean;
  managedUsers: UserProfile[];
  onAdd: () => void;
  onAddAgent: () => void;
  onAddInteraction: () => void;
  onAddPolicy: () => void;
  onDelete: (target: Target) => void;
  onEdit: (target: Target) => void;
  onRefresh: () => void;
  onRetryAgentPrerequisites: () => void;
  onRetryPrerequisites: () => void;
  showOwners: boolean;
  targetCreationPrerequisites: TargetCreationPrerequisites;
  targets: Target[];
}) {
  return (
    <section className="dashboard-stack">
      <TargetsSection
        agentCreationPrerequisites={agentCreationPrerequisites}
        commands={commands}
        error={error}
        isLoading={isLoading}
        onAdd={onAdd}
        onAddAgent={onAddAgent}
        onAddInteraction={onAddInteraction}
        onAddPolicy={onAddPolicy}
        onDelete={onDelete}
        onEdit={onEdit}
        onRefresh={onRefresh}
        onRetryAgentPrerequisites={onRetryAgentPrerequisites}
        onRetryPrerequisites={onRetryPrerequisites}
        managedUsers={managedUsers}
        showOwners={showOwners}
        targetCreationPrerequisites={targetCreationPrerequisites}
        targets={targets}
        title="Targets"
      />
    </section>
  );
}

function InteractionPage({
  agents,
  error,
  interactions,
  isLoading,
  managedUsers,
  onAdd,
  onDelete,
  onEdit,
  onRefresh,
  showOwners
}: {
  agents: Agent[];
  error: Error | null;
  interactions: InteractionInstance[];
  isLoading: boolean;
  managedUsers: UserProfile[];
  onAdd: () => void;
  onDelete: (interaction: InteractionInstance) => void;
  onEdit: (interaction: InteractionInstance) => void;
  onRefresh: () => void;
  showOwners: boolean;
}) {
  return (
    <section className="dashboard-stack">
      <InteractionsSection
        agents={agents}
        error={error}
        interactions={interactions}
        isLoading={isLoading}
        managedUsers={managedUsers}
        onAdd={onAdd}
        onDelete={onDelete}
        onEdit={onEdit}
        onRefresh={onRefresh}
        showOwners={showOwners}
      />
    </section>
  );
}

type HistoryFilters = {
  entry_type: '' | 'command' | 'interaction';
  q: string;
  agent_id: string;
  status: string;
  kind: string;
  interaction_instance_id: string;
  interaction_instance_creator_user_id: string;
  target: string;
  created_from: string;
  created_to: string;
  command_pattern: string;
  command_pattern_mode: 'literal' | 'glob' | 'regex';
};

type HistoryPanelWidths = {
  filters: number;
  details: number;
};

type HistoryResize = {
  panel: keyof HistoryPanelWidths;
  startX: number;
  startFilters: number;
  startDetails: number;
};

const emptyHistoryFilters: HistoryFilters = {
  entry_type: '',
  q: '',
  agent_id: '',
  status: '',
  kind: '',
  interaction_instance_id: '',
  interaction_instance_creator_user_id: '',
  target: '',
  created_from: '',
  created_to: '',
  command_pattern: '',
  command_pattern_mode: 'literal'
};

function HistoryPage({
  agents,
  interactions,
  managedUsers,
  session
}: {
  agents: Agent[];
  interactions: InteractionInstance[];
  managedUsers: UserProfile[];
  session: UserSession;
}) {
  const [draftFilters, setDraftFilters] = useState<HistoryFilters>(emptyHistoryFilters);
  const [appliedFilters, setAppliedFilters] = useState<HistoryFilters>(emptyHistoryFilters);
  const [cursorStack, setCursorStack] = useState<string[]>(['']);
  const [selectedID, setSelectedID] = useState('');
  const [panelWidths, setPanelWidths] = useState<HistoryPanelWidths>({ filters: 260, details: 360 });
  const resizeRef = useRef<HistoryResize | null>(null);
  const cursor = cursorStack[cursorStack.length - 1] || '';
  const query = useMemo(
    () => historyQueryFromFilters(appliedFilters, cursor),
    [appliedFilters, cursor]
  );
  const historyQuery = useQuery({
    queryKey: ['history', query],
    queryFn: () => getHistory(query)
  });
  const items = historyQuery.data?.items ?? [];
  const selected = items.find((item) => item.history_id === selectedID) ?? items[0] ?? null;

  useEffect(() => {
    const timer = window.setTimeout(() => {
      setAppliedFilters(draftFilters);
      setCursorStack(['']);
      setSelectedID('');
    }, 300);
    return () => window.clearTimeout(timer);
  }, [draftFilters]);

  useEffect(() => {
    function handlePointerMove(event: PointerEvent) {
      const resize = resizeRef.current;
      if (!resize) {
        return;
      }
      const delta = event.clientX - resize.startX;
      if (resize.panel === 'filters') {
        setPanelWidths((current) => ({
          ...current,
          filters: Math.min(420, Math.max(220, resize.startFilters + delta))
        }));
      } else {
        setPanelWidths((current) => ({
          ...current,
          details: Math.min(620, Math.max(280, resize.startDetails - delta))
        }));
      }
    }

    function stopResize() {
      if (!resizeRef.current) {
        return;
      }
      resizeRef.current = null;
      document.body.style.cursor = '';
      document.body.style.userSelect = '';
    }

    window.addEventListener('pointermove', handlePointerMove);
    window.addEventListener('pointerup', stopResize);
    window.addEventListener('pointercancel', stopResize);
    return () => {
      window.removeEventListener('pointermove', handlePointerMove);
      window.removeEventListener('pointerup', stopResize);
      window.removeEventListener('pointercancel', stopResize);
    };
  }, []);

  function startResize(panel: keyof HistoryPanelWidths, event: ReactPointerEvent<HTMLButtonElement>) {
    event.preventDefault();
    resizeRef.current = {
      panel,
      startX: event.clientX,
      startFilters: panelWidths.filters,
      startDetails: panelWidths.details
    };
    document.body.style.cursor = 'col-resize';
    document.body.style.userSelect = 'none';
  }

  function resizeWithKeyboard(panel: keyof HistoryPanelWidths, delta: number) {
    setPanelWidths((current) => ({
      ...current,
      [panel]: panel === 'filters'
        ? Math.min(420, Math.max(220, current.filters + delta))
        : Math.min(620, Math.max(280, current.details - delta))
    }));
  }

  function updateFilter<K extends keyof HistoryFilters>(key: K, value: HistoryFilters[K]) {
    setDraftFilters((current) => ({ ...current, [key]: value }));
  }

  function clearFilters() {
    setDraftFilters(emptyHistoryFilters);
    setAppliedFilters(emptyHistoryFilters);
    setCursorStack(['']);
    setSelectedID('');
  }

  function nextPage() {
    const nextCursor = historyQuery.data?.next_cursor;
    if (nextCursor) {
      setCursorStack((current) => [...current, nextCursor]);
      setSelectedID('');
    }
  }

  function previousPage() {
    setCursorStack((current) => (current.length > 1 ? current.slice(0, -1) : current));
    setSelectedID('');
  }

  return (
    <section
      className="interaction-history-shell"
      style={{
        gridTemplateColumns: `${panelWidths.filters}px 10px minmax(0, 1fr) 10px ${panelWidths.details}px`
      }}
    >
      <section className="interaction-history-filters panel">
        <div className="panel-header compact">
          <div className="panel-title">
            <Filter size={18} />
            <h2>Filters</h2>
          </div>
          <div className="history-filter-header-actions">
            <span className="history-filter-status" aria-live="polite">Updates automatically</span>
            <button className="link-button" type="button" onClick={clearFilters}>Clear</button>
          </div>
        </div>
        <div className="history-filter-fields">
          <label className="dialog-field">
            <span>Type</span>
            <select value={draftFilters.entry_type} onChange={(event) => updateFilter('entry_type', event.target.value as HistoryFilters['entry_type'])}>
              <option value="">All entries</option>
              <option value="command">Commands</option>
              <option value="interaction">Interactions</option>
            </select>
          </label>
          <label className="dialog-field">
            <span>Search</span>
            <input value={draftFilters.q} onChange={(event) => updateFilter('q', event.target.value)} placeholder="Command, subject, message, target..." />
          </label>
          <label className="dialog-field">
            <span>Agent</span>
            <select value={draftFilters.agent_id} onChange={(event) => updateFilter('agent_id', event.target.value)}>
              <option value="">All agents</option>
              {agents.map((agent) => (
                <option key={agent.id} value={agent.id}>
                  {agentLabel(agent)}
                </option>
              ))}
            </select>
          </label>
          {session.user.admin ? (
            <label className="dialog-field">
              <span>Creator</span>
              <select value={draftFilters.interaction_instance_creator_user_id} onChange={(event) => updateFilter('interaction_instance_creator_user_id', event.target.value)}>
                <option value="">All creators</option>
                {managedUsers.map((user) => (
                  <option key={user.id} value={user.id}>
                    {userLabel(user)}
                  </option>
                ))}
              </select>
            </label>
          ) : null}
          <label className="dialog-field">
            <span>Status</span>
            <select value={draftFilters.status} onChange={(event) => updateFilter('status', event.target.value)}>
              <option value="">All statuses</option>
              <option value="created">Created</option>
              <option value="accepted_by_user">Accepted by user</option>
              <option value="running">Running</option>
              <option value="completed">Completed</option>
              <option value="failed">Failed</option>
              <option value="timed_out">Timed out</option>
              <option value="interrupted">Interrupted</option>
              <option value="abandoned">Abandoned</option>
              <option value="blocked">Blocked</option>
              <option value="waiting_user_response">Waiting for response</option>
              <option value="pending">Pending</option>
              <option value="answered">Answered</option>
              <option value="expired">Expired</option>
              <option value="canceled">Canceled</option>
            </select>
          </label>
          <label className="dialog-field">
            <span>Kind</span>
            <select value={draftFilters.kind} onChange={(event) => updateFilter('kind', event.target.value)}>
              <option value="">All kinds</option>
              <option value="approval_request">Approval request</option>
              <option value="question">Question</option>
            </select>
          </label>
          <label className="dialog-field">
            <span>Provider instance</span>
            <select value={draftFilters.interaction_instance_id} onChange={(event) => updateFilter('interaction_instance_id', event.target.value)}>
              <option value="">All instances</option>
              {showLocalInteraction ? <option value="local">Local</option> : null}
              {interactions.map((interaction) => (
                <option key={interaction.id} value={interaction.id}>
                  {interactionLabel(interaction)}
                </option>
              ))}
            </select>
          </label>
          <label className="dialog-field">
            <span>Target</span>
            <input value={draftFilters.target} onChange={(event) => updateFilter('target', event.target.value)} placeholder="Hostname or IP" />
          </label>
          <label className="dialog-field">
            <span>Date from</span>
            <input type="datetime-local" value={draftFilters.created_from} onChange={(event) => updateFilter('created_from', event.target.value)} />
          </label>
          <label className="dialog-field">
            <span>Date to</span>
            <input type="datetime-local" value={draftFilters.created_to} onChange={(event) => updateFilter('created_to', event.target.value)} />
          </label>
          <label className="dialog-field">
            <span>Command pattern</span>
            <input value={draftFilters.command_pattern} onChange={(event) => updateFilter('command_pattern', event.target.value)} placeholder="show *, systemctl.*" />
          </label>
          <label className="dialog-field">
            <span>Pattern mode</span>
            <select value={draftFilters.command_pattern_mode} onChange={(event) => updateFilter('command_pattern_mode', event.target.value as HistoryFilters['command_pattern_mode'])}>
              <option value="literal">Literal</option>
              <option value="glob">Glob</option>
              <option value="regex">Regex</option>
            </select>
          </label>
        </div>
      </section>

      <HistoryResizeHandle
        label="Resize filters panel"
        value={panelWidths.filters}
        min={220}
        max={420}
        onPointerDown={(event) => startResize('filters', event)}
        onKeyDown={(event) => {
          if (event.key === 'ArrowLeft') {
            event.preventDefault();
            resizeWithKeyboard('filters', -16);
          } else if (event.key === 'ArrowRight') {
            event.preventDefault();
            resizeWithKeyboard('filters', 16);
          }
        }}
      />

      <section className="panel interaction-history-results">
        <div className="panel-header">
          <div className="panel-title">
            <History size={18} />
            <h2>History</h2>
            <span className="count-pill">{items.length}</span>
          </div>
          <div className="section-actions">
            <button className="icon-button bordered" type="button" aria-label="Refresh history" onClick={() => void historyQuery.refetch()}>
              <RefreshCw size={16} />
            </button>
          </div>
        </div>
        {historyQuery.isLoading ? (
          <LoadingRows />
        ) : historyQuery.error ? (
          <EmptyState message={historyQuery.error.message} tone="error" />
        ) : items.length === 0 ? (
          <EmptyState message="No history is visible for this account." />
        ) : (
          <>
            <div className="table-wrap history-table-wrap">
              <table className="interaction-history-table">
                <thead>
                  <tr>
                    <th>Time</th>
                    <th>Status</th>
                    <th>Agent</th>
                    <th>Kind</th>
                    <th>Command</th>
                    <th>Target</th>
                  </tr>
                </thead>
                <tbody>
                  {items.map((item) => (
                    <tr
                      key={item.history_id}
                      className={selected?.history_id === item.history_id ? 'selected-row' : ''}
                      onClick={() => setSelectedID(item.history_id)}
                    >
                      <td>{formatDate(item.created_at)}</td>
                      <td><InteractionStatusBadge status={item.status} /></td>
                      <td>{item.agent_name || item.agent_id}</td>
                      <td>{item.entry_type === 'command' ? 'Command' : statusLabel(item.kind || 'interaction')}</td>
                      <td><code>{item.command}</code></td>
                      <td>{item.target || 'Unassigned'}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
            <div className="history-pagination">
              <span>{cursorStack.length === 1 ? 'First page' : `Page ${cursorStack.length}`}</span>
              <button className="ghost-button" type="button" onClick={previousPage} disabled={cursorStack.length === 1}>Previous</button>
              <button className="ghost-button" type="button" onClick={nextPage} disabled={!historyQuery.data?.next_cursor}>Next</button>
            </div>
          </>
        )}
      </section>

      <HistoryResizeHandle
        label="Resize history details panel"
        value={panelWidths.details}
        min={280}
        max={620}
        onPointerDown={(event) => startResize('details', event)}
        onKeyDown={(event) => {
          if (event.key === 'ArrowLeft') {
            event.preventDefault();
            resizeWithKeyboard('details', -16);
          } else if (event.key === 'ArrowRight') {
            event.preventDefault();
            resizeWithKeyboard('details', 16);
          }
        }}
      />

      <HistoryDetail item={selected} />
    </section>
  );
}

function HistoryResizeHandle({
  label,
  value,
  min,
  max,
  onPointerDown,
  onKeyDown
}: {
  label: string;
  value: number;
  min: number;
  max: number;
  onPointerDown: (event: ReactPointerEvent<HTMLButtonElement>) => void;
  onKeyDown: (event: ReactKeyboardEvent<HTMLButtonElement>) => void;
}) {
  return (
    <button
      className="history-resize-handle"
      type="button"
      role="separator"
      aria-label={label}
      aria-orientation="vertical"
      aria-valuemin={min}
      aria-valuemax={max}
      aria-valuenow={value}
      onPointerDown={onPointerDown}
      onKeyDown={onKeyDown}
    >
      <span aria-hidden="true" />
    </button>
  );
}

function HistoryDetail({ item }: { item: HistoryItem | null }) {
  return (
    <section className="panel interaction-history-detail">
      <div className="panel-header">
        <div className="panel-title">
          <Eye size={18} />
          <h2>{item?.entry_type === 'command' ? 'Command Details' : 'Interaction Details'}</h2>
        </div>
      </div>
      {!item ? (
          <EmptyState message="Select a history entry to view details." />
      ) : (
        <div className="interaction-detail-body">
          <div className="detail-heading">
            <div>
              <h3>{item.entry_type === 'command' ? 'Command' : item.subject || 'Interaction'}</h3>
              <InteractionStatusBadge status={item.status} />
            </div>
          </div>
          <dl className="detail-list">
            <div><dt>Agent</dt><dd>{item.agent_name || item.agent_id}</dd></div>
            <div><dt>Target</dt><dd>{item.target || 'Unassigned'}</dd></div>
            <div><dt>Kind</dt><dd>{item.entry_type === 'command' ? 'Command' : statusLabel(item.kind || 'interaction')}</dd></div>
            {item.entry_type === 'interaction' ? <div><dt>Provider</dt><dd>{item.provider || item.interaction_instance_id}</dd></div> : null}
          </dl>
          <div className="detail-section">
            <h4>Command</h4>
            <pre>{item.command}</pre>
          </div>
          {item.entry_type === 'interaction' ? <div className="detail-section">
            <h4>Message</h4>
            <p>{item.message}</p>
          </div> : null}
          {item.policy_justification ? (
            <div className="detail-section">
              <h4>LLM justification</h4>
              <p>{item.policy_justification}</p>
            </div>
          ) : null}
          <dl className="detail-list">
            {item.entry_type === 'interaction' ? <>
              <div><dt>Approved</dt><dd>{item.approved === undefined ? 'N/A' : item.approved ? 'Yes' : 'No'}</dd></div>
              <div><dt>Responded by</dt><dd>{item.responded_by || 'No response'}</dd></div>
            </> : <div><dt>Result</dt><dd>{resultLabel(item.result || item.status)}</dd></div>}
            <div><dt>Created</dt><dd>{formatDate(item.created_at)}</dd></div>
            {item.entry_type === 'interaction' ? <div><dt>Responded</dt><dd>{item.responded_at ? formatDate(item.responded_at) : 'Not responded'}</dd></div> : <div><dt>Completed</dt><dd>{item.completed_at ? formatDate(item.completed_at) : 'Not completed'}</dd></div>}
            <div><dt>Task ID</dt><dd><code>{item.task_id}</code></dd></div>
            {item.interaction_id ? <div><dt>Interaction ID</dt><dd><code>{item.interaction_id}</code></dd></div> : null}
          </dl>
          {item.entry_type === 'interaction' && (item.answer || item.cancel_reason) ? (
            <div className="detail-section">
              <h4>Response</h4>
              <p>{item.answer || item.cancel_reason}</p>
            </div>
          ) : null}
        </div>
      )}
    </section>
  );
}

function InteractionStatusBadge({ status }: { status: string }) {
  return <span className={`result-badge ${interactionStatusTone(status)}`}>{statusLabel(status)}</span>;
}

function historyQueryFromFilters(filters: HistoryFilters, cursor: string): HistoryQuery {
  return {
    limit: 50,
    cursor,
    entry_type: filters.entry_type || undefined,
    agent_id: filters.agent_id || undefined,
    interaction_instance_id: filters.interaction_instance_id || undefined,
    interaction_instance_creator_user_id: filters.interaction_instance_creator_user_id || undefined,
    status: filters.status || undefined,
    kind: filters.kind || undefined,
    target: filters.target || undefined,
    created_from: datetimeLocalToRFC3339(filters.created_from),
    created_to: datetimeLocalToRFC3339(filters.created_to),
    q: filters.q || undefined,
    command_pattern: filters.command_pattern || undefined,
    command_pattern_mode: filters.command_pattern ? filters.command_pattern_mode : undefined
  };
}

function datetimeLocalToRFC3339(value: string) {
  if (!value) {
    return undefined;
  }
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) {
    return undefined;
  }
  return date.toISOString();
}

function interactionStatusTone(status: string) {
  switch (status) {
    case 'answered':
      return 'success';
    case 'expired':
      return 'warning';
    case 'canceled':
      return 'failed';
    case 'pending':
      return 'waiting';
    default:
      return 'waiting';
  }
}

function CommandHistorySection({
  commands,
  isLoading,
  error,
  onRefresh,
  onViewAll
}: {
  commands: HistoryItem[];
  isLoading: boolean;
  error: Error | null;
  onRefresh: () => void;
  onViewAll: () => void;
}) {
  return (
    <Panel
      action={
        <div className="section-actions">
          <button className="link-button" type="button" onClick={onViewAll}>
            View all history
            <FileClock size={16} />
          </button>
          <button className="icon-button bordered" type="button" aria-label="Refresh" onClick={onRefresh}>
            <RefreshCw size={16} />
          </button>
        </div>
      }
      count={commands.length}
      error={error}
      icon={<History size={18} />}
      isLoading={isLoading}
      title="Recent History"
    >
      {commands.length === 0 ? (
        <EmptyState message="No recent history is visible for this account." />
      ) : (
        <HistoryPreviewTable entries={commands.slice(0, 5)} />
      )}
    </Panel>
  );
}

function HistoryPreviewTable({ entries }: { entries: HistoryItem[] }) {
  return (
    <table>
      <thead>
        <tr>
          <th>Type</th>
          <th>Agent</th>
          <th>Target</th>
          <th>Command</th>
          <th>Status</th>
          <th>Timestamp</th>
        </tr>
      </thead>
      <tbody>
        {entries.map((entry) => (
          <tr key={entry.history_id}>
            <td>{entry.entry_type === 'command' ? 'Command' : 'Interaction'}</td>
            <td>{entry.agent_name || entry.agent_id || 'Unknown agent'}</td>
            <td>{entry.target || 'Unassigned'}</td>
            <td><code>{entry.command || entry.subject || entry.message || 'Interaction'}</code></td>
            <td><InteractionStatusBadge status={entry.status} /></td>
            <td>{formatDate(entry.completed_at || entry.created_at)}</td>
          </tr>
        ))}
      </tbody>
    </table>
  );
}

function TargetsSection({
  agentCreationPrerequisites,
  commands,
  error,
  isLoading,
  managedUsers,
  onAdd,
  onAddAgent,
  onAddInteraction,
  onAddPolicy,
  onDelete,
  onEdit,
  onRefresh,
  onRetryAgentPrerequisites,
  onRetryPrerequisites,
  showOwners,
  targetCreationPrerequisites,
  targets,
  title
}: {
  agentCreationPrerequisites: AgentCreationPrerequisites;
  commands: CommandHistoryItem[];
  error: Error | null;
  isLoading: boolean;
  managedUsers: UserProfile[];
  onAdd: () => void;
  onAddAgent: () => void;
  onAddInteraction: () => void;
  onAddPolicy: () => void;
  onDelete: (target: Target) => void;
  onEdit: (target: Target) => void;
  onRefresh: () => void;
  onRetryAgentPrerequisites: () => void;
  onRetryPrerequisites: () => void;
  showOwners: boolean;
  targetCreationPrerequisites: TargetCreationPrerequisites;
  targets: Target[];
  title: string;
}) {
  return (
    <Panel
      action={(
        <TargetSectionActions
          agentCreationPrerequisites={agentCreationPrerequisites}
          onAdd={onAdd}
          onAddAgent={onAddAgent}
          onAddInteraction={onAddInteraction}
          onAddPolicy={onAddPolicy}
          onRefresh={onRefresh}
          onRetryAgentPrerequisites={onRetryAgentPrerequisites}
          onRetryPrerequisites={onRetryPrerequisites}
          prerequisites={targetCreationPrerequisites}
        />
      )}
      count={targets.length}
      error={error}
      icon={<Network size={18} />}
      isLoading={isLoading}
      title={title}
    >
      {targets.length === 0 ? (
        <EmptyState message="No targets are assigned to this account." />
      ) : (
        <table>
          <thead>
            <tr>
              <th>Target Name</th>
              <th>Type</th>
              <th>{showOwners ? 'Owners' : 'Owner'}</th>
              <th>Endpoint</th>
              <th>Last Seen</th>
              <th className="actions-cell">Actions</th>
            </tr>
          </thead>
          <tbody>
            {targets.map((target) => (
              <tr key={target.id}>
                <td>{targetLabel(target)}</td>
                <td>{targetType(target)}</td>
                <td>{showOwners ? ownerLabels(target.user_ids, managedUsers) : ownerCountLabel(target.user_ids)}</td>
                <td>{target.hostname || target.ip || 'No endpoint'}</td>
                <td>{lastSeenForTarget(target, commands)}</td>
                <td className="actions-cell">
                  <RowActions onDelete={() => onDelete(target)} onEdit={() => onEdit(target)} targetName={targetLabel(target)} />
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
    </Panel>
  );
}

function AgentsSection({
  agentCreationPrerequisites,
  agents,
  targets,
  commands,
  managedUsers,
  onAdd,
  onAddInteraction,
  onDelete,
  onEdit,
  onRefresh,
  onRetryPrerequisites,
  showOwners,
  isLoading,
  error
}: {
  agentCreationPrerequisites: AgentCreationPrerequisites;
  agents: Agent[];
  targets: Target[];
  commands: CommandHistoryItem[];
  managedUsers: UserProfile[];
  onAdd: () => void;
  onAddInteraction: () => void;
  onDelete: (agent: Agent) => void;
  onEdit: (agent: Agent) => void;
  onRefresh: () => void;
  onRetryPrerequisites: () => void;
  showOwners: boolean;
  isLoading: boolean;
  error: Error | null;
}) {
  const latestByAgent = useMemo(() => latestCommandByAgent(commands), [commands]);

  return (
    <Panel
      action={(
        <AgentSectionActions
          onAdd={onAdd}
          onAddInteraction={onAddInteraction}
          onRefresh={onRefresh}
          onRetryPrerequisites={onRetryPrerequisites}
          prerequisites={agentCreationPrerequisites}
        />
      )}
      count={agents.length}
      error={error}
      icon={<Bot size={18} />}
      isLoading={isLoading}
      title="Agents"
    >
      {agents.length === 0 ? (
        <EmptyState message="No agents are assigned to this account." />
      ) : (
        <table>
          <thead>
            <tr>
              <th>Agent Name</th>
              {showOwners ? <th>Owners</th> : null}
              <th>Linked Target</th>
              <th>Latest Command</th>
              <th>Command Result</th>
              <th>Last Run</th>
              <th className="actions-cell">Actions</th>
            </tr>
          </thead>
          <tbody>
            {agents.map((agent) => {
              const latest = latestByAgent.get(agent.id);
              return (
                <tr key={agent.id}>
                  <td>
                    <span className={agent.enabled ? 'state-dot online' : 'state-dot'} />
                    {agent.display_name || agent.id}
                  </td>
                  {showOwners ? <td>{ownerLabels(agent.user_ids, managedUsers)}</td> : null}
                  <td>{linkedTarget(agent, targets)}</td>
                  <td>{latest ? <code>{latest.command}</code> : 'No commands yet'}</td>
                  <td>{latest ? <ResultBadge result={latest.result || latest.status} /> : <span className="muted">None</span>}</td>
                  <td>{latest ? formatDate(latest.completed_at || latest.created_at) : 'Never'}</td>
                  <td className="actions-cell">
                    <RowActions onDelete={() => onDelete(agent)} onEdit={() => onEdit(agent)} targetName={agentLabel(agent)} />
                  </td>
                </tr>
              );
            })}
          </tbody>
        </table>
      )}
    </Panel>
  );
}

function InteractionsSection({
  agents,
  error,
  interactions,
  isLoading,
  managedUsers,
  onAdd,
  onDelete,
  onEdit,
  onRefresh,
  showOwners
}: {
  agents: Agent[];
  error: Error | null;
  interactions: InteractionInstance[];
  isLoading: boolean;
  managedUsers: UserProfile[];
  onAdd: () => void;
  onDelete: (interaction: InteractionInstance) => void;
  onEdit: (interaction: InteractionInstance) => void;
  onRefresh: () => void;
  showOwners: boolean;
}) {
  return (
    <Panel
      action={<SectionActions label="Add Interaction" onAdd={onAdd} onRefresh={onRefresh} />}
      count={interactions.length}
      error={error}
      icon={<ListChecks size={18} />}
      isLoading={isLoading}
      title="Interactions"
    >
      {interactions.length === 0 ? (
        <EmptyState message="No interaction instances are assigned to this account." />
      ) : (
        <table>
          <thead>
            <tr>
              <th>Name</th>
              {showOwners ? <th>Creator</th> : null}
              <th>Type</th>
              <th>Status</th>
              <th>Endpoint</th>
              <th>Agents</th>
              <th className="actions-cell">Actions</th>
            </tr>
          </thead>
          <tbody>
            {interactions.map((item) => (
              <tr key={item.id}>
                <td>{interactionLabel(item)}</td>
                {showOwners ? <td>{item.creator_label || ownerLabels([item.creator_user_id], managedUsers)}</td> : null}
                <td>{item.type.toUpperCase()}</td>
                <td>
                  <span className={item.enabled ? 'state-dot online' : 'state-dot'} />
                  {item.enabled ? 'Enabled' : 'Disabled'}
                </td>
                <td>{interactionEndpoint(item)}</td>
                <td>{interactionAgentLabels(item, agents)}</td>
                <td className="actions-cell">
                  <RowActions onDelete={() => onDelete(item)} onEdit={() => onEdit(item)} targetName={interactionLabel(item)} />
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
    </Panel>
  );
}

function Panel({
  action,
  children,
  count,
  error,
  icon,
  isLoading,
  title
}: {
  action?: ReactNode;
  children: ReactNode;
  count: number;
  error: Error | null;
  icon: ReactNode;
  isLoading: boolean;
  title: string;
}) {
  return (
    <section className="panel">
      <div className="panel-header">
        <div className="panel-title">
          {icon}
          <h2>{title}</h2>
          <span className="count-pill">{count}</span>
        </div>
        {action}
      </div>
      {isLoading ? <LoadingRows /> : error ? <EmptyState message={error.message} tone="error" /> : <div className="table-wrap">{children}</div>}
    </section>
  );
}

function SectionActions({ label, onAdd, onRefresh }: { label: string; onAdd?: () => void; onRefresh?: () => void }) {
  return (
    <div className="section-actions">
      <button className="primary-action" type="button" onClick={onAdd}>
        <Plus size={16} />
        {label}
      </button>
      <button className="icon-button bordered" type="button" aria-label="Refresh" onClick={onRefresh}>
        <RefreshCw size={16} />
      </button>
      <button className="icon-button bordered" type="button" aria-label="More actions">
        <MoreHorizontal size={16} />
      </button>
    </div>
  );
}

function AgentSectionActions({
  onAdd,
  onAddInteraction,
  onRefresh,
  onRetryPrerequisites,
  prerequisites
}: {
  onAdd: () => void;
  onAddInteraction: () => void;
  onRefresh: () => void;
  onRetryPrerequisites: () => void;
  prerequisites: AgentCreationPrerequisites;
}) {
  const canAddAgent = prerequisites.status === 'ready';

  return (
    <div className="prerequisite-section-actions">
      <div className="section-actions">
        <button
          aria-label={canAddAgent ? 'Add Agent' : `Add Agent unavailable: ${prerequisites.message}`}
          className="primary-action"
          disabled={!canAddAgent}
          type="button"
          onClick={onAdd}
        >
          <Plus size={16} />
          Add Agent
        </button>
        <button className="icon-button bordered" type="button" aria-label="Refresh" onClick={onRefresh}>
          <RefreshCw size={16} />
        </button>
        <button className="icon-button bordered" type="button" aria-label="More actions">
          <MoreHorizontal size={16} />
        </button>
      </div>

      {!canAddAgent ? (
        <div className="prerequisite-guidance" role="status">
          <span>{prerequisites.message}</span>
          <div className="prerequisite-shortcuts">
            {prerequisites.status === 'error' ? (
              <button className="link-button" type="button" onClick={onRetryPrerequisites}>
                Retry
              </button>
            ) : null}
            {prerequisites.status === 'missing' ? (
              <button className="link-button" type="button" onClick={onAddInteraction}>
                Add Interaction
              </button>
            ) : null}
          </div>
        </div>
      ) : null}
    </div>
  );
}

function TargetSectionActions({
  agentCreationPrerequisites,
  onAdd,
  onAddAgent,
  onAddInteraction,
  onAddPolicy,
  onRefresh,
  onRetryAgentPrerequisites,
  onRetryPrerequisites,
  prerequisites
}: {
  agentCreationPrerequisites: AgentCreationPrerequisites;
  onAdd: () => void;
  onAddAgent: () => void;
  onAddInteraction: () => void;
  onAddPolicy: () => void;
  onRefresh: () => void;
  onRetryAgentPrerequisites: () => void;
  onRetryPrerequisites: () => void;
  prerequisites: TargetCreationPrerequisites;
}) {
  const canAddTarget = prerequisites.status === 'ready';
  const canAddAgent = agentCreationPrerequisites.status === 'ready';

  return (
    <div className="prerequisite-section-actions">
      <div className="section-actions">
        <button
          aria-label={canAddTarget ? 'Add Target' : `Add Target unavailable: ${prerequisites.message}`}
          className="primary-action"
          disabled={!canAddTarget}
          type="button"
          onClick={onAdd}
        >
          <Plus size={16} />
          Add Target
        </button>
        <button className="icon-button bordered" type="button" aria-label="Refresh" onClick={onRefresh}>
          <RefreshCw size={16} />
        </button>
        <button className="icon-button bordered" type="button" aria-label="More actions">
          <MoreHorizontal size={16} />
        </button>
      </div>

      {!canAddTarget ? (
        <div className="prerequisite-guidance" role="status">
          <span>{prerequisites.message}</span>
          <div className="prerequisite-shortcuts">
            {prerequisites.status === 'error' ? (
              <button className="link-button" type="button" onClick={onRetryPrerequisites}>
                Retry
              </button>
            ) : null}
            {prerequisites.status === 'missing' && prerequisites.missingAgent ? (
              <button
                aria-label={canAddAgent ? 'Add Agent' : `Add Agent unavailable: ${agentCreationPrerequisites.message}`}
                className="link-button"
                disabled={!canAddAgent}
                type="button"
                onClick={onAddAgent}
              >
                Add Agent
              </button>
            ) : null}
            {prerequisites.status === 'missing'
              && prerequisites.missingAgent
              && agentCreationPrerequisites.status === 'missing' ? (
                <button className="link-button" type="button" onClick={onAddInteraction}>
                  Add Interaction
                </button>
              ) : null}
            {prerequisites.status === 'missing'
              && prerequisites.missingAgent
              && agentCreationPrerequisites.status === 'error' ? (
                <button className="link-button" type="button" onClick={onRetryAgentPrerequisites}>
                  Retry interactions
                </button>
              ) : null}
            {prerequisites.status === 'missing' && prerequisites.missingPolicy ? (
              <button className="link-button" type="button" onClick={onAddPolicy}>
                Add Policy
              </button>
            ) : null}
          </div>
        </div>
      ) : null}
    </div>
  );
}

function HistorySectionActions({ onRefresh }: { onRefresh: () => void }) {
  return (
    <div className="section-actions">
      <button className="icon-button bordered" type="button" aria-label="Refresh" onClick={onRefresh}>
        <RefreshCw size={16} />
      </button>
      <button className="icon-button bordered" type="button" aria-label="More actions">
        <MoreHorizontal size={16} />
      </button>
    </div>
  );
}

function RowActions({ onDelete, onEdit, targetName }: { onDelete?: () => void; onEdit?: () => void; targetName: string }) {
  return (
    <div className="row-actions">
      <button className="icon-button bordered" type="button" aria-label={`Edit ${targetName}`} onClick={onEdit}>
        <Pencil size={15} />
      </button>
      <button className="icon-button bordered danger" type="button" aria-label={`Delete ${targetName}`} onClick={onDelete}>
        <Trash2 size={15} />
      </button>
    </div>
  );
}

function DeleteOnlyAction({ onDelete, targetName }: { onDelete: () => void; targetName: string }) {
  return (
    <div className="row-actions">
      <button className="icon-button bordered danger" type="button" aria-label={`Delete ${targetName}`} onClick={onDelete}>
        <Trash2 size={15} />
      </button>
    </div>
  );
}

function SettingsModal({
  appearanceError,
  error,
  isSaving,
  isSavingAppearance,
  onClose,
  onPreviewTheme,
  onSaveTheme,
  onSubmit,
  csrfToken,
  theme,
  user
}: {
  appearanceError: Error | null;
  error: Error | null;
  isSaving: boolean;
  isSavingAppearance: boolean;
  onClose: () => void;
  onPreviewTheme: (theme: Theme) => void;
  onSaveTheme: (theme: Theme) => Promise<void>;
  onSubmit: (input: ChangePasswordInput) => Promise<void>;
  csrfToken: string;
  theme: Theme;
  user: UserProfile;
}) {
  const [activeSection, setActiveSection] = useState<'security' | 'appearance' | 'llm'>('security');
  const [draftTheme, setDraftTheme] = useState<Theme>(theme);
  const [appearanceChanged, setAppearanceChanged] = useState(false);
  const [currentPassword, setCurrentPassword] = useState('');
  const [newPassword, setNewPassword] = useState('');
  const [confirmPassword, setConfirmPassword] = useState('');
  const [showCurrentPassword, setShowCurrentPassword] = useState(false);
  const [showNewPassword, setShowNewPassword] = useState(false);
  const [showConfirmPassword, setShowConfirmPassword] = useState(false);
  const [localError, setLocalError] = useState('');
  const [changed, setChanged] = useState(false);

  function updatePassword(setter: (value: string) => void, value: string) {
    setter(value);
    setLocalError('');
    setChanged(false);
  }

  async function handleSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    setLocalError('');
    setChanged(false);
    if (!currentPassword || !newPassword || !confirmPassword) {
      setLocalError('All password fields are required.');
      return;
    }
    if (newPassword !== confirmPassword) {
      setLocalError('New passwords must match.');
      return;
    }
    try {
      await onSubmit({ current_password: currentPassword, new_password: newPassword });
      setCurrentPassword('');
      setNewPassword('');
      setConfirmPassword('');
      setShowCurrentPassword(false);
      setShowNewPassword(false);
      setShowConfirmPassword(false);
      setChanged(true);
    } catch {
      // Mutation errors are rendered from React Query state below the fields.
    }
  }

  function selectTheme(nextTheme: Theme) {
    setDraftTheme(nextTheme);
    setAppearanceChanged(false);
    onPreviewTheme(nextTheme);
  }

  async function handleSaveAppearance(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    setAppearanceChanged(false);
    try {
      await onSaveTheme(draftTheme);
      setAppearanceChanged(true);
    } catch {
      // Mutation errors are rendered from React Query state below the choices.
    }
  }

  return (
    <div className="modal-backdrop" role="presentation">
      <section aria-modal="true" className="dialog settings-dialog" role="dialog" aria-labelledby="settings-dialog-title">
        <div className="dialog-header settings-dialog-header">
          <div>
            <span className="settings-user-label">{user.name || user.username}</span>
            <h2 id="settings-dialog-title">Settings</h2>
            <p>Manage your Proxyma account and preferences.</p>
          </div>
          <button
            className="icon-button"
            type="button"
            aria-label="Close settings"
            onClick={onClose}
            disabled={isSaving || isSavingAppearance}
          >
            <X size={18} />
          </button>
        </div>

        <div className="settings-layout">
          <nav className="settings-nav" aria-label="Settings sections">
            <button
              className={`settings-nav-item ${activeSection === 'security' ? 'active' : ''}`}
              type="button"
              aria-current={activeSection === 'security' ? 'page' : undefined}
              onClick={() => setActiveSection('security')}
            >
              <ShieldCheck size={17} />
              Security
            </button>
            <button
              className={`settings-nav-item ${activeSection === 'appearance' ? 'active' : ''}`}
              type="button"
              aria-current={activeSection === 'appearance' ? 'page' : undefined}
              onClick={() => setActiveSection('appearance')}
            >
              <Palette size={17} />
              Appearance
            </button>
            <button
              className={`settings-nav-item ${activeSection === 'llm' ? 'active' : ''}`}
              type="button"
              aria-current={activeSection === 'llm' ? 'page' : undefined}
              onClick={() => setActiveSection('llm')}
            >
              <Bot size={17} />
              LLM profiles
            </button>
          </nav>

          {activeSection === 'security' ? (
          <form className="settings-content" onSubmit={handleSubmit}>
            <div>
              <h3>Password</h3>
              <p>Choose a password that is unique to your Proxyma account.</p>
            </div>
            <div className="dialog-grid settings-password-grid">
              <div className="dialog-field wide">
                <label htmlFor="settings-current-password">Current password</label>
                <div className="password-input-shell">
                  <input
                    autoComplete="current-password"
                    id="settings-current-password"
                    type={showCurrentPassword ? 'text' : 'password'}
                    value={currentPassword}
                    onChange={(event) => updatePassword(setCurrentPassword, event.target.value)}
                  />
                  <button
                    aria-label={showCurrentPassword ? 'Hide current password' : 'Show current password'}
                    className="icon-button"
                    onClick={() => setShowCurrentPassword((visible) => !visible)}
                    type="button"
                  >
                    {showCurrentPassword ? <EyeOff size={17} /> : <Eye size={17} />}
                  </button>
                </div>
              </div>
              <div className="dialog-field">
                <label htmlFor="settings-new-password">New password</label>
                <div className="password-input-shell">
                  <input
                    autoComplete="new-password"
                    id="settings-new-password"
                    type={showNewPassword ? 'text' : 'password'}
                    value={newPassword}
                    onChange={(event) => updatePassword(setNewPassword, event.target.value)}
                  />
                  <button
                    aria-label={showNewPassword ? 'Hide new password' : 'Show new password'}
                    className="icon-button"
                    onClick={() => setShowNewPassword((visible) => !visible)}
                    type="button"
                  >
                    {showNewPassword ? <EyeOff size={17} /> : <Eye size={17} />}
                  </button>
                </div>
              </div>
              <div className="dialog-field">
                <label htmlFor="settings-confirm-password">Confirm new password</label>
                <div className="password-input-shell">
                  <input
                    autoComplete="new-password"
                    id="settings-confirm-password"
                    type={showConfirmPassword ? 'text' : 'password'}
                    value={confirmPassword}
                    onChange={(event) => updatePassword(setConfirmPassword, event.target.value)}
                  />
                  <button
                    aria-label={showConfirmPassword ? 'Hide confirm new password' : 'Show confirm new password'}
                    className="icon-button"
                    onClick={() => setShowConfirmPassword((visible) => !visible)}
                    type="button"
                  >
                    {showConfirmPassword ? <EyeOff size={17} /> : <Eye size={17} />}
                  </button>
                </div>
              </div>
            </div>

            <p className="settings-session-note">This browser stays signed in. Your other Proxyma sessions will be signed out.</p>
            {localError || error ? <div className="form-error compact">{localError || error?.message}</div> : null}
            {changed ? (
              <div className="form-success" role="status">
                <CheckCircle2 size={17} />
                Password changed successfully.
              </div>
            ) : null}

            <div className="dialog-actions settings-actions">
              <button className="ghost-button" type="button" onClick={onClose} disabled={isSaving}>
                Cancel
              </button>
              <button className="primary-action" type="submit" disabled={isSaving}>
                {isSaving ? 'Saving...' : 'Save password'}
              </button>
            </div>
          </form>
          ) : activeSection === 'appearance' ? (
            <form className="settings-content" onSubmit={handleSaveAppearance}>
              <div>
                <h3>Appearance</h3>
                <p>Choose how Proxyma looks for your account.</p>
              </div>

              <div className="theme-choice-list" role="radiogroup" aria-label="Color theme">
                <label className={`theme-choice ${draftTheme === 'light' ? 'selected' : ''}`}>
                  <input
                    checked={draftTheme === 'light'}
                    name="theme"
                    onChange={() => selectTheme('light')}
                    type="radio"
                    value="light"
                  />
                  <span className="theme-preview light-preview" aria-hidden="true">
                    <i />
                    <i />
                    <i />
                  </span>
                  <span>
                    <strong>Current UI</strong>
                    <small>The original warm, light appearance.</small>
                  </span>
                </label>
                <label className={`theme-choice ${draftTheme === 'dark' ? 'selected' : ''}`}>
                  <input
                    checked={draftTheme === 'dark'}
                    name="theme"
                    onChange={() => selectTheme('dark')}
                    type="radio"
                    value="dark"
                  />
                  <span className="theme-preview dark-preview" aria-hidden="true">
                    <i />
                    <i />
                    <i />
                  </span>
                  <span>
                    <strong>Dark</strong>
                    <small>A low-glare version using the same Proxyma styling.</small>
                  </span>
                </label>
              </div>

              <p className="settings-session-note">Your choice applies to this account on every sign-in.</p>
              {appearanceError ? <div className="form-error compact">{appearanceError.message}</div> : null}
              {appearanceChanged ? (
                <div className="form-success" role="status">
                  <CheckCircle2 size={17} />
                  Appearance saved successfully.
                </div>
              ) : null}

              <div className="dialog-actions settings-actions">
                <button className="ghost-button" type="button" onClick={onClose} disabled={isSavingAppearance}>
                  Cancel
                </button>
                <button className="primary-action" type="submit" disabled={isSavingAppearance || draftTheme === theme}>
                  {isSavingAppearance ? 'Saving...' : 'Save appearance'}
                </button>
              </div>
            </form>
          ) : (
            <div className="settings-content">
              <LLMProfilesSettings csrfToken={csrfToken} user={user} />
            </div>
          )}
        </div>
      </section>
    </div>
  );
}

function UserFormModal({
  activationPolicy,
  error,
  isSaving,
  mode,
  onClose,
  onSubmit,
  user
}: {
  activationPolicy?: ActivationPolicy;
  error: Error | null;
  isSaving: boolean;
  mode: 'create' | 'edit';
  onClose: () => void;
  onSubmit: (input: UserMutationInput) => Promise<UserProfile | void>;
  user?: UserProfile;
}) {
  const [username, setUsername] = useState(user?.username ?? '');
  const [name, setName] = useState(user?.name ?? '');
  const [admin, setAdmin] = useState(user?.admin ?? false);
  const [localError, setLocalError] = useState('');
  const [lifetimeValue, setLifetimeValue] = useState((activationPolicy?.default_lifetime_seconds ?? 12 * 3600) / 3600);
  const [lifetimeUnit, setLifetimeUnit] = useState<'minutes' | 'hours' | 'days'>('hours');
  const [createdUser, setCreatedUser] = useState<UserProfile | null>(null);

  async function handleSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    setLocalError('');
    const input: UserMutationInput = {
      username,
      name,
      admin
    };
    if (mode === 'create') {
      input.activation_token_lifetime_seconds = Math.round(lifetimeValue * lifetimeUnitSeconds(lifetimeUnit));
      if (input.activation_token_lifetime_seconds <= 0 || input.activation_token_lifetime_seconds > (activationPolicy?.maximum_lifetime_seconds ?? 48 * 3600)) {
        setLocalError('Activation lifetime must be positive and within the configured maximum.');
        return;
      }
    }
    try {
      const result = await onSubmit(input);
      if (result?.activation_url) setCreatedUser(result);
    } catch {
      // Mutation errors are rendered from React Query state above the action row.
    }
  }

  const title = mode === 'create' ? 'Add User' : `Edit ${user ? userLabel(user) : 'User'}`;

  if (createdUser?.activation_url) {
    const absoluteURL = new URL(createdUser.activation_url, window.location.origin).toString();
    return <div className="modal-backdrop" role="presentation"><section aria-modal="true" className="dialog" role="dialog" aria-labelledby="activation-link-title">
      <div className="dialog-header"><div><h2 id="activation-link-title">User created</h2><p>Share this one-time activation link with {createdUser.name}.</p></div><button className="icon-button" type="button" aria-label="Close activation link" onClick={onClose}><X size={18} /></button></div>
      <div className="dialog-grid"><label className="dialog-field wide"><span>Activation link</span><input readOnly value={absoluteURL} /><small>This secret link will not be shown again. It expires {createdUser.activation_expires_at ? formatDate(createdUser.activation_expires_at) : 'at the configured time'}.</small></label></div>
      <div className="dialog-actions"><button className="ghost-button" type="button" onClick={onClose}>Close</button><button className="primary-action" type="button" onClick={() => void navigator.clipboard.writeText(absoluteURL)}>Copy link</button></div>
    </section></div>;
  }

  return (
    <div className="modal-backdrop" role="presentation">
      <section aria-modal="true" className="dialog" role="dialog" aria-labelledby="user-dialog-title">
        <div className="dialog-header">
          <div>
            <h2 id="user-dialog-title">{title}</h2>
            <p>{mode === 'create' ? 'The user will choose a password from a one-time activation link.' : 'Update the user profile. Password resets are initiated from the Users list.'}</p>
          </div>
          <button className="icon-button" type="button" aria-label="Close user dialog" onClick={onClose}>
            <X size={18} />
          </button>
        </div>

        <form onSubmit={handleSubmit}>
          <div className="dialog-grid">
            <label className="dialog-field">
              <span>Username</span>
              <input value={username} onChange={(event) => setUsername(event.target.value)} placeholder="ops.user" />
            </label>
            <label className="dialog-field">
              <span>Name</span>
              <input value={name} onChange={(event) => setName(event.target.value)} placeholder="Operations User" />
            </label>
            {mode === 'create' ? <>
              <label className="dialog-field"><span>Activation lifetime</span><input min="1" type="number" value={lifetimeValue} onChange={(event) => setLifetimeValue(Number(event.target.value))} /></label>
              <label className="dialog-field"><span>Unit</span><select value={lifetimeUnit} onChange={(event) => setLifetimeUnit(event.target.value as 'minutes' | 'hours' | 'days')}><option value="minutes">Minutes</option><option value="hours">Hours</option><option value="days">Days</option></select><small>Maximum {formatDuration(activationPolicy?.maximum_lifetime_seconds ?? 48 * 3600)}.</small></label>
            </> : null}
            <label className="toggle-field wide">
              <input type="checkbox" checked={admin} onChange={(event) => setAdmin(event.target.checked)} />
              <span>Administrator</span>
            </label>
          </div>

          {localError || error ? <div className="form-error compact">{localError || error?.message}</div> : null}

          <div className="dialog-actions">
            <button className="ghost-button" type="button" onClick={onClose}>
              Cancel
            </button>
            <button className="primary-action" type="submit" disabled={isSaving}>
              {isSaving ? 'Saving...' : 'Save User'}
            </button>
          </div>
        </form>
      </section>
    </div>
  );
}

function lifetimeUnitSeconds(unit: 'minutes' | 'hours' | 'days') {
  return unit === 'minutes' ? 60 : unit === 'days' ? 86400 : 3600;
}

function formatDuration(seconds: number) {
  if (seconds % 86400 === 0) return `${seconds / 86400} days`;
  if (seconds % 3600 === 0) return `${seconds / 3600} hours`;
  return `${Math.floor(seconds / 60)} minutes`;
}

function RegenerateActivationModal({ activationPolicy, error, isSaving, onClose, onSubmit, purpose, user }: {
  activationPolicy?: ActivationPolicy;
  error: Error | null;
  isSaving: boolean;
  onClose: () => void;
  onSubmit: (seconds: number) => Promise<{ activation_url: string; activation_expires_at: string }>;
  purpose: 'activation' | 'password_reset';
  user: UserProfile;
}) {
  const [value, setValue] = useState((activationPolicy?.default_lifetime_seconds ?? 12 * 3600) / 3600);
  const [unit, setUnit] = useState<'minutes' | 'hours' | 'days'>('hours');
  const [result, setResult] = useState<{ activation_url: string; activation_expires_at: string } | null>(null);
  const [localError, setLocalError] = useState('');
  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const seconds = Math.round(value * lifetimeUnitSeconds(unit));
    if (seconds <= 0 || seconds > (activationPolicy?.maximum_lifetime_seconds ?? 48 * 3600)) return setLocalError('Link lifetime is outside the configured limit.');
    setLocalError('');
    try { setResult(await onSubmit(seconds)); } catch { /* rendered below */ }
  }
  const absoluteURL = result ? new URL(result.activation_url, window.location.origin).toString() : '';
  const isReset = purpose === 'password_reset';
  const linkName = isReset ? 'password reset' : 'activation';
  return <div className="modal-backdrop" role="presentation"><section aria-modal="true" className="dialog" role="dialog" aria-labelledby="regenerate-title">
    <div className="dialog-header"><div><h2 id="regenerate-title">{result ? `${isReset ? 'Password reset' : 'Activation'} link ready` : isReset && user.status === 'active' ? 'Reset password' : `Regenerate ${linkName} link`}</h2><p>{result ? `Share this one-time link with ${user.name}.` : isReset && user.status === 'active' ? `${user.name} will be signed out and their current password will stop working.` : `The previous link for ${user.name} will stop working immediately.`}</p></div><button className="icon-button" type="button" aria-label={`Close ${linkName} link dialog`} onClick={onClose}><X size={18} /></button></div>
    {result ? <><div className="dialog-grid"><label className="dialog-field wide"><span>{isReset ? 'Password reset' : 'Activation'} link</span><input readOnly value={absoluteURL} /><small>This secret link will not be shown again. It expires {formatDate(result.activation_expires_at)}.</small></label></div><div className="dialog-actions"><button className="ghost-button" type="button" onClick={onClose}>Close</button><button className="primary-action" type="button" onClick={() => void navigator.clipboard.writeText(absoluteURL)}>Copy link</button></div></> : <form onSubmit={submit}><div className="dialog-grid"><label className="dialog-field"><span>Link lifetime</span><input min="1" type="number" value={value} onChange={(event) => setValue(Number(event.target.value))} /></label><label className="dialog-field"><span>Unit</span><select value={unit} onChange={(event) => setUnit(event.target.value as 'minutes' | 'hours' | 'days')}><option value="minutes">Minutes</option><option value="hours">Hours</option><option value="days">Days</option></select><small>Maximum {formatDuration(activationPolicy?.maximum_lifetime_seconds ?? 48 * 3600)}.</small></label></div>{localError || error ? <div className="form-error compact">{localError || error?.message}</div> : null}<div className="dialog-actions"><button className="ghost-button" type="button" onClick={onClose}>Cancel</button><button className="primary-action" type="submit" disabled={isSaving}>{isSaving ? 'Creating...' : isReset && user.status === 'active' ? 'Create reset link' : 'Regenerate link'}</button></div></form>}
  </section></div>;
}

function AgentFormModal({
  agent,
  currentUserID,
  error,
  interactionError,
  interactions,
  interactionsLoading,
  isSaving,
  mode,
  onClose,
  onSubmit
}: {
  agent?: Agent;
  currentUserID: string;
  error: Error | null;
  interactionError: Error | null;
  interactions: InteractionInstance[];
  interactionsLoading: boolean;
  isSaving: boolean;
  mode: 'create' | 'edit';
  onClose: () => void;
  onSubmit: (input: AgentMutationInput) => Promise<Agent>;
}) {
  const [displayName, setDisplayName] = useState(agent?.display_name ?? '');
  const [agentJwtSigningKey, setAgentJwtSigningKey] = useState('');
  const [showAgentJwtSigningKey, setShowAgentJwtSigningKey] = useState(false);
  const [enabled, setEnabled] = useState(agent?.enabled ?? true);
  const [interactionInstanceID, setInteractionInstanceID] = useState(() => {
    const assignedInteraction = agent?.interaction_instance_id;
    if (assignedInteraction && (showLocalInteraction || assignedInteraction !== 'local')) {
      return assignedInteraction;
    }
    return showLocalInteraction ? 'local' : '';
  });
  const [localError, setLocalError] = useState('');
  const [createdAgent, setCreatedAgent] = useState<Agent | null>(null);
  const [copied, setCopied] = useState<'id' | 'configuration' | null>(null);

  const ownedInteractions = selectableInteractions(interactions, currentUserID)
    .sort((left, right) => interactionLabel(left).localeCompare(interactionLabel(right)));
  const currentInteraction = interactions.find((interaction) => interaction.id === interactionInstanceID);
  const currentAssignmentUnavailable = interactionInstanceID !== 'local'
    && !ownedInteractions.some((interaction) => interaction.id === interactionInstanceID);

  async function handleSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    setLocalError('');
    if (mode === 'create' && !agentJwtSigningKey.trim()) {
      setLocalError('Agent JWT Signing Key is required for a new agent.');
      return;
    }
    if (!showLocalInteraction && !interactionInstanceID) {
      setLocalError('Select an enabled interaction before saving the agent.');
      return;
    }
    const input: AgentMutationInput = {
      display_name: displayName,
      enabled,
      interaction_instance_id: interactionInstanceID
    };
    if (mode === 'create' || agentJwtSigningKey.trim()) {
      input.agent_jwt_signing_key = agentJwtSigningKey;
    }
    try {
      const savedAgent = await onSubmit(input);
      if (mode === 'create') {
        setCreatedAgent(savedAgent);
      }
    } catch {
      // Mutation errors are rendered from React Query state above the action row.
    }
  }

  async function copyToClipboard(value: string, target: 'id' | 'configuration') {
    setLocalError('');
    try {
      await navigator.clipboard.writeText(value);
      setCopied(target);
    } catch {
      setCopied(null);
      setLocalError('Unable to copy to the clipboard. Select the value and copy it manually.');
    }
  }

  const title = createdAgent ? 'Agent created' : mode === 'create' ? 'Add Agent' : `Edit ${agent ? agentLabel(agent) : 'Agent'}`;
  const signingKeyHelp = mode === 'edit'
    ? 'Leave blank to keep the stored Agent JWT Signing Key.'
    : 'Symmetric key material used to sign short-lived Proxyma JWTs; the key itself is never sent to the API.';
  const harnessConfiguration = createdAgent
    ? JSON.stringify({ agent_id: createdAgent.id, agent_jwt_signing_key: agentJwtSigningKey }, null, 2)
    : '';

  return (
    <div className="modal-backdrop" role="presentation">
      <section aria-modal="true" className="dialog" role="dialog" aria-labelledby="agent-dialog-title">
        <div className="dialog-header">
          <div>
            <h2 id="agent-dialog-title">{title}</h2>
            <p>
              {createdAgent
                ? 'Copy these credentials into the Proxyma harness before closing this dialog.'
                : 'Agent JWT Signing Keys are write-only and are never shown after saving.'}
            </p>
          </div>
          <button className="icon-button" type="button" aria-label="Close agent dialog" onClick={onClose}>
            <X size={18} />
          </button>
        </div>

        {createdAgent ? (
          <div className="agent-created-panel">
            <div className="agent-created-banner">
              <CheckCircle2 size={19} />
              <div>
                <strong>{agentLabel(createdAgent)} is ready</strong>
                <span>The Agent ID was generated by Proxyma and is now active.</span>
              </div>
            </div>

            <div className="dialog-grid">
              <div className="dialog-field wide">
                <label htmlFor="created-agent-id">Agent ID</label>
                <div className="copy-input-shell">
                  <input id="created-agent-id" readOnly value={createdAgent.id} />
                  <button
                    aria-label="Copy Agent ID"
                    className="icon-button"
                    onClick={() => void copyToClipboard(createdAgent.id, 'id')}
                    type="button"
                  >
                    {copied === 'id' ? <CheckCircle2 size={17} /> : <Copy size={17} />}
                  </button>
                </div>
                <small>Use this value as <code>agent_id</code> in the harness agent configuration.</small>
              </div>

              <label className="dialog-field wide">
                <span>Harness agent configuration</span>
                <textarea readOnly rows={5} value={harnessConfiguration} />
                <small>This configuration contains the Agent JWT Signing Key and will not be shown again after you close this dialog.</small>
              </label>
            </div>

            <div aria-live="polite" className="copy-status" role="status">
              {copied === 'id' ? 'Agent ID copied.' : copied === 'configuration' ? 'Harness configuration copied.' : ''}
            </div>
            {localError ? <div className="form-error compact">{localError}</div> : null}

            <div className="dialog-actions">
              <button className="ghost-button" type="button" onClick={onClose}>
                Done
              </button>
              <button
                className="primary-action"
                type="button"
                onClick={() => void copyToClipboard(harnessConfiguration, 'configuration')}
              >
                {copied === 'configuration' ? <CheckCircle2 size={17} /> : <Copy size={17} />}
                {copied === 'configuration' ? 'Configuration copied' : 'Copy configuration'}
              </button>
            </div>
          </div>
        ) : (
        <form onSubmit={handleSubmit}>
          <div className="dialog-grid">
            <label className="dialog-field wide">
              <span>Display name</span>
              <input value={displayName} onChange={(event) => setDisplayName(event.target.value)} placeholder="Build automation agent" />
            </label>
            {mode === 'edit' && agent ? (
              <div className="dialog-field wide">
                <label htmlFor="agent-id">Agent ID</label>
                <div className="copy-input-shell">
                  <input id="agent-id" readOnly value={agent.id} />
                  <button
                    aria-label="Copy Agent ID"
                    className="icon-button"
                    onClick={() => void copyToClipboard(agent.id, 'id')}
                    type="button"
                  >
                    {copied === 'id' ? <CheckCircle2 size={17} /> : <Copy size={17} />}
                  </button>
                </div>
                <small>Use this value as <code>agent_id</code> in the harness agent configuration.</small>
              </div>
            ) : null}
            <div className="dialog-field wide">
              <label htmlFor="agent-jwt-signing-key">Agent JWT Signing Key</label>
              <div className="password-input-shell">
                <input
                  autoComplete="new-password"
                  id="agent-jwt-signing-key"
                  type={showAgentJwtSigningKey ? 'text' : 'password'}
                  value={agentJwtSigningKey}
                  onChange={(event) => setAgentJwtSigningKey(event.target.value)}
                  placeholder={mode === 'edit' ? 'Keep current Agent JWT Signing Key' : 'Agent JWT Signing Key'}
                />
                <button
                  aria-label={showAgentJwtSigningKey ? 'Hide Agent JWT Signing Key' : 'Show Agent JWT Signing Key'}
                  className="icon-button"
                  onClick={() => setShowAgentJwtSigningKey((visible) => !visible)}
                  type="button"
                >
                  {showAgentJwtSigningKey ? <EyeOff size={17} /> : <Eye size={17} />}
                </button>
              </div>
              <small>{signingKeyHelp}</small>
            </div>
            <label className="dialog-field wide">
              <span>Interaction</span>
              <select
                disabled={interactionsLoading || Boolean(interactionError)}
                value={interactionInstanceID}
                onChange={(event) => setInteractionInstanceID(event.target.value)}
              >
                {showLocalInteraction
                  ? <option value="local">Local console · built in</option>
                  : <option disabled value="">Select an interaction</option>}
                {ownedInteractions.map((interaction) => (
                  <option key={interaction.id} value={interaction.id}>
                    {interactionLabel(interaction)} · {interaction.type.toUpperCase()}
                  </option>
                ))}
                {currentAssignmentUnavailable ? (
                  <option disabled value={interactionInstanceID}>
                    {currentInteraction ? interactionLabel(currentInteraction) : interactionInstanceID} · currently assigned, unavailable
                  </option>
                ) : null}
              </select>
              <small>
                {currentAssignmentUnavailable
                  ? showLocalInteraction
                    ? 'This assignment belongs to another user or is unavailable. Keep it unchanged, or replace it with Local or one of your interactions.'
                    : 'This assignment belongs to another user or is unavailable. Keep it unchanged, or replace it with one of your interactions.'
                  : showLocalInteraction
                    ? 'Choose Local or an enabled interaction you created.'
                    : 'Choose an enabled interaction you created.'}
              </small>
            </label>
            <label className="toggle-field wide">
              <input type="checkbox" checked={enabled} onChange={(event) => setEnabled(event.target.checked)} />
              <span>Enabled</span>
            </label>
          </div>

          {localError || error || interactionError ? <div className="form-error compact">{localError || error?.message || interactionError?.message}</div> : null}
          <div aria-live="polite" className="copy-status" role="status">
            {copied === 'id' ? 'Agent ID copied.' : ''}
          </div>

          <div className="dialog-actions">
            <button className="ghost-button" type="button" onClick={onClose}>
              Cancel
            </button>
            <button className="primary-action" type="submit" disabled={isSaving || interactionsLoading || Boolean(interactionError)}>
              {isSaving ? 'Saving...' : 'Save Agent'}
            </button>
          </div>
        </form>
        )}
      </section>
    </div>
  );
}

function InteractionTypeModal({ onClose, onSelectNtfy }: { onClose: () => void; onSelectNtfy: () => void }) {
  return (
    <div className="modal-backdrop" role="presentation">
      <section aria-modal="true" className="dialog" role="dialog" aria-labelledby="interaction-type-title">
        <div className="dialog-header">
          <div>
            <h2 id="interaction-type-title">Add Interaction</h2>
            <p>Select an interaction type to configure.</p>
          </div>
          <button className="icon-button" type="button" aria-label="Close interaction type dialog" onClick={onClose}>
            <X size={18} />
          </button>
        </div>
        <div className="dialog-grid">
          <button className="type-choice" type="button" onClick={onSelectNtfy}>
            <ListChecks size={20} />
            <span>
              <strong>NTFY</strong>
              <small>Publish approval requests to an ntfy topic.</small>
            </span>
          </button>
          {showLocalInteraction ? (
            <button className="type-choice" type="button" disabled>
              <PlugZap size={20} />
              <span>
                <strong>Local</strong>
                <small>Built-in singleton. No file-backed instance is created.</small>
              </span>
            </button>
          ) : null}
        </div>
      </section>
    </div>
  );
}

function InteractionFormModal({
  error,
  interaction,
  isSaving,
  managedUsers,
  mode,
  onClose,
  onSubmit,
  showOwnerSelector
}: {
  error: Error | null;
  interaction?: InteractionInstance;
  isSaving: boolean;
  managedUsers: UserProfile[];
  mode: 'create' | 'edit';
  onClose: () => void;
  onSubmit: (input: InteractionMutationInput) => Promise<void>;
  showOwnerSelector: boolean;
}) {
  const [displayName, setDisplayName] = useState(interaction?.display_name ?? '');
  const [enabled, setEnabled] = useState(interaction?.enabled ?? true);
  const [creatorUserID, setCreatorUserID] = useState(interaction?.creator_user_id ?? managedUsers[0]?.id ?? '');
  const [baseURL, setBaseURL] = useState(interaction?.base_url ?? 'https://ntfy.sh');
  const [requestTopic, setRequestTopic] = useState(interaction?.request_topic ?? '');
  const [responseTopic, setResponseTopic] = useState(interaction?.response_topic ?? '');
  const [token, setToken] = useState('');
  const [pollInterval, setPollInterval] = useState(interaction?.poll_interval ?? '5s');
  const [pollTimeout, setPollTimeout] = useState(interaction?.poll_timeout ?? '30s');
  const [respondedBy, setRespondedBy] = useState(interaction?.responded_by ?? 'ntfy');

  async function handleSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    await onSubmit({
      type: 'ntfy',
      creator_user_id: showOwnerSelector ? creatorUserID : undefined,
      display_name: displayName,
      enabled,
      base_url: baseURL,
      request_topic: requestTopic,
      response_topic: responseTopic,
      token,
      poll_interval: pollInterval,
      poll_timeout: pollTimeout,
      responded_by: respondedBy
    });
  }

  const title = mode === 'create' ? 'Add NTFY Interaction' : `Edit ${interaction ? interactionLabel(interaction) : 'NTFY Interaction'}`;

  return (
    <div className="modal-backdrop" role="presentation">
      <section aria-modal="true" className="dialog wide" role="dialog" aria-labelledby="interaction-dialog-title">
        <div className="dialog-header">
          <div>
            <h2 id="interaction-dialog-title">{title}</h2>
            <p>NTFY tokens are write-only and are never shown after saving.</p>
          </div>
          <button className="icon-button" type="button" aria-label="Close interaction dialog" onClick={onClose}>
            <X size={18} />
          </button>
        </div>
        <form onSubmit={handleSubmit}>
          <div className="dialog-grid">
            <label className="dialog-field">
              <span>Display Name</span>
              <input value={displayName} onChange={(event) => setDisplayName(event.target.value)} placeholder="Ops phone" />
            </label>
            {showOwnerSelector ? (
              <label className="dialog-field">
                <span>Creator</span>
                <select value={creatorUserID} onChange={(event) => setCreatorUserID(event.target.value)} required>
                  {managedUsers.map((user) => (
                    <option key={user.id} value={user.id}>
                      {userLabel(user)}
                    </option>
                  ))}
                </select>
              </label>
            ) : null}
            <label className="dialog-field">
              <span>Base URL</span>
              <input value={baseURL} onChange={(event) => setBaseURL(event.target.value)} placeholder="https://ntfy.sh" required />
            </label>
            <label className="dialog-field">
              <span>Request Topic</span>
              <input value={requestTopic} onChange={(event) => setRequestTopic(event.target.value)} placeholder="approval-requests" required />
            </label>
            <label className="dialog-field">
              <span>Response Topic</span>
              <input value={responseTopic} onChange={(event) => setResponseTopic(event.target.value)} placeholder="approval-responses" required />
            </label>
            <label className="dialog-field">
              <span>Token</span>
              <input
                type="password"
                value={token}
                onChange={(event) => setToken(event.target.value)}
                placeholder={mode === 'edit' && interaction?.token_configured ? 'Keep current token' : 'Optional bearer token'}
              />
            </label>
            <label className="dialog-field">
              <span>Poll Interval</span>
              <input value={pollInterval} onChange={(event) => setPollInterval(event.target.value)} placeholder="5s" required />
            </label>
            <label className="dialog-field">
              <span>Poll Timeout</span>
              <input value={pollTimeout} onChange={(event) => setPollTimeout(event.target.value)} placeholder="30s" required />
            </label>
            <label className="dialog-field">
              <span>Responded By</span>
              <input value={respondedBy} onChange={(event) => setRespondedBy(event.target.value)} placeholder="ntfy" />
            </label>
            <label className="toggle-field">
              <input type="checkbox" checked={enabled} onChange={(event) => setEnabled(event.target.checked)} />
              <span>Enabled</span>
            </label>
          </div>
          {error ? <div className="form-error compact">{error.message}</div> : null}
          <div className="dialog-actions">
            <button className="ghost-button" type="button" onClick={onClose}>
              Cancel
            </button>
            <button className="primary-action" type="submit" disabled={isSaving}>
              {isSaving ? 'Saving...' : 'Save Interaction'}
            </button>
          </div>
        </form>
      </section>
    </div>
  );
}

function TargetFormModal({
  agents,
  error,
  isSaving,
  isTestingCredentials,
  mode,
  onClose,
  onSubmit,
  onTestCredentials,
  policies,
  target
}: {
  agents: Agent[];
  error: Error | null;
  isSaving: boolean;
  isTestingCredentials: boolean;
  mode: 'create' | 'edit';
  onClose: () => void;
  onSubmit: (input: TargetMutationInput) => Promise<void>;
  onTestCredentials: (input: TargetCredentialTestInput) => ReturnType<typeof testTargetCredentials>;
  policies: Policy[];
  target?: Target;
}) {
  const [hostname, setHostname] = useState(target?.hostname ?? '');
  const [ip, setIp] = useState(target?.ip ?? '');
  const [description, setDescription] = useState(target?.description ?? '');
  const [username, setUsername] = useState('');
	const [transport, setTransport] = useState<'ssh' | 'winrm'>(target?.transport ?? 'ssh');
	const [targetOS, setTargetOS] = useState<'linux' | 'windows'>(target?.os ?? (target?.transport === 'winrm' ? 'windows' : 'linux'));
  const [authType, setAuthType] = useState(target?.auth_type || 'password');
	const [winrmScheme, setWinrmScheme] = useState<'http' | 'https'>(target?.winrm?.scheme ?? 'https');
	const [winrmPort, setWinrmPort] = useState(String(target?.winrm?.port ?? 5986));
	const [winrmTLSInsecure, setWinrmTLSInsecure] = useState(target?.winrm?.tls_insecure ?? false);
  const [password, setPassword] = useState('');
  const [privateKeyFile, setPrivateKeyFile] = useState<File | null>(null);
  const [selectedPolicyID, setSelectedPolicyID] = useState(target?.policy_id ?? '');
  const [selectedAgentIDs, setSelectedAgentIDs] = useState<string[]>(target?.agent_ids ?? []);
  const [localError, setLocalError] = useState('');
  const [credentialTestStatus, setCredentialTestStatus] = useState<CredentialTestStatus>('idle');
  const [credentialTestError, setCredentialTestError] = useState('');
	const [credentialTestToken, setCredentialTestToken] = useState('');
	const [pendingHostKey, setPendingHostKey] = useState<SSHHostKeyInfo | null>(null);
  const policyOptions = useMemo(() => policySelectOptions(policies, selectedPolicyID), [policies, selectedPolicyID]);
  const agentOptions = useMemo(() => agentSelectOptions(agents, selectedAgentIDs), [agents, selectedAgentIDs]);
  const selectedAgentSummary = selectedAgentIDs.length === 0 ? 'Select agents' : selectedAgentIDs.map((id) => agentOptionLabel(id, agents)).join(', ');
  const canSaveTargetSelections = selectedPolicyID.trim() !== '' && selectedAgentIDs.length > 0;
	const credentialEndpointReady = transport === 'ssh' ? ip.trim() !== '' : (hostname.trim() !== '' || ip.trim() !== '');
  const credentialReady =
	credentialEndpointReady && username.trim() !== '' && (transport === 'ssh' && authType === 'private_key' ? privateKeyFile !== null : password !== '');
	const credentialTestRequired =
		mode === 'create' ||
		!!target && (
			transport !== target.transport ||
			hostname.trim() !== (target.hostname ?? '') ||
			ip.trim() !== (target.ip ?? '') ||
			username.trim() !== '' ||
			authType !== (target.auth_type || 'password') ||
			password !== '' ||
			privateKeyFile !== null ||
			(transport === 'winrm' && (
				winrmScheme !== (target.winrm?.scheme ?? 'https') ||
				Number(winrmPort) !== (target.winrm?.port ?? 5986) ||
				winrmTLSInsecure !== (target.winrm?.tls_insecure ?? false)
			))
		);

  async function handleSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    setLocalError('');
    if (!selectedPolicyID.trim()) {
      setLocalError('Policy name is required.');
      return;
    }
    if (selectedAgentIDs.length === 0) {
      setLocalError('At least one agent name is required.');
      return;
    }
		if (transport === 'winrm' && (!Number.isInteger(Number(winrmPort)) || Number(winrmPort) < 1 || Number(winrmPort) > 65535)) {
			setLocalError('WinRM port must be between 1 and 65535.');
			return;
		}
    const input: TargetMutationInput = {
		transport,
		os: transport === 'winrm' ? 'windows' : targetOS,
      hostname,
      ip,
      description,
      username,
      policy_id: selectedPolicyID.trim(),
      agent_ids: selectedAgentIDs,
      auth_type: authType
    };
	if (credentialTestToken) {
		input.credential_test_token = credentialTestToken;
	}
		if (transport === 'winrm') {
			input.auth_type = 'password';
			input.password = password;
			input.winrm = { scheme: winrmScheme, port: Number(winrmPort), tls_insecure: winrmTLSInsecure };
		} else if (authType === 'private_key') {
      if (privateKeyFile) {
        input.private_key_file = privateKeyFile;
      }
    } else {
      input.password = password;
    }
    if (mode === 'create' && !username.trim()) {
      setLocalError('Username is required for a new target.');
      return;
    }
		if ((mode === 'create' || target?.transport !== transport || target?.auth_type !== 'password') && (transport === 'winrm' || authType === 'password') && !password) {
      setLocalError('Password is required for a password target.');
      return;
    }
		if (mode === 'create' && transport === 'ssh' && authType === 'private_key' && !privateKeyFile) {
      setLocalError('Private key file is required for a private key target.');
      return;
    }
    try {
      await onSubmit(input);
    } catch {
      // Mutation errors are rendered from React Query state above the action row.
    }
  }

  function clearCredentialTest() {
    setCredentialTestStatus('idle');
    setCredentialTestError('');
	setCredentialTestToken('');
	setPendingHostKey(null);
  }

	function handleHostnameChange(nextHostname: string) {
		setHostname(nextHostname);
		clearCredentialTest();
	}

  function handleIPChange(nextIP: string) {
    setIp(nextIP);
    clearCredentialTest();
  }

  function handleUsernameChange(nextUsername: string) {
    setUsername(nextUsername);
    clearCredentialTest();
  }

  function handlePasswordChange(nextPassword: string) {
    setPassword(nextPassword);
    clearCredentialTest();
  }

  async function handleTestCredentials(approvedFingerprint?: string) {
    if (!credentialReady || isTestingCredentials) {
      return;
    }
    setCredentialTestStatus('testing');
    setCredentialTestError('');
    const input: TargetCredentialTestInput = {
		transport,
		os: transport === 'winrm' ? 'windows' : targetOS,
		hostname,
      ip,
      username,
      auth_type: authType
    };
	if (target?.id) input.target_id = target.id;
	if (approvedFingerprint) input.approved_host_key_fingerprint = approvedFingerprint;
		if (transport === 'winrm') {
			input.auth_type = 'password';
			input.password = password;
			input.winrm = { scheme: winrmScheme, port: Number(winrmPort), tls_insecure: winrmTLSInsecure };
		} else if (authType === 'private_key') {
      input.private_key_file = privateKeyFile ?? undefined;
    } else {
      input.password = password;
    }
    try {
	  const result = await onTestCredentials(input);
	  setCredentialTestToken(result.credential_test_token);
	  setPendingHostKey(null);
      setCredentialTestStatus('success');
    } catch (err) {
	  if (err instanceof APIError && err.code === 'ssh_host_key_unknown' && err.hostKey) {
		setPendingHostKey(err.hostKey);
		setCredentialTestStatus('idle');
		setCredentialTestError('');
		return;
	  }
      setCredentialTestStatus('error');
		setCredentialTestError(err instanceof Error ? err.message : `${transport === 'winrm' ? 'WinRM' : 'SSH'} credential test failed`);
    }
  }

  function handlePolicyChange(policyID: string) {
    setSelectedPolicyID(policyID);
  }

  function toggleAgent(agentID: string) {
    setSelectedAgentIDs((current) => {
      if (current.includes(agentID)) {
        return current.filter((id) => id !== agentID);
      }
      return [...current, agentID];
    });
  }

  function handleAuthTypeChange(nextAuthType: string) {
    setAuthType(nextAuthType);
    setLocalError('');
    clearCredentialTest();
    if (nextAuthType === 'private_key') {
      setPassword('');
    } else {
      setPrivateKeyFile(null);
    }
  }

	function handleTransportChange(nextTransport: 'ssh' | 'winrm') {
		setTransport(nextTransport);
		setAuthType('password');
		setPrivateKeyFile(null);
		if (nextTransport === 'winrm') {
			setTargetOS('windows');
		}
		clearCredentialTest();
	}

  function handlePrivateKeyFileChange(event: ChangeEvent<HTMLInputElement>) {
    setPrivateKeyFile(event.target.files?.[0] ?? null);
    clearCredentialTest();
  }

  const title = mode === 'create' ? 'Add Target' : `Edit ${target ? targetLabel(target) : 'Target'}`;
  const credentialHelp = mode === 'edit' ? 'Leave blank to keep the stored credential.' : undefined;
  const privateKeyHelp =
    mode === 'edit'
      ? 'Choose a new private key file to replace the stored credential, or leave blank to keep it.'
      : 'Upload an unencrypted SSH private key from your laptop. Proxyma stores it under secrets/private_keys/.';

  return (
    <div className="modal-backdrop" role="presentation">
      <section aria-modal="true" className="dialog" role="dialog" aria-labelledby="target-dialog-title">
        <div className="dialog-header">
          <div>
            <h2 id="target-dialog-title">{title}</h2>
            <p>Target credentials are write-only and are never shown after saving.</p>
          </div>
          <button className="icon-button" type="button" aria-label="Close target dialog" onClick={onClose}>
            <X size={18} />
          </button>
        </div>

        <form onSubmit={handleSubmit}>
          <div className="dialog-grid">
            <label className="dialog-field">
              <span>Hostname</span>
              <input value={hostname} onChange={(event) => handleHostnameChange(event.target.value)} placeholder="DEMO-WEB-01" />
            </label>
            <label className="dialog-field">
              <span>IP address</span>
              <input value={ip} onChange={(event) => handleIPChange(event.target.value)} placeholder="10.10.1.12" />
            </label>
            <label className="dialog-field wide">
              <span>Description</span>
              <input value={description} onChange={(event) => setDescription(event.target.value)} placeholder="Production web server" />
            </label>
            <label className="dialog-field">
              <span>Username</span>
              <input
                value={username}
                onChange={(event) => handleUsernameChange(event.target.value)}
                placeholder={mode === 'edit' ? 'Keep current username' : 'Proxyma-runner'}
              />
            </label>
			<label className="dialog-field">
				<span>Transport</span>
				<select value={transport} onChange={(event) => handleTransportChange(event.target.value as 'ssh' | 'winrm')}>
					<option value="ssh">SSH</option>
					<option value="winrm">WinRM</option>
				</select>
			</label>
			<label className="dialog-field">
				<span>Operating system</span>
				<select
					value={transport === 'winrm' ? 'windows' : targetOS}
					onChange={(event) => {
						setTargetOS(event.target.value as 'linux' | 'windows');
					}}
					disabled={transport === 'winrm'}
				>
					<option value="linux">Linux</option>
					<option value="windows">Windows</option>
				</select>
			</label>
			<label className="dialog-field">
              <span>Auth type</span>
				<select value={transport === 'winrm' ? 'password' : authType} onChange={(event) => handleAuthTypeChange(event.target.value)} disabled={transport === 'winrm'}>
                <option value="password">Password</option>
                <option value="private_key">Private key</option>
              </select>
            </label>
			{transport === 'winrm' ? (
				<>
					<label className="dialog-field">
						<span>WinRM scheme</span>
						<select value={winrmScheme} onChange={(event) => {
							const scheme = event.target.value as 'http' | 'https';
							setWinrmScheme(scheme);
							setWinrmPort(scheme === 'https' ? '5986' : '5985');
							if (scheme === 'http') setWinrmTLSInsecure(false);
							clearCredentialTest();
						}}>
							<option value="https">HTTPS</option>
							<option value="http">HTTP</option>
						</select>
					</label>
					<label className="dialog-field">
						<span>WinRM port</span>
						<input type="number" min="1" max="65535" value={winrmPort} onChange={(event) => { setWinrmPort(event.target.value); clearCredentialTest(); }} />
					</label>
					<label className="dialog-field wide">
						<span>Password</span>
						<input type="password" value={password} onChange={(event) => handlePasswordChange(event.target.value)} placeholder="Target password" />
						{credentialHelp ? <small>{credentialHelp}</small> : null}
					</label>
					<label className="dialog-field wide">
						<input type="checkbox" checked={winrmTLSInsecure} disabled={winrmScheme !== 'https'} onChange={(event) => { setWinrmTLSInsecure(event.target.checked); clearCredentialTest(); }} />
						<span>Allow an untrusted HTTPS certificate</span>
					</label>
				</>
			) : authType === 'private_key' ? (
              <label className="dialog-field wide private-key-upload-field">
                <span>Private key file</span>
                <div className="file-upload-control">
                  <Upload size={18} />
                  <div>
                    <strong>{privateKeyFile ? privateKeyFile.name : 'Choose key file'}</strong>
                    <small>{privateKeyFile ? 'Ready to upload' : 'No file selected'}</small>
                  </div>
                  <input className="file-upload-input" aria-label="Private key file" type="file" onChange={handlePrivateKeyFileChange} />
                </div>
                <small>{privateKeyHelp}</small>
              </label>
            ) : (
              <label className="dialog-field wide">
                <span>Password</span>
                <input type="password" value={password} onChange={(event) => handlePasswordChange(event.target.value)} placeholder="Target password" />
                {credentialHelp ? <small>{credentialHelp}</small> : null}
              </label>
            )}
            <div className="credential-test-row wide">
              <button
                className="ghost-button credential-test-button"
                type="button"
                onClick={() => {
                  void handleTestCredentials();
                }}
                disabled={!credentialReady || isTestingCredentials}
              >
                {credentialTestStatus === 'testing' || isTestingCredentials ? <Loader2 size={16} className="spin-icon" /> : <PlugZap size={16} />}
                Test credentials
              </button>
              {credentialTestStatus === 'success' ? (
                <span className="credential-test-status success">
                  <CheckCircle2 size={16} />
					{transport === 'winrm' ? 'WinRM connection verified' : 'SSH connection verified'}
                </span>
              ) : null}
              {credentialTestStatus === 'testing' || isTestingCredentials ? (
				<span className="credential-test-status testing">Testing {transport === 'winrm' ? 'WinRM' : 'SSH'} connection...</span>
              ) : null}
              {credentialTestStatus === 'error' ? (
                <span className="credential-test-status error">{credentialTestError || 'SSH credential test failed'}</span>
              ) : null}
            </div>
			{pendingHostKey ? (
				<div className="host-key-approval wide" role="alertdialog" aria-labelledby="host-key-approval-title">
					<div className="warning-title">
						<AlertTriangle size={20} />
						<strong id="host-key-approval-title">Unknown SSH host key</strong>
					</div>
					<p>Verify this fingerprint through a trusted channel before continuing.</p>
					<dl>
						<div><dt>Host</dt><dd>{pendingHostKey.host}:{pendingHostKey.port}</dd></div>
						<div><dt>Algorithm</dt><dd>{pendingHostKey.algorithm}</dd></div>
						<div><dt>SHA-256 fingerprint</dt><dd><code>{pendingHostKey.fingerprint}</code></dd></div>
					</dl>
					<div className="host-key-approval-actions">
						<button className="ghost-button" type="button" onClick={() => setPendingHostKey(null)}>Cancel</button>
						<button className="primary-action" type="button" disabled={isTestingCredentials} onClick={() => void handleTestCredentials(pendingHostKey.fingerprint)}>
							Trust and retry
						</button>
					</div>
				</div>
			) : null}
            <label className="dialog-field">
              <span>Policy Name</span>
              <select
                value={selectedPolicyID}
                onChange={(event) => handlePolicyChange(event.target.value)}
                disabled={policyOptions.length === 0}
                required
              >
                <option value="">{policyOptions.length === 0 ? 'No policies available' : 'Select policy'}</option>
                {policyOptions.map((option) => (
                  <option key={option.id} value={option.id}>
                    {option.label}
                  </option>
                ))}
              </select>
            </label>
            <fieldset className="dialog-field multi-select-field">
              <legend>Agent Name</legend>
              <details className="multi-select">
                <summary>{agentOptions.length === 0 ? 'No agents available' : selectedAgentSummary}</summary>
                <div className="multi-select-menu" role="group" aria-label="Agent Name">
                  {agentOptions.length === 0 ? (
                    <span className="multi-select-empty">No agents available</span>
                  ) : (
                    agentOptions.map((option) => (
                      <label key={option.id} className="multi-select-option">
                        <input
                          type="checkbox"
                          checked={selectedAgentIDs.includes(option.id)}
                          onChange={() => toggleAgent(option.id)}
                        />
                        <span>{option.label}</span>
                      </label>
                    ))
                  )}
                </div>
              </details>
            </fieldset>
          </div>

          {localError || error ? <div className="form-error compact">{localError || error?.message}</div> : null}

          <div className="dialog-actions">
            <button className="ghost-button" type="button" onClick={onClose}>
              Cancel
            </button>
            <button className="primary-action" type="submit" disabled={isSaving || !canSaveTargetSelections || (credentialTestRequired && !credentialTestToken)}>
              {isSaving ? 'Saving...' : 'Save Target'}
            </button>
          </div>
        </form>
      </section>
    </div>
  );
}

function DeleteAgentModal({
  agent,
  error,
  isDeleting,
  onClose,
  onConfirm
}: {
  agent: Agent;
  error: Error | null;
  isDeleting: boolean;
  onClose: () => void;
  onConfirm: () => Promise<void>;
}) {
  return (
    <div className="modal-backdrop" role="presentation">
      <section aria-modal="true" className="dialog narrow" role="dialog" aria-labelledby="delete-agent-title">
        <div className="dialog-header">
          <div className="warning-title">
            <AlertTriangle size={20} />
            <h2 id="delete-agent-title">Delete Agent</h2>
          </div>
          <button className="icon-button" type="button" aria-label="Close delete dialog" onClick={onClose}>
            <X size={18} />
          </button>
        </div>
        <p className="dialog-copy">Delete {agentLabel(agent)} from the Proxyma agent inventory.</p>
        {error ? <div className="form-error compact">{error.message}</div> : null}
        <div className="dialog-actions">
          <button className="ghost-button" type="button" onClick={onClose}>
            Cancel
          </button>
          <button
            className="danger-action"
            type="button"
            onClick={() => {
              void onConfirm().catch(() => undefined);
            }}
            disabled={isDeleting}
          >
            {isDeleting ? 'Deleting...' : 'Delete Agent'}
          </button>
        </div>
      </section>
    </div>
  );
}

function DeleteUserModal({
  error,
  isDeleting,
  onClose,
  onConfirm,
  user
}: {
  error: Error | null;
  isDeleting: boolean;
  onClose: () => void;
  onConfirm: () => Promise<void>;
  user: UserProfile;
}) {
  return (
    <div className="modal-backdrop" role="presentation">
      <section aria-modal="true" className="dialog narrow" role="dialog" aria-labelledby="delete-user-title">
        <div className="dialog-header">
          <div className="warning-title">
            <AlertTriangle size={20} />
            <h2 id="delete-user-title">Delete User</h2>
          </div>
          <button className="icon-button" type="button" aria-label="Close delete dialog" onClick={onClose}>
            <X size={18} />
          </button>
        </div>
        <p className="dialog-copy">Delete {userLabel(user)} from Proxyma users.</p>
        {error ? <div className="form-error compact">{error.message}</div> : null}
        <div className="dialog-actions">
          <button className="ghost-button" type="button" onClick={onClose}>
            Cancel
          </button>
          <button
            className="danger-action"
            type="button"
            onClick={() => {
              void onConfirm().catch(() => undefined);
            }}
            disabled={isDeleting}
          >
            {isDeleting ? 'Deleting...' : 'Delete User'}
          </button>
        </div>
      </section>
    </div>
  );
}

function DeleteInteractionModal({
  error,
  interaction,
  isDeleting,
  onClose,
  onConfirm
}: {
  error: Error | null;
  interaction: InteractionInstance;
  isDeleting: boolean;
  onClose: () => void;
  onConfirm: () => Promise<void>;
}) {
  return (
    <div className="modal-backdrop" role="presentation">
      <section aria-modal="true" className="dialog narrow" role="dialog" aria-labelledby="delete-interaction-title">
        <div className="dialog-header">
          <div>
            <h2 id="delete-interaction-title">Delete Interaction</h2>
            <p>{interactionLabel(interaction)}</p>
          </div>
          <button className="icon-button" type="button" aria-label="Close delete dialog" onClick={onClose}>
            <X size={18} />
          </button>
        </div>
        <p className="delete-copy">
          This removes the NTFY interaction configuration. Deletion is blocked while any agent still references this interaction instance.
        </p>
        {error ? <div className="form-error compact">{error.message}</div> : null}
        <div className="dialog-actions">
          <button className="ghost-button" type="button" onClick={onClose}>
            Cancel
          </button>
          <button
            className="primary-action danger-action"
            type="button"
            onClick={() => {
              void onConfirm();
            }}
            disabled={isDeleting}
          >
            {isDeleting ? 'Deleting...' : 'Delete Interaction'}
          </button>
        </div>
      </section>
    </div>
  );
}

function DeleteTargetModal({
  error,
  isDeleting,
  onClose,
  onConfirm,
  target
}: {
  error: Error | null;
  isDeleting: boolean;
  onClose: () => void;
  onConfirm: () => Promise<void>;
  target: Target;
}) {
  return (
    <div className="modal-backdrop" role="presentation">
      <section aria-modal="true" className="dialog narrow" role="dialog" aria-labelledby="delete-target-title">
        <div className="dialog-header">
          <div className="warning-title">
            <AlertTriangle size={20} />
            <h2 id="delete-target-title">Delete Target</h2>
          </div>
          <button className="icon-button" type="button" aria-label="Close delete dialog" onClick={onClose}>
            <X size={18} />
          </button>
        </div>
        <p className="dialog-copy">Delete {targetLabel(target)} from the Proxyma target inventory.</p>
        {error ? <div className="form-error compact">{error.message}</div> : null}
        <div className="dialog-actions">
          <button className="ghost-button" type="button" onClick={onClose}>
            Cancel
          </button>
          <button
            className="danger-action"
            type="button"
            onClick={() => {
              void onConfirm().catch(() => undefined);
            }}
            disabled={isDeleting}
          >
            {isDeleting ? 'Deleting...' : 'Delete Target'}
          </button>
        </div>
      </section>
    </div>
  );
}

function ResultBadge({ result }: { result: string }) {
  const normalized = result.toLowerCase();
  return <span className={`result-badge ${normalized}`}>{resultLabel(normalized)}</span>;
}

function RoleBadge({ admin }: { admin: boolean }) {
  return <span className={`result-badge ${admin ? 'success' : 'waiting'}`}>{admin ? 'Administrator' : 'User'}</span>;
}

function EmptyState({ message, tone = 'neutral' }: { message: string; tone?: 'neutral' | 'error' }) {
  return (
    <div className={`empty-state ${tone}`}>
      <ClipboardList size={20} />
      <span>{message}</span>
    </div>
  );
}

function LoadingRows() {
  return (
    <div className="loading-rows" aria-label="Loading">
      <span />
      <span />
      <span />
    </div>
  );
}

function initials(value: string) {
  return value
    .split(/\s+/)
    .filter(Boolean)
    .slice(0, 2)
    .map((part) => part[0]?.toUpperCase())
    .join('');
}

function targetLabel(target: Target) {
  return target.hostname || target.ip || target.description || target.id;
}

function userLabel(user: UserProfile) {
  return user.name || user.username || user.id;
}

function ownerLabels(userIDs: string[], users: UserProfile[]) {
  if (userIDs.length === 0) {
    return 'No owners';
  }
  const usersByID = new Map(users.map((user) => [user.id, user]));
  return userIDs
    .map((id) => {
      const user = usersByID.get(id);
      if (!user) {
        return id;
      }
      if (user.name && user.username) {
        return `${user.name} (${user.username})`;
      }
      return userLabel(user);
    })
    .join(', ');
}

function ownerCountLabel(userIDs: string[]) {
  return userIDs.length === 1 ? '1 assigned user' : `${userIDs.length} assigned users`;
}

function agentLabel(agent: Agent) {
  return agent.display_name || agent.id;
}

function interactionLabel(interaction: InteractionInstance) {
  return interaction.display_name || interaction.request_topic || interaction.id;
}

function interactionEndpoint(interaction: InteractionInstance) {
  const base = interaction.base_url || 'No endpoint';
  if (interaction.request_topic && interaction.response_topic) {
    return `${base} / ${interaction.request_topic} -> ${interaction.response_topic}`;
  }
  if (interaction.request_topic) {
    return `${base} / ${interaction.request_topic}`;
  }
  return base;
}

function interactionAgentLabels(interaction: InteractionInstance, agents: Agent[]) {
  if (!interaction.referenced_agent_ids || interaction.referenced_agent_ids.length === 0) {
    return 'No agents';
  }
  return interaction.referenced_agent_ids.map((agentID) => agentOptionLabel(agentID, agents)).join(', ');
}

function getTargetCreationPrerequisites(
  agents: Agent[],
  policies: Policy[],
  isLoading: boolean,
  hasError: boolean
): TargetCreationPrerequisites {
  if (isLoading) {
    return {
      status: 'loading',
      message: 'Checking agent and policy prerequisites…',
      missingAgent: false,
      missingPolicy: false
    };
  }
  if (hasError) {
    return {
      status: 'error',
      message: 'Unable to verify agents and policies.',
      missingAgent: false,
      missingPolicy: false
    };
  }

  const missingAgent = agents.length === 0;
  const missingPolicy = policies.length === 0;
  if (missingAgent || missingPolicy) {
    const message = missingAgent && missingPolicy
      ? 'Create an agent and a policy before adding a target.'
      : missingAgent
        ? 'Create an agent before adding a target.'
        : 'Create a policy before adding a target.';
    return { status: 'missing', message, missingAgent, missingPolicy };
  }

  return { status: 'ready', message: '', missingAgent: false, missingPolicy: false };
}

function getAgentCreationPrerequisites(
  interactions: InteractionInstance[],
  currentUserID: string,
  isLoading: boolean,
  hasError: boolean
): AgentCreationPrerequisites {
  if (showLocalInteraction) {
    return { status: 'ready', message: '' };
  }
  if (isLoading) {
    return { status: 'loading', message: 'Checking interaction prerequisites…' };
  }
  if (hasError) {
    return { status: 'error', message: 'Unable to verify interactions.' };
  }
  if (selectableInteractions(interactions, currentUserID).length === 0) {
    return { status: 'missing', message: 'Create an interaction before adding an agent.' };
  }
  return { status: 'ready', message: '' };
}

function selectableInteractions(interactions: InteractionInstance[], currentUserID: string) {
  return interactions.filter((interaction) => interaction.enabled && interaction.creator_user_id === currentUserID);
}

function policySelectOptions(policies: Policy[], selectedPolicyID: string) {
  const options = policies.map((policy) => ({ id: policy.id, label: policy.name || policy.id }));
  if (selectedPolicyID && !options.some((option) => option.id === selectedPolicyID)) {
    options.push({ id: selectedPolicyID, label: selectedPolicyID });
  }
  return options.sort((a, b) => a.label.localeCompare(b.label));
}

function agentSelectOptions(agents: Agent[], selectedAgentIDs: string[]) {
  const options = agents.map((agent) => ({ id: agent.id, label: agentLabel(agent) }));
  for (const agentID of selectedAgentIDs) {
    if (!options.some((option) => option.id === agentID)) {
      options.push({ id: agentID, label: agentID });
    }
  }
  return options.sort((a, b) => a.label.localeCompare(b.label));
}

function agentOptionLabel(agentID: string, agents: Agent[]) {
  const agent = agents.find((item) => item.id === agentID);
  return agent ? agentLabel(agent) : agentID;
}

function targetType(target: Target) {
	if (target.transport === 'winrm') {
		return 'WinRM password';
	}
  if (target.auth_type === 'private_key') {
    return 'SSH key';
  }
  if (target.auth_type === 'password') {
    return 'SSH password';
  }
  return target.auth_type || 'Target';
}

function latestCommandByAgent(commands: CommandHistoryItem[]) {
  const byAgent = new Map<string, CommandHistoryItem>();
  for (const command of commands) {
    if (command.entry_type === 'command' && command.agent_id && !byAgent.has(command.agent_id)) {
      byAgent.set(command.agent_id, command);
    }
  }
  return byAgent;
}

function linkedTarget(agent: Agent, targets: Target[]) {
  const target = targets.find((item) => item.agent_ids.includes(agent.id));
  return target ? targetLabel(target) : 'Not linked';
}

function lastSeenForTarget(target: Target, commands: CommandHistoryItem[]) {
  const label = targetLabel(target).toLowerCase();
  const matched = commands.find((command) => command.entry_type === 'command' && command.target?.toLowerCase() === label);
  return matched ? formatDate(matched.completed_at || matched.created_at) : 'No recent activity';
}

function formatDate(value: string) {
  return new Intl.DateTimeFormat(undefined, {
    month: 'short',
    day: '2-digit',
    year: 'numeric',
    hour: '2-digit',
    minute: '2-digit'
  }).format(new Date(value));
}

function resultLabel(result: string) {
  switch (result) {
    case 'success':
      return 'Success';
    case 'warning':
      return 'Warning';
    case 'failed':
      return 'Failed';
    case 'waiting':
      return 'Waiting';
    default:
      return result || 'Unknown';
  }
}

function statusLabel(status: string) {
  return status
    .split(/[_-]/)
    .filter(Boolean)
    .map((part) => part.charAt(0).toUpperCase() + part.slice(1))
    .join(' ');
}
