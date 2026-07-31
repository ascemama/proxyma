import { expect, test } from '@playwright/test';

test('user can log in and load the dashboard', async ({ page }) => {
  const consoleErrors: string[] = [];
  let commandHistoryRequests = 0;
  page.on('console', (message) => {
    if (message.type() === 'error') {
      consoleErrors.push(message.text());
    }
  });
  page.on('request', (request) => {
    if (request.url().endsWith('/v1/user/command-history?limit=500')) {
      commandHistoryRequests++;
    }
  });

  await page.goto('/');
  await expect(page.getByRole('heading', { name: 'Proxyma' })).toBeVisible();
  await expect(page.getByText('Agent Proxy Monitoring')).toBeVisible();
  await expect(page.getByText(/AGPL|No warranty|Version/)).toHaveCount(0);
  await expect(page.getByRole('link', { name: /License|Source/ })).toHaveCount(0);

  await page.getByLabel('Username').fill('antoine');
  await page.getByLabel('Password', { exact: true }).fill('correct-password');
  await page.getByRole('button', { name: 'Sign In' }).click();

  await expect(page.getByRole('heading', { name: 'Dashboard' })).toBeVisible();
  await expect(page.getByText('AGPL-3.0', { exact: true })).toBeVisible();
  const sourceLink = page.getByRole('link', { name: 'Source', exact: true });
  await expect(sourceLink).toHaveAttribute('href', 'https://github.com/ascemama/proxyma');
  await expect(page.getByRole('link', { name: 'License' })).toHaveCount(0);
  await expect(page.getByText('No warranty')).toHaveCount(0);
  await expect(page.getByText(/^Version (?:\d+\.\d+\.\d+(?:-[0-9A-Za-z.-]+)?(?:\+\d+\.g[0-9a-f]{7}(?:\.dirty)?)?|dev(?:\+g[0-9a-f]{7}(?:\.dirty)?)?)$/)).toBeVisible();
  await page.getByRole('button', { name: 'Collapse', exact: true }).click();
  await expect(sourceLink).toBeHidden();
  await expect(page.getByText(/^Version /)).toBeHidden();
  await page.getByRole('button', { name: 'Toggle navigation' }).click();
  await expect(sourceLink).toBeVisible();
  await expect(page.getByRole('heading', { name: 'Recent Command History' })).toBeVisible();
  await expect(page.getByRole('heading', { name: 'Targets' })).toBeVisible();
  await expect(page.getByRole('heading', { name: 'Agents' })).toBeVisible();
  const recentCommandHistory = page.locator('section.panel').filter({ has: page.getByRole('heading', { name: 'Recent Command History' }) });
  const refreshCommandHistory = recentCommandHistory.getByRole('button', { name: 'Refresh' });
  await expect(refreshCommandHistory).toBeVisible();
  await expect.poll(() => commandHistoryRequests).toBe(1);
  await refreshCommandHistory.click();
  await expect.poll(() => commandHistoryRequests).toBe(2);
  await expect(page.getByRole('row', { name: /Target Name Type Owners Endpoint Last Seen Actions/ })).toBeVisible();
  await expect(page.getByRole('row', { name: /SSH (password|key)/ }).first()).toBeVisible();
  await expect(page.getByRole('row', { name: /Agent Name Owners Linked Target Latest Command Command Result Last Run Actions/ })).toBeVisible();
  await expect(page.getByRole('button', { name: 'Target', exact: true })).toBeEnabled();
  await expect(page.getByRole('button', { name: 'Agent', exact: true })).toBeEnabled();
  await expect(page.getByRole('button', { name: 'Users', exact: true })).toBeEnabled();
  await expect(page.getByRole('button', { name: 'Policy', exact: true })).toBeEnabled();
  await expect(page.getByRole('button', { name: 'Command History', exact: true })).toBeEnabled();
  await expect(page.getByRole('button', { name: 'Interactions', exact: true })).toBeEnabled();

  await page.getByRole('button', { name: 'Interactions', exact: true }).click();
  await expect(page.getByRole('heading', { level: 1, name: 'Interactions' })).toBeVisible();
  await expect(page.getByRole('row', { name: /Name Creator Type Status Endpoint Agents Actions/ })).toBeVisible();
  await expect(page.getByRole('row', { name: /Example ntfy.*NTFY.*Enabled/ })).toBeVisible();
  await expect(page.getByRole('button', { name: 'Logs', exact: true })).toBeDisabled();

  await page.getByRole('button', { name: 'Interaction History', exact: true }).click();
  const providerInstanceFilter = page.getByLabel('Provider instance');
  await expect(providerInstanceFilter.locator('option[value="local"]')).toHaveCount(1);

  expect(consoleErrors).toEqual([]);
});

