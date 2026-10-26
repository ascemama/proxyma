import { expect, test, type Page } from '@playwright/test';

const userID = 'fcd2f3a9-d546-44f4-837b-0c7aa4219752';
const otherUserID = 'a49e7f6c-aa6f-40cd-ac95-e2c876db5bf5';
const interactionID = '6a5b2d49-8c61-4f2e-9f1a-7f47ddbd71a8';

type MockInteraction = {
  id: string;
  type: string;
  creator_user_id: string;
  display_name: string;
  enabled: boolean;
  token_configured: boolean;
  referenced_agent_ids: string[];
};

type ProductionAPIState = {
  interactions: MockInteraction[];
  submittedAgent: Record<string, unknown> | null;
  failInteractions?: boolean;
  waitForInteractions?: Promise<void>;
};

test('production GUI hides local interaction and requires a configured interaction', async ({ page }) => {
  const state = productionAPIState([configuredInteraction()]);
  await installProductionAPIMocks(page, state);
  await login(page);

  await page.getByRole('button', { name: 'Agents', exact: true }).click();
  await page.getByRole('button', { name: 'Add Agent' }).click();
  const dialog = page.getByRole('dialog', { name: 'Add Agent' });
  const interactionSelect = dialog.getByLabel('Interaction');

  await expect(interactionSelect).toHaveValue('');
  await expect(interactionSelect.locator('option[value="local"]')).toHaveCount(0);
  await expect(dialog.getByText(/Choose Local|Local console/)).toHaveCount(0);

  await dialog.getByLabel('Display name').fill('Production Agent');
  await dialog.getByLabel('Agent JWT Signing Key', { exact: true }).fill('production-agent-signing-key');
  await dialog.getByRole('button', { name: 'Save Agent' }).click();
  await expect(dialog.getByText('Select an enabled interaction before saving the agent.')).toBeVisible();
  expect(state.submittedAgent).toBeNull();

  await interactionSelect.selectOption(interactionID);
  await dialog.getByRole('button', { name: 'Save Agent' }).click();
  await expect(page.getByRole('dialog', { name: 'Agent created' })).toBeVisible();
  expect(state.submittedAgent).toMatchObject({ interaction_instance_id: interactionID });

  await page.getByRole('dialog', { name: 'Agent created' }).getByRole('button', { name: 'Done' }).click();
  await page.getByRole('button', { name: 'History', exact: true }).click();
  await expect(page.getByLabel('Provider instance').locator('option[value="local"]')).toHaveCount(0);
});

test('production requires an interaction at every Add Agent entry point and enables after creation', async ({ page }) => {
  const state = productionAPIState([]);
  await installProductionAPIMocks(page, state);
  await login(page);

  let agentsPanel = panel(page, 'Agents');
  await expect(agentsPanel.getByRole('button', { name: /^Add Agent unavailable:/ })).toBeDisabled();
  await expect(agentsPanel.getByText('Create an interaction before adding an agent.', { exact: true })).toBeVisible();

  const targetsPanel = panel(page, 'Targets');
  await expect(targetsPanel.getByRole('button', { name: /^Add Agent unavailable:/ })).toBeDisabled();
  await expect(targetsPanel.getByRole('button', { name: 'Add Interaction', exact: true })).toBeVisible();

  await page.getByRole('button', { name: 'Agents', exact: true }).click();
  agentsPanel = panel(page, 'Agents');
  await expect(agentsPanel.getByRole('button', { name: /^Add Agent unavailable:/ })).toBeDisabled();
  await agentsPanel.getByRole('button', { name: 'Add Interaction', exact: true }).click();

  const typeDialog = page.getByRole('dialog', { name: 'Add Interaction' });
  await expect(typeDialog.getByText('Local', { exact: true })).toHaveCount(0);
  await typeDialog.getByRole('button', { name: /NTFY/ }).click();
  const interactionDialog = page.getByRole('dialog', { name: 'Add NTFY Interaction' });
  await interactionDialog.getByLabel('Display Name').fill('First production interaction');
  await interactionDialog.getByLabel('Request Topic').fill('production-requests');
  await interactionDialog.getByLabel('Response Topic').fill('production-responses');
  await interactionDialog.getByRole('button', { name: 'Save Interaction' }).click();
  await expect(interactionDialog).toBeHidden();

  await expect(agentsPanel.getByRole('button', { name: 'Add Agent', exact: true })).toBeEnabled();
  await agentsPanel.getByRole('button', { name: 'Add Agent', exact: true }).click();
  await expect(page.getByRole('dialog', { name: 'Add Agent' })).toBeVisible();
});

test('production ignores disabled and foreign interactions for Agent prerequisites', async ({ page }) => {
  const state = productionAPIState([
    { ...configuredInteraction(), enabled: false },
    { ...configuredInteraction(), id: '7937dd2d-0b8a-4880-8afa-0ea60fd65a43', creator_user_id: otherUserID }
  ]);
  await installProductionAPIMocks(page, state);
  await login(page);

  const agentsPanel = panel(page, 'Agents');
  await expect(agentsPanel.getByRole('button', { name: /^Add Agent unavailable:/ })).toBeDisabled();
  await expect(agentsPanel.getByText('Create an interaction before adding an agent.', { exact: true })).toBeVisible();
});

