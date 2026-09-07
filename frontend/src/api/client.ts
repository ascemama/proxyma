import type { Theme } from '../theme';

export type UserProfile = {
  id: string;
  username: string;
  name: string;
  admin: boolean;
  status: 'active' | 'pending_activation' | 'password_reset_pending';
  theme: Theme;
  activation_url?: string;
  activation_expires_at?: string;
};

export type UserSession = {
  user: UserProfile;
  expires_at: string;
  csrf_token: string;
  build: {
    version: string;
    commit: string;
  };
};

export type UserMutationInput = {
  username?: string;
  name?: string;
  admin?: boolean;
  activation_token_lifetime_seconds?: number;
};

export type ActivationPolicy = { default_lifetime_seconds: number; maximum_lifetime_seconds: number };
export type ActivationContext = { username: string; name: string; purpose: 'activation' | 'password_reset'; expires_at: string };

export type ChangePasswordInput = {
  current_password: string;
  new_password: string;
};

export type UserSettingsInput = {
  theme: Theme;
};

export type Agent = {
  id: string;
  display_name?: string;
  enabled: boolean;
  user_ids: string[];
  interaction_instance_id?: string;
};

export type AgentMutationInput = {
  display_name?: string;
  enabled?: boolean;
  agent_jwt_signing_key?: string;
  interaction_instance_id?: string;
};

export type Target = {
	id: string;
	transport: 'ssh' | 'winrm';
	os: 'linux' | 'windows';
  hostname?: string;
  ip?: string;
  policy_ids: string[];
  agent_ids: string[];
  user_ids: string[];
  description?: string;
	auth_type?: string;
	winrm?: WinRMSettings;
};

export type WinRMSettings = {
  scheme: 'http' | 'https';
  port?: number;
  tls_insecure?: boolean;
};

export type TargetMutationInput = {
	transport: 'ssh' | 'winrm';
	os: 'linux' | 'windows';
  hostname?: string;
  ip?: string;
  description?: string;
  username?: string;
  policy_ids?: string[];
  agent_ids?: string[];
  auth_type?: string;
  password?: string;
	private_key_file?: File;
	winrm?: WinRMSettings;
	target_id?: string;
	approved_host_key_fingerprint?: string;
	credential_test_token?: string;
};

export type SSHHostKeyInfo = {
	host: string;
	port: number;
	algorithm: string;
	fingerprint: string;
};

export type InteractionInstance = {
  id: string;
  type: 'ntfy';
  creator_user_id: string;
  creator_label?: string;
  display_name?: string;
  enabled: boolean;
  base_url?: string;
  request_topic?: string;
  response_topic?: string;
  poll_interval?: string;
  poll_timeout?: string;
  responded_by?: string;
  token_configured: boolean;
  referenced_agent_ids: string[];
};

export type InteractionMutationInput = {
  type?: 'ntfy';
  creator_user_id?: string;
  display_name?: string;
  enabled?: boolean;
  base_url?: string;
  request_topic?: string;
  response_topic?: string;
  token?: string;
  poll_interval?: string;
  poll_timeout?: string;
  responded_by?: string;
};

export type PolicyRule = {
  id: string;
  effect: 'allow' | 'approval' | 'deny';
  remote_command_regex?: string;
  remote_command_prefix_any?: string[];
  always?: boolean;
  reason?: string;
};

export type Policy = {
  id: string;
  name: string;
  description?: string;
  priority: number;
  enabled: boolean;
  rules: PolicyRule[];
};

export type PolicyMutationInput = {
  name?: string;
  description?: string;
  priority?: number;
  enabled?: boolean;
  rules?: PolicyRule[];
};

export type CommandHistoryItem = {
  task_id: string;
  agent_id?: string;
  agent_name?: string;
  target?: string;
  command: string;
  status: string;
  result: string;
  created_at: string;
  completed_at?: string;
};

export type InteractionHistoryItem = {
  interaction_id: string;
  task_id: string;
  agent_id: string;
  agent_name?: string;
  interaction_instance_id: string;
  interaction_instance_creator_user_id?: string;
  provider?: string;
  kind: string;
  expected_response: string;
  status: string;
  subject: string;
  message: string;
  requested_by?: string;
  approved?: boolean;
  answer?: string;
  responded_by?: string;
  cancel_reason?: string;
  target?: string;
  target_hostname?: string;
  target_ip?: string;
  command: string;
  created_at: string;
  expires_at?: string;
  responded_at?: string;
  task_created_at: string;
};