test('user can change their password from settings', async ({ page }) => {
  await page.goto('/');
  await page.getByLabel('Username').fill('antoine');
  await page.getByLabel('Password', { exact: true }).fill('correct-password');
  await page.getByRole('button', { name: 'Sign In' }).click();

  await page.getByRole('button', { name: 'Open settings for Antoine' }).click();
  const settings = page.getByRole('dialog', { name: 'Settings' });
  await expect(settings).toBeVisible();
  await expect(settings.getByRole('button', { name: 'Appearance', exact: true })).toBeEnabled();

  let passwordChangeRequests = 0;
  page.on('request', (request) => {
    if (request.url().endsWith('/v1/user/change-password')) {
      passwordChangeRequests++;
    }
  });
  await settings.getByLabel('Current password', { exact: true }).fill('correct-password');
  await settings.getByLabel('New password', { exact: true }).fill('new-password');
  await settings.getByLabel('Confirm new password', { exact: true }).fill('different-password');

  await expect(settings.getByLabel('Current password', { exact: true })).toHaveAttribute('type', 'password');
  await expect(settings.getByLabel('New password', { exact: true })).toHaveAttribute('type', 'password');
  await expect(settings.getByLabel('Confirm new password', { exact: true })).toHaveAttribute('type', 'password');
  await settings.getByRole('button', { name: 'Show current password' }).click();
  await expect(settings.getByLabel('Current password', { exact: true })).toHaveAttribute('type', 'text');
  await expect(settings.getByLabel('New password', { exact: true })).toHaveAttribute('type', 'password');
  await settings.getByRole('button', { name: 'Hide current password' }).click();
  await expect(settings.getByLabel('Current password', { exact: true })).toHaveAttribute('type', 'password');
  await settings.getByRole('button', { name: 'Show new password' }).click();
  await expect(settings.getByLabel('New password', { exact: true })).toHaveAttribute('type', 'text');
  await expect(settings.getByLabel('Confirm new password', { exact: true })).toHaveAttribute('type', 'password');
  await settings.getByRole('button', { name: 'Hide new password' }).click();
  await expect(settings.getByLabel('New password', { exact: true })).toHaveAttribute('type', 'password');
  await settings.getByRole('button', { name: 'Show confirm new password' }).click();
  await expect(settings.getByLabel('Confirm new password', { exact: true })).toHaveAttribute('type', 'text');
  await settings.getByRole('button', { name: 'Hide confirm new password' }).click();
  await expect(settings.getByLabel('Confirm new password', { exact: true })).toHaveAttribute('type', 'password');
  expect(passwordChangeRequests).toBe(0);

  await settings.getByRole('button', { name: 'Save password' }).click();
  await expect(settings.getByText('New passwords must match.')).toBeVisible();
  expect(passwordChangeRequests).toBe(0);

  await settings.getByLabel('Current password', { exact: true }).fill('wrong-password');
  await settings.getByLabel('Confirm new password', { exact: true }).fill('new-password');
  await settings.getByRole('button', { name: 'Save password' }).click();
  await expect(settings.getByText('current password is incorrect')).toBeVisible();
  expect(passwordChangeRequests).toBe(1);

  await settings.getByLabel('Current password', { exact: true }).fill('correct-password');
  await settings.getByRole('button', { name: 'Save password' }).click();
  await expect(settings.getByText('Password changed successfully.')).toBeVisible();
  await settings.getByRole('button', { name: 'Cancel' }).click();
  await expect(page.getByRole('heading', { name: 'Dashboard' })).toBeVisible();

  await page.getByRole('button', { name: 'Logout' }).click();
  await page.getByLabel('Username').fill('antoine');
  await page.getByLabel('Password', { exact: true }).fill('correct-password');
  await page.getByRole('button', { name: 'Sign In' }).click();
  await expect(page.getByText('invalid username or password')).toBeVisible();
  await page.getByLabel('Password', { exact: true }).fill('new-password');
  await page.getByRole('button', { name: 'Sign In' }).click();
  await expect(page.getByRole('heading', { name: 'Dashboard' })).toBeVisible();

  await page.getByRole('button', { name: 'Open settings for Antoine' }).click();
  const restoreSettings = page.getByRole('dialog', { name: 'Settings' });
  await restoreSettings.getByLabel('Current password', { exact: true }).fill('new-password');
  await restoreSettings.getByLabel('New password', { exact: true }).fill('correct-password');
  await restoreSettings.getByLabel('Confirm new password', { exact: true }).fill('correct-password');
  await restoreSettings.getByRole('button', { name: 'Save password' }).click();
  await expect(restoreSettings.getByText('Password changed successfully.')).toBeVisible();
});

test('user can preview persist and restore their appearance theme', async ({ page }) => {
  await page.goto('/');
  await expect(page.locator('html')).toHaveAttribute('data-theme', 'light');
  await page.getByLabel('Username').fill('operator');
  await page.getByLabel('Password', { exact: true }).fill('correct-password');
  await page.getByRole('button', { name: 'Sign In' }).click();
  await expect(page.getByRole('heading', { name: 'Dashboard' })).toBeVisible();

  await page.getByRole('button', { name: 'Open settings for Operator' }).click();
  const settings = page.getByRole('dialog', { name: 'Settings' });
  await settings.getByRole('button', { name: 'Appearance', exact: true }).click();
  await expect(settings.getByRole('radio', { name: /Current UI/ })).toBeChecked();
  await settings.getByRole('radio', { name: /Dark/ }).check();
  await expect(page.locator('html')).toHaveAttribute('data-theme', 'dark');
  await settings.getByRole('button', { name: 'Cancel' }).click();
  await expect(settings).toBeHidden();
  await expect(page.locator('html')).toHaveAttribute('data-theme', 'light');

  await page.getByRole('button', { name: 'Open settings for Operator' }).click();
  const saveSettings = page.getByRole('dialog', { name: 'Settings' });
  await saveSettings.getByRole('button', { name: 'Appearance', exact: true }).click();
  await saveSettings.getByRole('radio', { name: /Dark/ }).check();
  await saveSettings.getByRole('button', { name: 'Save appearance' }).click();
  await expect(saveSettings.getByText('Appearance saved successfully.')).toBeVisible();
  await expect(page.locator('html')).toHaveAttribute('data-theme', 'dark');
  await expect.poll(() => page.evaluate(() => window.sessionStorage.getItem('proxyma_authenticated_theme'))).toBe('dark');
  await saveSettings.getByRole('button', { name: 'Close settings' }).click();

  await page.reload();
  await expect(page.locator('html')).toHaveAttribute('data-theme', 'dark');
  await expect(page.getByRole('heading', { name: 'Dashboard' })).toBeVisible();
  await page.getByRole('button', { name: 'Logout' }).click();
  await expect(page.getByRole('heading', { name: 'Proxyma' })).toBeVisible();
  await expect(page.locator('html')).toHaveAttribute('data-theme', 'light');

  await page.getByLabel('Username').fill('operator');
  await page.getByLabel('Password', { exact: true }).fill('correct-password');
  await page.getByRole('button', { name: 'Sign In' }).click();
  await expect(page.locator('html')).toHaveAttribute('data-theme', 'dark');
  await page.getByRole('button', { name: 'Open settings for Operator' }).click();
  const restoreSettings = page.getByRole('dialog', { name: 'Settings' });
  await restoreSettings.getByRole('button', { name: 'Appearance', exact: true }).click();
  await restoreSettings.getByRole('radio', { name: /Current UI/ }).check();
  await restoreSettings.getByRole('button', { name: 'Save appearance' }).click();
  await expect(restoreSettings.getByText('Appearance saved successfully.')).toBeVisible();
  await expect(page.locator('html')).toHaveAttribute('data-theme', 'light');
});