test('production disables Agent creation while interactions load and supports retry after failure', async ({ page }) => {
  let releaseInteractions: (() => void) | undefined;
  const state = productionAPIState([configuredInteraction()]);
  state.waitForInteractions = new Promise<void>((resolve) => {
    releaseInteractions = resolve;
  });
  state.failInteractions = true;
  await installProductionAPIMocks(page, state);
  await login(page);

  const agentsPanel = panel(page, 'Agents');
  await expect(agentsPanel.getByText('Checking interaction prerequisites…', { exact: true })).toBeVisible();
  await expect(agentsPanel.getByRole('button', { name: /^Add Agent unavailable:/ })).toBeDisabled();

  releaseInteractions?.();
  await expect(agentsPanel.getByText('Unable to verify interactions.', { exact: true })).toBeVisible();
  state.failInteractions = false;
  state.waitForInteractions = undefined;
  await agentsPanel.getByRole('button', { name: 'Retry', exact: true }).click();
  await expect(agentsPanel.getByRole('button', { name: 'Add Agent', exact: true })).toBeEnabled();
});

function productionAPIState(interactions: MockInteraction[]): ProductionAPIState {
  return { interactions, submittedAgent: null };
}

async function installProductionAPIMocks(page: Page, state: ProductionAPIState) {
  await page.route('**/v1/user/**', async (route) => {
    const request = route.request();
    const path = new URL(request.url()).pathname;
    const method = request.method();

    if (path === '/v1/user/login' && method === 'POST') {
      await route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify(sessionResponse()) });
      return;
    }
    if (path === '/v1/user/agents' && method === 'POST') {
      state.submittedAgent = request.postDataJSON() as Record<string, unknown>;
      await route.fulfill({
        status: 201,
        contentType: 'application/json',
        body: JSON.stringify({
          id: 'cf3d244b-8f4a-4d56-900a-a11897d0a371',
          display_name: state.submittedAgent.display_name,
          enabled: state.submittedAgent.enabled,
          user_ids: [userID],
          interaction_instance_id: state.submittedAgent.interaction_instance_id
        })
      });
      return;
    }
    if (path === '/v1/user/interactions' && method === 'POST') {
      const input = request.postDataJSON() as Record<string, unknown>;
      const created = configuredInteraction({
        display_name: String(input.display_name ?? ''),
        creator_user_id: String(input.creator_user_id ?? userID)
      });
      state.interactions = [...state.interactions, created];
      await route.fulfill({ status: 201, contentType: 'application/json', body: JSON.stringify(created) });
      return;
    }
    if (path === '/v1/user/interactions' && method === 'GET') {
      await state.waitForInteractions;
      if (state.failInteractions) {
        await route.fulfill({ status: 500, contentType: 'application/json', body: JSON.stringify({ error: 'interaction fixture failure' }) });
      } else {
        await route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ interactions: state.interactions }) });
      }
      return;
    }

    const body = responseFor(path);
    await route.fulfill({
      status: body === null ? 404 : 200,
      contentType: 'application/json',
      body: JSON.stringify(body ?? { error: 'not found' })
    });
  });
}

async function login(page: Page) {
  await page.goto('/');
  await page.getByLabel('Username').fill('demo-admin');
  await page.getByLabel('Password', { exact: true }).fill('correct-password');
  await page.getByRole('button', { name: 'Sign In' }).click();
}

function panel(page: Page, heading: string) {
  return page.locator('section.panel').filter({ has: page.getByRole('heading', { name: heading, exact: true }) });
}

function configuredInteraction(overrides: Partial<MockInteraction> = {}): MockInteraction {
  return {
    id: interactionID,
    type: 'ntfy',
    creator_user_id: userID,
    display_name: 'Production ntfy',
    enabled: true,
    token_configured: true,
    referenced_agent_ids: [],
    ...overrides
  };
}

function sessionResponse() {
  return {
    user: {
      id: userID,
      username: 'demo-admin',
      name: 'Demo Admin',
      admin: true,
      status: 'active',
      theme: 'light'
    },
    expires_at: '2030-01-01T00:00:00Z',
    csrf_token: 'production-csrf-token',
    build: {
      version: '1.0.0',
      commit: 'abcdef0'
    }
  };
}

function responseFor(path: string) {
  switch (path) {
    case '/v1/user/agents':
      return { agents: [] };
    case '/v1/user/targets':
      return { targets: [] };
    case '/v1/user/users':
      return { users: [sessionResponse().user] };
    case '/v1/user/policies':
      return { policies: [] };
    case '/v1/user/activation-policy':
      return { default_lifetime_seconds: 43200, maximum_lifetime_seconds: 172800 };
    case '/v1/user/history':
      return { items: [] };
    default:
      return null;
  }
}