export type InteractionHistoryQuery = {
  limit?: number;
  cursor?: string;
  agent_id?: string;
  interaction_instance_id?: string;
  interaction_instance_creator_user_id?: string;
  status?: string;
  kind?: string;
  target?: string;
  created_from?: string;
  created_to?: string;
  q?: string;
  command_pattern?: string;
  command_pattern_mode?: 'literal' | 'glob' | 'regex';
};

export type InteractionHistoryPage = {
  items: InteractionHistoryItem[];
  next_cursor?: string;
};

type ErrorResponse = {
  error?: string;
  code?: string;
};

export class APIError extends Error {
  status: number;
  code?: string;
  hostKey?: SSHHostKeyInfo;

  constructor(message: string, status: number, code?: string, hostKey?: SSHHostKeyInfo) {
    super(message);
    this.name = 'APIError';
    this.status = status;
    this.code = code;
    this.hostKey = hostKey;
  }
}

async function requestJSON<T>(path: string, options: RequestInit = {}): Promise<T> {
  const headers =
    options.body instanceof FormData
      ? options.headers
      : {
          'Content-Type': 'application/json',
          ...options.headers
        };
  const response = await fetch(path, {
    ...options,
    credentials: 'include',
    headers
  });

  if (!response.ok) {
    let message = `Request failed with ${response.status}`;
    let code: string | undefined;
    let hostKey: SSHHostKeyInfo | undefined;
    try {
      const payload = (await response.json()) as ErrorResponse & { host_key?: SSHHostKeyInfo };
      if (payload.error) {
        message = payload.error;
      }
	  code = payload.code;
	  hostKey = payload.host_key;
    } catch {
      // Keep the generic HTTP status message.
    }
    throw new APIError(message, response.status, code, hostKey);
  }

  return (await response.json()) as T;
}

export function login(username: string, password: string): Promise<UserSession> {
  return requestJSON<UserSession>('/v1/user/login', {
    method: 'POST',
    body: JSON.stringify({ username, password })
  });
}

export function getSession(): Promise<UserSession> {
  return requestJSON<UserSession>('/v1/user/session');
}

export function logout(csrfToken: string): Promise<{ status: string }> {
  return requestJSON<{ status: string }>('/v1/user/logout', {
    method: 'POST',
    headers: {
      'X-CSRF-Token': csrfToken
    }
  });
}

export function changePassword(input: ChangePasswordInput, csrfToken: string): Promise<{ status: string }> {
  return requestJSON<{ status: string }>('/v1/user/change-password', {
    method: 'POST',
    headers: {
      'X-CSRF-Token': csrfToken
    },
    body: JSON.stringify(input)
  });
}

export function getActivation(token: string): Promise<ActivationContext> {
  return requestJSON<ActivationContext>('/v1/user/activation', { headers: { Authorization: `Activation ${token}` } });
}

export function completeActivation(token: string, newPassword: string): Promise<{ status: string }> {
  return requestJSON<{ status: string }>('/v1/user/activation', {
    method: 'POST', headers: { Authorization: `Activation ${token}` }, body: JSON.stringify({ new_password: newPassword })
  });
}

export function getActivationPolicy(csrfToken: string): Promise<ActivationPolicy> {
  return requestJSON<ActivationPolicy>('/v1/user/activation-policy', { headers: { 'X-CSRF-Token': csrfToken } });
}

export function regenerateActivationLink(id: string, lifetimeSeconds: number | undefined, csrfToken: string): Promise<{ activation_url: string; activation_expires_at: string }> {
  return requestJSON(`/v1/user/users/${encodeURIComponent(id)}/activation-link`, {
    method: 'POST', headers: { 'X-CSRF-Token': csrfToken },
    body: JSON.stringify(lifetimeSeconds ? { activation_token_lifetime_seconds: lifetimeSeconds } : {})
  });
}

export function createPasswordResetLink(id: string, lifetimeSeconds: number | undefined, csrfToken: string): Promise<{ activation_url: string; activation_expires_at: string }> {
  return requestJSON(`/v1/user/users/${encodeURIComponent(id)}/password-reset-link`, {
    method: 'POST', headers: { 'X-CSRF-Token': csrfToken },
    body: JSON.stringify(lifetimeSeconds ? { activation_token_lifetime_seconds: lifetimeSeconds } : {})
  });
}