test('admin user sees all agents targets command history and owners', async ({ page }) => {
  const consoleErrors: string[] = [];
  page.on('console', (message) => {
    if (message.type() === 'error') {
      consoleErrors.push(message.text());
    }
  });

  await page.goto('/');
  await page.getByLabel('Username').fill('antoine');
  await page.getByLabel('Password', { exact: true }).fill('correct-password');
  await page.getByRole('button', { name: 'Sign In' }).click();

  await page.getByRole('button', { name: 'Target', exact: true }).click();
  await expect(page.getByRole('row', { name: /Target Name Type Owners Endpoint Last Seen Actions/ })).toBeVisible();
  await expect(page.getByRole('row', { name: /WEB-SRV-01.*Antoine \(antoine\)/ })).toBeVisible();
  await expect(page.getByRole('row', { name: /OP-EDGE-01.*Operator \(operator\)/ })).toBeVisible();
  await expect(page.getByRole('row', { name: /OP-WORKER-01.*Operator \(operator\)/ })).toBeVisible();

  await page.getByRole('button', { name: 'Agent', exact: true }).click();
  await expect(page.getByRole('row', { name: /Agent Name Owners Linked Target Latest Command Command Result Last Run Actions/ })).toBeVisible();
  await expect(page.getByRole('row', { name: /agent-web-01.*Antoine \(antoine\)/ })).toBeVisible();
  await expect(page.getByRole('row', { name: /operator-edge-agent.*Operator \(operator\)/ })).toBeVisible();
  await expect(page.getByRole('row', { name: /operator-worker-agent.*Operator \(operator\)/ })).toBeVisible();

  await page.getByRole('button', { name: 'Command History', exact: true }).click();
  await expect(page.getByRole('row', { name: /agent-web-01 WEB-SRV-01 antoine-private-audit Completed Success/ })).toBeVisible();
  await expect(page.getByRole('row', { name: /operator-edge-agent OP-EDGE-01 operator-edge-audit Completed Success/ })).toBeVisible();
  await expect(page.getByRole('row', { name: /operator-worker-agent OP-WORKER-01 operator-worker-audit Completed Warning/ })).toBeVisible();

  expect(consoleErrors).toEqual([]);
});

test('user can open the full command history tab', async ({ page }) => {
  const consoleErrors: string[] = [];
  page.on('console', (message) => {
    if (message.type() === 'error') {
      consoleErrors.push(message.text());
    }
  });

  await page.goto('/');
  await page.getByLabel('Username').fill('antoine');
  await page.getByLabel('Password', { exact: true }).fill('correct-password');
  await page.getByRole('button', { name: 'Sign In' }).click();

  await page.getByRole('button', { name: 'Command History', exact: true }).click();
  await expect(page.getByRole('heading', { level: 1, name: 'Command History', exact: true })).toBeVisible();
  await expect(page.getByRole('heading', { level: 2, name: 'Command History', exact: true })).toBeVisible();
  await expect(page.getByRole('row', { name: /Agent Target Command Status Result Timestamp/ })).toBeVisible();
  await expect(page.getByRole('row', { name: /agent-web-01 WEB-SRV-01 antoine-private-audit Completed Success/ })).toBeVisible();

  await page.getByRole('button', { name: 'Dashboard', exact: true }).click();
  await page.getByRole('button', { name: /View all history/ }).click();
  await expect(page.getByRole('heading', { level: 1, name: 'Command History', exact: true })).toBeVisible();

  expect(consoleErrors).toEqual([]);
});

test('user can manage agents from the agent tab', async ({ context, page }) => {
  const consoleErrors: string[] = [];
  page.on('console', (message) => {
    if (message.type() === 'error') {
      consoleErrors.push(message.text());
    }
  });

  await context.grantPermissions(['clipboard-read', 'clipboard-write'], { origin: 'http://127.0.0.1:15183' });
  await page.goto('/');
  await page.getByLabel('Username').fill('antoine');
  await page.getByLabel('Password', { exact: true }).fill('correct-password');
  await page.getByRole('button', { name: 'Sign In' }).click();

  await page.getByRole('button', { name: 'Agent', exact: true }).click();
  await expect(page.getByRole('heading', { name: 'Agent', exact: true })).toBeVisible();
  await expect(page.getByRole('heading', { name: 'Agents', exact: true })).toBeVisible();
  await expect(page.getByRole('row', { name: /Agent Name Owners Linked Target Latest Command Command Result Last Run Actions/ })).toBeVisible();

  await page.getByRole('button', { name: 'Add Agent' }).click();
  await expect(page.getByRole('dialog', { name: 'Add Agent' })).toBeVisible();
  const addAgentDialog = page.getByRole('dialog', { name: 'Add Agent' });
  await expect(addAgentDialog.getByLabel('Agent ID', { exact: true })).toHaveCount(0);
  await page.getByLabel('Display name').fill('Deploy Agent');
  await page.getByRole('button', { name: 'Save Agent' }).click();
  await expect(addAgentDialog.getByText('Agent JWT Signing Key is required for a new agent.')).toBeVisible();
  const agentJwtSigningKeyInput = addAgentDialog.getByLabel('Agent JWT Signing Key', { exact: true });
  await agentJwtSigningKeyInput.fill('deploy-agent-signing-key');
  const interactionSelect = addAgentDialog.getByLabel('Interaction');
  await expect(interactionSelect).toHaveValue('local');
  await expect(interactionSelect.locator('option[value="local"]')).toHaveCount(1);
  await expect(interactionSelect.getByRole('option', { name: /Example ntfy.*NTFY/ })).toHaveCount(1);
  await interactionSelect.selectOption('6a5b2d49-8c61-4f2e-9f1a-7f47ddbd71a8');
  await expect(agentJwtSigningKeyInput).toHaveAttribute('type', 'password');
  await addAgentDialog.getByRole('button', { name: 'Show Agent JWT Signing Key' }).click();
  await expect(agentJwtSigningKeyInput).toHaveAttribute('type', 'text');
  await addAgentDialog.getByRole('button', { name: 'Hide Agent JWT Signing Key' }).click();
  await expect(agentJwtSigningKeyInput).toHaveAttribute('type', 'password');
  await page.getByRole('button', { name: 'Save Agent' }).click();
  await expect(page.getByRole('dialog', { name: 'Add Agent' })).toBeHidden();
  const createdAgentDialog = page.getByRole('dialog', { name: 'Agent created' });
  await expect(createdAgentDialog).toBeVisible();
  const createdAgentIDInput = createdAgentDialog.getByLabel('Agent ID', { exact: true });
  const createdAgentID = await createdAgentIDInput.inputValue();
  expect(createdAgentID).toMatch(/^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/);
  const expectedHarnessConfiguration = JSON.stringify({
    agent_id: createdAgentID,
    agent_jwt_signing_key: 'deploy-agent-signing-key'
  }, null, 2);
  await expect(createdAgentDialog.getByLabel('Harness agent configuration')).toHaveValue(expectedHarnessConfiguration);

  await createdAgentDialog.getByRole('button', { name: 'Copy Agent ID' }).click();
  await expect(createdAgentDialog.getByRole('status')).toHaveText('Agent ID copied.');
  await expect.poll(() => page.evaluate(() => navigator.clipboard.readText())).toBe(createdAgentID);

  await createdAgentDialog.getByRole('button', { name: 'Copy configuration' }).click();
  await expect(createdAgentDialog.getByRole('status')).toHaveText('Harness configuration copied.');
  await expect.poll(async () => (await page.evaluate(() => navigator.clipboard.readText())).replace(/\r\n/g, '\n')).toBe(expectedHarnessConfiguration);
  await createdAgentDialog.getByRole('button', { name: 'Done' }).click();
  await expect(createdAgentDialog).toBeHidden();
  await expect(page.getByText('deploy-agent-signing-key')).toHaveCount(0);
  await expect(page.getByRole('row', { name: /Deploy Agent/ })).toBeVisible();

  await page.getByRole('button', { name: 'Edit Deploy Agent' }).click();
  await expect(page.getByRole('dialog', { name: 'Edit Deploy Agent' })).toBeVisible();
  const editAgentDialog = page.getByRole('dialog', { name: 'Edit Deploy Agent' });
  await expect(editAgentDialog.getByLabel('Agent ID', { exact: true })).toHaveValue(createdAgentID);
  await expect(editAgentDialog.getByLabel('Agent ID', { exact: true })).toHaveAttribute('readonly', '');
  await expect(editAgentDialog.getByLabel('Interaction')).toHaveValue('6a5b2d49-8c61-4f2e-9f1a-7f47ddbd71a8');
  await editAgentDialog.getByLabel('Interaction').selectOption('local');
  await page.getByLabel('Display name').fill('Deploy Agent 2');
  await editAgentDialog.getByRole('checkbox', { name: 'Enabled', exact: true }).uncheck();
  await page.getByRole('button', { name: 'Save Agent' }).click();
  await expect(page.getByRole('dialog', { name: 'Edit Deploy Agent' })).toBeHidden();
  await expect(page.getByRole('row', { name: /Deploy Agent 2/ })).toBeVisible();

  await page.getByRole('button', { name: 'Delete Deploy Agent 2' }).click();
  await expect(page.getByRole('dialog', { name: 'Delete Agent' })).toBeVisible();
  await page.getByRole('button', { name: 'Delete Agent', exact: true }).click();
  await expect(page.getByRole('row', { name: /Deploy Agent 2/ })).toBeHidden();

  expect(consoleErrors).toEqual([]);
});