export function updateUserSettings(input: UserSettingsInput, csrfToken: string): Promise<UserProfile> {
  return requestJSON<UserProfile>('/v1/user/settings', {
    method: 'PUT',
    headers: {
      'X-CSRF-Token': csrfToken
    },
    body: JSON.stringify(input)
  });
}

export async function getAgents(): Promise<Agent[]> {
  const response = await requestJSON<{ agents: Agent[] }>('/v1/user/agents');
  return response.agents;
}

export async function getUsers(): Promise<UserProfile[]> {
  const response = await requestJSON<{ users: UserProfile[] }>('/v1/user/users');
  return response.users;
}

export function createUser(input: UserMutationInput, csrfToken: string): Promise<UserProfile> {
  return requestJSON<UserProfile>('/v1/user/users', {
    method: 'POST',
    headers: {
      'X-CSRF-Token': csrfToken
    },
    body: JSON.stringify(input)
  });
}

export function updateUser(id: string, input: UserMutationInput, csrfToken: string): Promise<UserProfile> {
  return requestJSON<UserProfile>(`/v1/user/users/${encodeURIComponent(id)}`, {
    method: 'PUT',
    headers: {
      'X-CSRF-Token': csrfToken
    },
    body: JSON.stringify(input)
  });
}

export function deleteUser(id: string, csrfToken: string): Promise<{ status: string }> {
  return requestJSON<{ status: string }>(`/v1/user/users/${encodeURIComponent(id)}`, {
    method: 'DELETE',
    headers: {
      'X-CSRF-Token': csrfToken
    }
  });
}

export function createAgent(input: AgentMutationInput, csrfToken: string): Promise<Agent> {
  return requestJSON<Agent>('/v1/user/agents', {
    method: 'POST',
    headers: {
      'X-CSRF-Token': csrfToken
    },
    body: JSON.stringify(input)
  });
}

export function updateAgent(id: string, input: AgentMutationInput, csrfToken: string): Promise<Agent> {
  return requestJSON<Agent>(`/v1/user/agents/${encodeURIComponent(id)}`, {
    method: 'PUT',
    headers: {
      'X-CSRF-Token': csrfToken
    },
    body: JSON.stringify(input)
  });
}

export function deleteAgent(id: string, csrfToken: string): Promise<{ status: string }> {
  return requestJSON<{ status: string }>(`/v1/user/agents/${encodeURIComponent(id)}`, {
    method: 'DELETE',
    headers: {
      'X-CSRF-Token': csrfToken
    }
  });
}

export async function getTargets(): Promise<Target[]> {
  const response = await requestJSON<{ targets: Target[] }>('/v1/user/targets');
  return response.targets;
}

export async function getInteractions(): Promise<InteractionInstance[]> {
  const response = await requestJSON<{ interactions: InteractionInstance[] }>('/v1/user/interactions');
  return response.interactions;
}

export function createInteraction(input: InteractionMutationInput, csrfToken: string): Promise<InteractionInstance> {
  return requestJSON<InteractionInstance>('/v1/user/interactions', {
    method: 'POST',
    headers: {
      'X-CSRF-Token': csrfToken
    },
    body: JSON.stringify(input)
  });
}

export function updateInteraction(id: string, input: InteractionMutationInput, csrfToken: string): Promise<InteractionInstance> {
  return requestJSON<InteractionInstance>(`/v1/user/interactions/${encodeURIComponent(id)}`, {
    method: 'PUT',
    headers: {
      'X-CSRF-Token': csrfToken
    },
    body: JSON.stringify(input)
  });
}

export function deleteInteraction(id: string, csrfToken: string): Promise<{ status: string }> {
  return requestJSON<{ status: string }>(`/v1/user/interactions/${encodeURIComponent(id)}`, {
    method: 'DELETE',
    headers: {
      'X-CSRF-Token': csrfToken
    }
  });
}

export function createTarget(input: TargetMutationInput, csrfToken: string): Promise<Target> {
  const { body, headers } = targetMutationRequest(input, csrfToken);
  return requestJSON<Target>('/v1/user/targets', {
    method: 'POST',
    headers,
    body
  });
}