test('admin can manage users from the users tab', async ({ page }) => {
  const consoleErrors: string[] = [];
  page.on('console', (message) => {
    if (message.type() === 'error') {
      consoleErrors.push(message.text());
    }
  });

  await page.goto('/');
  await page.getByLabel('Username').fill('antoine');
  await page.getByLabel('Password', { exact: true }).fill('correct-password');
  await page.getByRole('button', { name: 'Sign In' }).click();

  await page.getByRole('button', { name: 'Users', exact: true }).click();
  await expect(page.getByRole('heading', { level: 1, name: 'Users', exact: true })).toBeVisible();
  await expect(page.getByRole('heading', { level: 2, name: 'Users', exact: true })).toBeVisible();
  await expect(page.getByRole('row', { name: /Username Name Role Status Actions/ })).toBeVisible();

  await page.getByRole('button', { name: 'Add User' }).click();
  await expect(page.getByRole('dialog', { name: 'Add User' })).toBeVisible();
  await page.getByLabel('Username').fill('temporary-user');
  await page.getByLabel('Name', { exact: true }).fill('Temporary User');
  await page.getByRole('button', { name: 'Save User' }).click();
  await expect(page.getByRole('dialog', { name: 'User created' })).toBeVisible();
  await expect(page.getByRole('textbox', { name: /Activation link/ })).toHaveValue(/#\/activate\?token=/);
  await page.getByRole('button', { name: 'Close', exact: true }).click();
  await expect(page.getByRole('row', { name: /temporary-user Temporary User User Pending Activation/ })).toBeVisible();

  await page.getByRole('button', { name: 'Edit Temporary User' }).click();
  await expect(page.getByRole('dialog', { name: 'Edit Temporary User' })).toBeVisible();
  await page.getByLabel('Username').fill('temporary-admin');
  await page.getByLabel('Name', { exact: true }).fill('Temporary Admin');
  await page.getByLabel('Administrator').check();
  await page.getByRole('button', { name: 'Save User' }).click();
  await expect(page.getByRole('dialog', { name: 'Edit Temporary User' })).toBeHidden();
  await expect(page.getByRole('row', { name: /temporary-admin Temporary Admin Administrator/ })).toBeVisible();

  await page.getByRole('button', { name: 'Delete Temporary Admin' }).click();
  await expect(page.getByRole('dialog', { name: 'Delete User' })).toBeVisible();
  await page.getByRole('button', { name: 'Delete User', exact: true }).click();
  await expect(page.getByRole('row', { name: /temporary-admin Temporary Admin/ })).toBeHidden();

  expect(consoleErrors).toEqual([]);
});

test('admin-created user can activate and sign in', async ({ page }) => {
  await page.goto('/');
  await page.getByLabel('Username').fill('antoine');
  await page.getByLabel('Password', { exact: true }).fill('correct-password');
  await page.getByRole('button', { name: 'Sign In' }).click();
  await page.getByRole('button', { name: 'Users', exact: true }).click();
  await page.getByRole('button', { name: 'Add User' }).click();
  await page.getByLabel('Username').fill('activation-user');
  await page.getByLabel('Name', { exact: true }).fill('Activation User');
  await page.getByLabel('Activation lifetime').fill('1');
  await page.getByLabel('Unit').selectOption('hours');
  await page.getByRole('button', { name: 'Save User' }).click();
  const activationURL = await page.getByRole('textbox', { name: /Activation link/ }).inputValue();
  await page.getByRole('button', { name: 'Close', exact: true }).click();
  await page.getByRole('button', { name: 'Logout' }).click();

  await page.goto(activationURL);
  await expect(page.getByRole('heading', { name: 'Activate your account' })).toBeVisible();
  await expect(page.getByText(/AGPL|No warranty|Version/)).toHaveCount(0);
  await expect(page.getByRole('link', { name: /License|Source/ })).toHaveCount(0);
  await expect(page.getByText('Activation User · activation-user')).toBeVisible();
  await page.getByRole('textbox', { name: 'New password', exact: true }).fill('activation-password');
  await page.getByRole('textbox', { name: 'Confirm new password', exact: true }).fill('activation-password');
  await page.getByRole('button', { name: 'Activate account' }).click();
  await expect(page.getByRole('heading', { name: 'Account activated' })).toBeVisible();
  await page.getByRole('button', { name: 'Return to sign in' }).click();
  await page.getByLabel('Username').fill('activation-user');
  await page.getByLabel('Password', { exact: true }).fill('activation-password');
  await page.getByRole('button', { name: 'Sign In' }).click();
  await expect(page.getByRole('heading', { name: 'Dashboard' })).toBeVisible();
});

test('admin can reset another user password with a one-time link', async ({ page }) => {
  await page.goto('/');
  await page.getByLabel('Username').fill('antoine');
  await page.getByLabel('Password', { exact: true }).fill('correct-password');
  await page.getByRole('button', { name: 'Sign In' }).click();
  await page.getByRole('button', { name: 'Users', exact: true }).click();
  await page.getByRole('button', { name: 'Add User' }).click();
  await page.getByLabel('Username').fill('reset-link-user');
  await page.getByLabel('Name', { exact: true }).fill('Reset Link User');
  await page.getByRole('button', { name: 'Save User' }).click();
  const activationURL = await page.getByRole('textbox', { name: /Activation link/ }).inputValue();
  await page.getByRole('button', { name: 'Close', exact: true }).click();
  await page.getByRole('button', { name: 'Logout' }).click();

  await page.goto(activationURL);
  await page.getByRole('textbox', { name: 'New password', exact: true }).fill('old-password');
  await page.getByRole('textbox', { name: 'Confirm new password', exact: true }).fill('old-password');
  await page.getByRole('button', { name: 'Activate account' }).click();
  await page.getByRole('button', { name: 'Return to sign in' }).click();
  await page.getByLabel('Username').fill('antoine');
  await page.getByLabel('Password', { exact: true }).fill('correct-password');
  await page.getByRole('button', { name: 'Sign In' }).click();
  await page.getByRole('button', { name: 'Users', exact: true }).click();

  await page.getByRole('button', { name: 'Reset password for Reset Link User' }).click();
  await expect(page.getByRole('dialog', { name: 'Reset password' })).toBeVisible();
  await page.getByRole('button', { name: 'Create reset link' }).click();
  const resetURL = await page.getByRole('textbox', { name: /Password reset link/ }).inputValue();
  await expect(page.getByRole('row', { name: /reset-link-user Reset Link User User Password Reset Pending/ })).toBeVisible();
  await page.getByRole('button', { name: 'Close', exact: true }).click();
  await page.getByRole('button', { name: 'Logout' }).click();

  await page.getByLabel('Username').fill('reset-link-user');
  await page.getByLabel('Password', { exact: true }).fill('old-password');
  await page.getByRole('button', { name: 'Sign In' }).click();
  await expect(page.getByText('invalid username or password')).toBeVisible();
  await page.goto(resetURL);
  await expect(page.getByRole('heading', { name: 'Reset your password' })).toBeVisible();
  await page.getByRole('textbox', { name: 'New password', exact: true }).fill('new-password');
  await page.getByRole('textbox', { name: 'Confirm new password', exact: true }).fill('new-password');
  await page.getByRole('button', { name: 'Reset password' }).click();
  await expect(page.getByRole('heading', { name: 'Password reset' })).toBeVisible();
  await page.getByRole('button', { name: 'Return to sign in' }).click();
  await page.getByLabel('Username').fill('reset-link-user');
  await page.getByLabel('Password', { exact: true }).fill('new-password');
  await page.getByRole('button', { name: 'Sign In' }).click();
  await expect(page.getByRole('heading', { name: 'Dashboard' })).toBeVisible();
});

test('non-admin user sees only their own agents and targets', async ({ page }) => {
  const consoleErrors: string[] = [];
  page.on('console', (message) => {
    if (message.type() === 'error') {
      consoleErrors.push(message.text());
    }
  });

  await page.goto('/');
  await page.getByLabel('Username').fill('operator');
  await page.getByLabel('Password', { exact: true }).fill('correct-password');
  await page.getByRole('button', { name: 'Sign In' }).click();

  await expect(page.getByRole('heading', { name: 'Dashboard' })).toBeVisible();
  await expect(page.getByRole('button', { name: 'Users', exact: true })).toHaveCount(0);

  await page.getByRole('button', { name: 'Target', exact: true }).click();
  await expect(page.getByRole('heading', { name: 'Target', exact: true })).toBeVisible();
  await expect(page.getByRole('row', { name: /OP-EDGE-01/ })).toBeVisible();
  await expect(page.getByRole('row', { name: /OP-WORKER-01/ })).toBeVisible();
  for (const name of ['WEB-SRV-01', 'DB-SRV-01', 'APP-SRV-01']) {
    await expect(page.getByRole('row', { name: new RegExp(name) })).toHaveCount(0);
  }

  await page.getByRole('button', { name: 'Agent', exact: true }).click();
  await expect(page.getByRole('heading', { name: 'Agent', exact: true })).toBeVisible();
  await expect(page.getByRole('row', { name: /operator-edge-agent/ })).toBeVisible();
  await expect(page.getByRole('row', { name: /operator-worker-agent/ })).toBeVisible();
  for (const name of ['agent-web-01', 'agent-db-01', 'agent-app-01']) {
    await expect(page.getByRole('row', { name: new RegExp(name) })).toHaveCount(0);
  }

  await page.getByRole('button', { name: 'Edit operator-edge-agent' }).click();
  const foreignAssignmentDialog = page.getByRole('dialog', { name: 'Edit operator-edge-agent' });
  const foreignInteraction = foreignAssignmentDialog.getByLabel('Interaction');
  await expect(foreignInteraction).toHaveValue('6a5b2d49-8c61-4f2e-9f1a-7f47ddbd71a8');
  await expect(foreignInteraction.locator('option[value="6a5b2d49-8c61-4f2e-9f1a-7f47ddbd71a8"]')).toHaveAttribute('disabled', '');
  await expect(foreignAssignmentDialog.getByText(/belongs to another user or is unavailable/)).toBeVisible();
  await foreignAssignmentDialog.getByRole('button', { name: 'Cancel' }).click();

  await page.getByRole('button', { name: 'Command History', exact: true }).click();
  await expect(page.getByRole('heading', { level: 1, name: 'Command History', exact: true })).toBeVisible();
  await expect(page.getByRole('row', { name: /operator-edge-agent OP-EDGE-01 operator-edge-audit Completed Success/ })).toBeVisible();
  await expect(page.getByRole('row', { name: /operator-worker-agent OP-WORKER-01 operator-worker-audit Completed Warning/ })).toBeVisible();
  for (const name of ['check-drift --full', 'backup --verify', 'rotate-credentials', 'antoine-private-audit', 'WEB-SRV-01', 'DB-SRV-01', 'APP-SRV-01']) {
    await expect(page.getByRole('row', { name: new RegExp(name) })).toHaveCount(0);
  }

  expect(consoleErrors).toEqual([]);
});

test('non-admin user can view add and delete policies without editing', async ({ page }) => {
  const consoleErrors: string[] = [];
  page.on('console', (message) => {
    if (message.type() === 'error') {
      consoleErrors.push(message.text());
    }
  });

  await page.goto('/');
  await page.getByLabel('Username').fill('operator');
  await page.getByLabel('Password', { exact: true }).fill('correct-password');
  await page.getByRole('button', { name: 'Sign In' }).click();

  await page.getByRole('button', { name: 'Policy', exact: true }).click();
  await expect(page.getByRole('heading', { level: 1, name: 'Policy', exact: true })).toBeVisible();
  await expect(page.getByRole('heading', { level: 2, name: 'Policies', exact: true })).toBeVisible();
  await expect(page.getByRole('row', { name: /Baseline Readonly Fixture.*Enabled 100/ })).toBeVisible();
  await expect(page.getByRole('button', { name: /Edit .*Policy/ })).toHaveCount(0);

  await page.getByRole('button', { name: 'Add Policy' }).click();
  const dialog = page.getByRole('dialog', { name: 'Add Policy' });
  await expect(dialog).toBeVisible();
  await dialog.getByLabel('Name').fill('Temporary E2E Policy');
  await dialog.getByLabel('Description').fill('Created by the policy tab e2e test');
  await dialog.getByLabel('Priority').fill('7');
  await dialog.getByLabel('Rule ID').fill('allow-e2e');
  await dialog.getByLabel('Effect').selectOption('allow');
  await dialog.getByLabel('Reason').fill('e2e cleanup target');
  await dialog.getByRole('button', { name: 'Save Policy' }).click();
  await expect(dialog).toBeHidden();
  await expect(page.getByRole('row', { name: /Temporary E2E Policy.*Enabled 7/ })).toBeVisible();

  await page.getByRole('button', { name: 'Delete Temporary E2E Policy' }).click();
  await expect(page.getByRole('dialog', { name: 'Delete Policy' })).toBeVisible();
  await page.getByRole('button', { name: 'Delete Policy', exact: true }).click();
  await expect(page.getByRole('row', { name: /Temporary E2E Policy/ })).toBeHidden();

  expect(consoleErrors).toEqual([]);
});

test('development Local interaction keeps Agent creation enabled without configured interactions', async ({ page }) => {
  await page.route('**/v1/user/interactions', async (route) => {
    if (route.request().method() === 'GET') {
      await route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ interactions: [] }) });
      return;
    }
    await route.continue();
  });

  await page.goto('/');
  await page.getByLabel('Username').fill('antoine');
  await page.getByLabel('Password', { exact: true }).fill('correct-password');
  await page.getByRole('button', { name: 'Sign In' }).click();

  let agentsPanel = page.locator('section.panel').filter({ has: page.getByRole('heading', { name: 'Agents', exact: true }) });
  await expect(agentsPanel.getByRole('button', { name: 'Add Agent', exact: true })).toBeEnabled();

  await page.getByRole('button', { name: 'Agent', exact: true }).click();
  agentsPanel = page.locator('section.panel').filter({ has: page.getByRole('heading', { name: 'Agents', exact: true }) });
  await agentsPanel.getByRole('button', { name: 'Add Agent', exact: true }).click();
  const dialog = page.getByRole('dialog', { name: 'Add Agent' });
  await expect(dialog.getByLabel('Interaction')).toHaveValue('local');
  await dialog.getByRole('button', { name: 'Cancel' }).click();
});