export function updateTarget(id: string, input: TargetMutationInput, csrfToken: string): Promise<Target> {
  const { body, headers } = targetMutationRequest(input, csrfToken);
  return requestJSON<Target>(`/v1/user/targets/${encodeURIComponent(id)}`, {
    method: 'PUT',
    headers,
    body
  });
}

export type TargetCredentialTestInput = Pick<TargetMutationInput, 'transport' | 'os' | 'hostname' | 'ip' | 'username' | 'auth_type' | 'password' | 'private_key_file' | 'winrm' | 'target_id' | 'approved_host_key_fingerprint'>;

export type TargetCredentialTestResponse = {
  status: string;
  message?: string;
	credential_test_token: string;
};

export function testTargetCredentials(
  input: TargetCredentialTestInput,
  csrfToken: string
): Promise<TargetCredentialTestResponse> {
  const { body, headers } = targetMutationRequest(input, csrfToken);
  return requestJSON<TargetCredentialTestResponse>('/v1/user/targets/test-credentials', {
    method: 'POST',
    headers,
    body
  });
}

function targetMutationRequest(input: TargetMutationInput, csrfToken: string): { body: BodyInit; headers: HeadersInit } {
  if (!input.private_key_file) {
    return {
      body: JSON.stringify(input),
      headers: {
        'X-CSRF-Token': csrfToken
      }
    };
  }
  const form = new FormData();
	appendFormValue(form, 'hostname', input.hostname);
	appendFormValue(form, 'transport', input.transport);
	appendFormValue(form, 'os', input.os);
  appendFormValue(form, 'ip', input.ip);
  appendFormValue(form, 'description', input.description);
  appendFormValue(form, 'username', input.username);
  appendFormValue(form, 'auth_type', input.auth_type);
	appendFormValue(form, 'password', input.password);
	appendFormValue(form, 'target_id', input.target_id);
	appendFormValue(form, 'approved_host_key_fingerprint', input.approved_host_key_fingerprint);
	appendFormValue(form, 'credential_test_token', input.credential_test_token);
  for (const policyID of input.policy_ids ?? []) {
    form.append('policy_ids', policyID);
  }
  for (const agentID of input.agent_ids ?? []) {
    form.append('agent_ids', agentID);
  }
  form.append('private_key_file', input.private_key_file);
  return {
    body: form,
    headers: {
      'X-CSRF-Token': csrfToken
    }
  };
}

function appendFormValue(form: FormData, key: string, value: string | undefined) {
  if (value !== undefined) {
    form.append(key, value);
  }
}

export function deleteTarget(id: string, csrfToken: string): Promise<{ status: string }> {
  return requestJSON<{ status: string }>(`/v1/user/targets/${encodeURIComponent(id)}`, {
    method: 'DELETE',
    headers: {
      'X-CSRF-Token': csrfToken
    }
  });
}

export async function getPolicies(): Promise<Policy[]> {
  const response = await requestJSON<{ policies: Policy[] }>('/v1/user/policies');
  return response.policies;
}

export function createPolicy(input: PolicyMutationInput, csrfToken: string): Promise<Policy> {
  return requestJSON<Policy>('/v1/user/policies', {
    method: 'POST',
    headers: {
      'X-CSRF-Token': csrfToken
    },
    body: JSON.stringify(input)
  });
}

export function deletePolicy(id: string, csrfToken: string): Promise<{ status: string }> {
  return requestJSON<{ status: string }>(`/v1/user/policies/${encodeURIComponent(id)}`, {
    method: 'DELETE',
    headers: {
      'X-CSRF-Token': csrfToken
    }
  });
}

export async function getCommandHistory(limit = 20): Promise<CommandHistoryItem[]> {
  const response = await requestJSON<{ commands: CommandHistoryItem[] }>(`/v1/user/command-history?limit=${limit}`);
  return response.commands;
}

export async function getInteractionHistory(query: InteractionHistoryQuery = {}): Promise<InteractionHistoryPage> {
  const params = new URLSearchParams();
  for (const [key, value] of Object.entries(query)) {
    if (value !== undefined && value !== '') {
      params.set(key, String(value));
    }
  }
  const suffix = params.toString() ? `?${params.toString()}` : '';
  return requestJSON<InteractionHistoryPage>(`/v1/user/interaction-history${suffix}`);
}