test('target creation requires both an agent and a policy on every target entry point', async ({ page }) => {
  await page.route('**/v1/user/agents', async (route) => {
    if (route.request().method() === 'GET') {
      await route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ agents: [] }) });
      return;
    }
    await route.continue();
  });
  await page.route('**/v1/user/policies', async (route) => {
    if (route.request().method() === 'GET') {
      await route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ policies: [] }) });
      return;
    }
    await route.continue();
  });

  await page.goto('/');
  await page.getByLabel('Username').fill('antoine');
  await page.getByLabel('Password', { exact: true }).fill('correct-password');
  await page.getByRole('button', { name: 'Sign In' }).click();

  let targetsPanel = page.locator('section.panel').filter({ has: page.getByRole('heading', { name: 'Targets', exact: true }) });
  let addTarget = targetsPanel.getByRole('button', { name: /^Add Target unavailable:/ });
  await expect(addTarget).toBeDisabled();
  await expect(targetsPanel.getByText('Create an agent and a policy before adding a target.', { exact: true })).toBeVisible();
  await expect(targetsPanel.getByRole('button', { name: 'Add Agent', exact: true })).toBeVisible();
  await expect(targetsPanel.getByRole('button', { name: 'Add Policy', exact: true })).toBeVisible();

  await targetsPanel.getByRole('button', { name: 'Add Agent', exact: true }).click();
  await expect(page.getByRole('dialog', { name: 'Add Agent' })).toBeVisible();
  await page.getByRole('dialog', { name: 'Add Agent' }).getByRole('button', { name: 'Cancel' }).click();
  await targetsPanel.getByRole('button', { name: 'Add Policy', exact: true }).click();
  await expect(page.getByRole('dialog', { name: 'Add Policy' })).toBeVisible();
  await page.getByRole('dialog', { name: 'Add Policy' }).getByRole('button', { name: 'Cancel' }).click();

  await page.getByRole('button', { name: 'Target', exact: true }).click();
  targetsPanel = page.locator('section.panel').filter({ has: page.getByRole('heading', { name: 'Targets', exact: true }) });
  addTarget = targetsPanel.getByRole('button', { name: /^Add Target unavailable:/ });
  await expect(addTarget).toBeDisabled();
  await expect(targetsPanel.getByText('Create an agent and a policy before adding a target.', { exact: true })).toBeVisible();
});

test('non-admin target creation identifies each missing prerequisite and enables when both exist', async ({ page }) => {
  let agentsMissing = true;
  let policiesMissing = false;
  await page.route('**/v1/user/agents', async (route) => {
    if (route.request().method() === 'GET' && agentsMissing) {
      await route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ agents: [] }) });
      return;
    }
    await route.continue();
  });
  await page.route('**/v1/user/policies', async (route) => {
    if (route.request().method() === 'GET' && policiesMissing) {
      await route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ policies: [] }) });
      return;
    }
    await route.continue();
  });

  await page.goto('/');
  await page.getByLabel('Username').fill('operator');
  await page.getByLabel('Password', { exact: true }).fill('correct-password');
  await page.getByRole('button', { name: 'Sign In' }).click();

  let targetsPanel = page.locator('section.panel').filter({ has: page.getByRole('heading', { name: 'Owned Targets', exact: true }) });
  await expect(targetsPanel.getByRole('button', { name: /^Add Target unavailable:/ })).toBeDisabled();
  await expect(targetsPanel.getByText('Create an agent before adding a target.', { exact: true })).toBeVisible();
  await expect(targetsPanel.getByRole('button', { name: /^Add Agent unavailable:/ })).toBeDisabled();
  await expect(targetsPanel.getByRole('button', { name: 'Add Interaction', exact: true })).toBeVisible();
  await expect(targetsPanel.getByRole('button', { name: 'Add Policy', exact: true })).toHaveCount(0);

  agentsMissing = false;
  policiesMissing = true;
  await page.reload();
  targetsPanel = page.locator('section.panel').filter({ has: page.getByRole('heading', { name: 'Owned Targets', exact: true }) });
  await expect(targetsPanel.getByRole('button', { name: /^Add Target unavailable:/ })).toBeDisabled();
  await expect(targetsPanel.getByText('Create a policy before adding a target.', { exact: true })).toBeVisible();
  await expect(targetsPanel.getByRole('button', { name: 'Add Agent', exact: true })).toHaveCount(0);
  await expect(targetsPanel.getByRole('button', { name: 'Add Policy', exact: true })).toBeVisible();

  policiesMissing = false;
  await page.reload();
  targetsPanel = page.locator('section.panel').filter({ has: page.getByRole('heading', { name: 'Owned Targets', exact: true }) });
  await expect(targetsPanel.getByRole('button', { name: 'Add Target', exact: true })).toBeEnabled();
  await expect(targetsPanel.getByText(/before adding a target/)).toHaveCount(0);
});

test('target creation stays disabled while prerequisites load and supports retry after failure', async ({ page }) => {
  let releaseAgents: (() => void) | undefined;
  const agentsReleased = new Promise<void>((resolve) => {
    releaseAgents = resolve;
  });
  let failPolicies = false;
  await page.route('**/v1/user/agents', async (route) => {
    if (route.request().method() === 'GET') {
      await agentsReleased;
    }
    await route.continue();
  });
  await page.route('**/v1/user/policies', async (route) => {
    if (route.request().method() === 'GET' && failPolicies) {
      await route.fulfill({ status: 500, contentType: 'application/json', body: JSON.stringify({ error: 'policy fixture failure' }) });
      return;
    }
    await route.continue();
  });

  await page.goto('/');
  await page.getByLabel('Username').fill('antoine');
  await page.getByLabel('Password', { exact: true }).fill('correct-password');
  await page.getByRole('button', { name: 'Sign In' }).click();

  const targetsPanel = page.locator('section.panel').filter({ has: page.getByRole('heading', { name: 'Targets', exact: true }) });
  await expect(targetsPanel.getByText('Checking agent and policy prerequisites…', { exact: true })).toBeVisible();
  await expect(targetsPanel.getByRole('button', { name: /^Add Target unavailable:/ })).toBeDisabled();

  releaseAgents?.();
  await expect(targetsPanel.getByRole('button', { name: 'Add Target', exact: true })).toBeEnabled();

  failPolicies = true;
  await page.reload();
  await expect(targetsPanel.getByText('Unable to verify agents and policies.', { exact: true })).toBeVisible();
  await expect(targetsPanel.getByRole('button', { name: /^Add Target unavailable:/ })).toBeDisabled();
  failPolicies = false;
  await targetsPanel.getByRole('button', { name: 'Retry', exact: true }).click();
  await expect(targetsPanel.getByRole('button', { name: 'Add Target', exact: true })).toBeEnabled();
});

test('user can manage targets from the target tab', async ({ page }) => {
  const consoleErrors: string[] = [];
  let managedTarget: Record<string, unknown> | null = null;
  page.on('console', (message) => {
    if (message.type() === 'error') {
      consoleErrors.push(message.text());
    }
  });

  await page.route('**/v1/user/targets**', async (route) => {
    const request = route.request();
    const path = new URL(request.url()).pathname;
    const method = request.method();
    if (path === '/v1/user/targets/test-credentials') {
      await route.fulfill({
        contentType: 'application/json',
        body: JSON.stringify({ status: 'success', message: 'SSH connection verified', credential_test_token: 'target-management-proof' })
      });
      return;
    }
    if (path === '/v1/user/targets' && method === 'GET') {
      const response = await route.fetch();
      const payload = (await response.json()) as { targets: Record<string, unknown>[] };
      if (managedTarget) {
        payload.targets = [...payload.targets.filter((target) => target.id !== managedTarget?.id), managedTarget];
      }
      await route.fulfill({ response, json: payload });
      return;
    }
    if (path === '/v1/user/targets' && method === 'POST') {
      const input = request.postDataJSON() as Record<string, unknown>;
      managedTarget = {
        ...input,
        id: '99999999-9999-4999-8999-999999999999',
        user_ids: ['fcd2f3a9-d546-44f4-837b-0c7aa4219752']
      };
      await route.fulfill({ status: 201, contentType: 'application/json', body: JSON.stringify(managedTarget) });
      return;
    }
    if (path === '/v1/user/targets/99999999-9999-4999-8999-999999999999' && method === 'PUT' && managedTarget) {
      managedTarget = { ...managedTarget, ...(request.postDataJSON() as Record<string, unknown>) };
      await route.fulfill({ contentType: 'application/json', body: JSON.stringify(managedTarget) });
      return;
    }
    if (path === '/v1/user/targets/99999999-9999-4999-8999-999999999999' && method === 'DELETE') {
      managedTarget = null;
      await route.fulfill({ contentType: 'application/json', body: JSON.stringify({ status: 'deleted' }) });
      return;
    }
    await route.continue();
  });

  await page.goto('/');
  await page.getByLabel('Username').fill('antoine');
  await page.getByLabel('Password', { exact: true }).fill('correct-password');
  await page.getByRole('button', { name: 'Sign In' }).click();

  await page.getByRole('button', { name: 'Target', exact: true }).click();
  await expect(page.getByRole('heading', { name: 'Target', exact: true })).toBeVisible();
  await expect(page.getByRole('heading', { name: 'Targets', exact: true })).toBeVisible();
  await expect(page.getByRole('row', { name: /Target Name Type Owners Endpoint Last Seen Actions/ })).toBeVisible();

  await page.getByRole('button', { name: 'Add Target' }).click();
  const addTargetDialog = page.getByRole('dialog', { name: 'Add Target' });
  await expect(addTargetDialog).toBeVisible();
  await addTargetDialog.getByLabel('Hostname').fill('OPS-SRV-01');
  await addTargetDialog.getByLabel('IP address').fill('10.10.9.10');
  await addTargetDialog.getByLabel('Description').fill('Operations test target');
  await addTargetDialog.getByLabel('Username').fill('fixture-ops');
  await addTargetDialog.getByLabel('Password', { exact: true }).fill('fixture-ops-password');
  await addTargetDialog.getByLabel('Policy Name').selectOption({ label: 'Baseline Readonly Fixture' });
  await addTargetDialog.getByText('Select agents').click();
  await addTargetDialog.getByLabel('agent-web-01').check();
  await expect(addTargetDialog.getByRole('button', { name: 'Save Target' })).toBeDisabled();
  await addTargetDialog.getByRole('button', { name: 'Test credentials' }).click();
  await expect(addTargetDialog.getByText('SSH connection verified')).toBeVisible();
  await addTargetDialog.getByRole('button', { name: 'Save Target' }).click();
  await expect(addTargetDialog).toBeHidden();
  await expect(page.getByRole('row', { name: /OPS-SRV-01 SSH password/ })).toBeVisible();

  await page.getByRole('button', { name: 'Edit OPS-SRV-01' }).click();
  await expect(page.getByRole('dialog', { name: 'Edit OPS-SRV-01' })).toBeVisible();
  await page.getByLabel('Description').fill('Metadata-only operations update');
  await expect(page.getByRole('button', { name: 'Save Target' })).toBeEnabled();
  await page.getByRole('button', { name: 'Save Target' }).click();
  await expect(page.getByRole('dialog', { name: 'Edit OPS-SRV-01' })).toBeHidden();

  await page.getByRole('button', { name: 'Edit OPS-SRV-01' }).click();
  await expect(page.getByRole('dialog', { name: 'Edit OPS-SRV-01' })).toBeVisible();
  await page.getByLabel('Hostname').fill('OPS-SRV-02');
  await page.getByLabel('IP address').fill('10.10.9.11');
  await page.getByLabel('Description').fill('Updated operations test target');
  await page.getByLabel('Username').fill('fixture-ops');
  await page.getByRole('dialog', { name: 'Edit OPS-SRV-01' }).getByRole('textbox', { name: /^Password/ }).fill('fixture-ops-password');
  await expect(page.getByRole('button', { name: 'Save Target' })).toBeDisabled();
  await page.getByRole('button', { name: 'Test credentials' }).click();
  await expect(page.getByText('SSH connection verified')).toBeVisible();
  await page.getByRole('button', { name: 'Save Target' }).click();
  await expect(page.getByRole('dialog', { name: 'Edit OPS-SRV-01' })).toBeHidden();
  await expect(page.getByRole('row', { name: /OPS-SRV-02 SSH password/ })).toBeVisible();

  await page.getByRole('button', { name: 'Delete OPS-SRV-02' }).click();
  await expect(page.getByRole('dialog', { name: 'Delete Target' })).toBeVisible();
  await page.getByRole('button', { name: 'Delete Target' }).click();
  await expect(page.getByRole('row', { name: /OPS-SRV-02 SSH password/ })).toBeHidden();

  expect(consoleErrors).toEqual([]);
});
