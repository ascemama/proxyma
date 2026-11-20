import { expect, test } from '@playwright/test';

const screenshotKey = 'docs-only-agent-signing-key';
const screenshotAgentID = '00000000-0000-4000-8000-000000000001';
const screenshotDirectory = 'test-results/docs-screenshots';

test('captures the current first-time configuration GUI', async ({ page }) => {
  await page.setViewportSize({ width: 1440, height: 900 });
  await page.goto('/');

  await expect(page.getByRole('heading', { name: 'Proxyma' })).toBeVisible();
  await page.screenshot({
    path: `${screenshotDirectory}/first-time-login.jpg`,
    type: 'jpeg',
    quality: 90,
    fullPage: true
  });

  await login(page);
  await expect(page.getByRole('heading', { name: 'Dashboard' })).toBeVisible();
  await expect(page.getByText('Recent History')).toBeVisible();
  await page.screenshot({
    path: `${screenshotDirectory}/dashboard.jpg`,
    type: 'jpeg',
    quality: 90,
    fullPage: true
  });

  await page.getByRole('button', { name: 'Policies', exact: true }).click();
  await expect(page.getByRole('heading', { level: 1, name: 'Policies', exact: true })).toBeVisible();
  await page.getByRole('button', { name: 'Add policy', exact: true }).click();
  const policyDetail = page.locator('.policy-detail-panel');
  await expect(policyDetail.getByRole('heading', { name: 'New policy', exact: true })).toBeVisible();
  await policyDetail.getByLabel('Name').fill('Safe diagnostics');
  await policyDetail.getByLabel('Description').fill('Read-only diagnostic commands');
  await policyDetail.locator('.policy-add-rule-menu summary').click();
  await policyDetail.getByRole('menuitem', { name: /Whitelist rule/ }).click();
  await policyDetail.getByLabel('Command template for whitelist rule').fill('hostname');
  await policyDetail.getByLabel('Reason for whitelist rule').fill('Read-only system identity check');
  await page.screenshot({
    path: `${screenshotDirectory}/first-time-policy.jpg`,
    type: 'jpeg',
    quality: 90,
    fullPage: true
  });

  await page.getByRole('button', { name: 'Interactions', exact: true }).click();
  await expect(page.getByRole('heading', { level: 1, name: 'Interactions', exact: true })).toBeVisible();
  await page.getByRole('button', { name: 'Add Interaction', exact: true }).click();
  const interactionTypeDialog = page.getByRole('dialog', { name: 'Add Interaction' });
  await interactionTypeDialog.getByRole('button', { name: /NTFY/ }).click();
  const interactionDialog = page.getByRole('dialog', { name: 'Add NTFY Interaction' });
  await expect(interactionDialog).toBeVisible();
  await interactionDialog.getByLabel('Display Name').fill('Documentation approvals');
  await interactionDialog.getByLabel('Base URL').fill('https://ntfy.example');
  await interactionDialog.getByLabel('Request Topic').fill('proxyma-docs-requests');
  await interactionDialog.getByLabel('Response Topic').fill('proxyma-docs-responses');
  await interactionDialog.getByLabel('Poll Interval').fill('5s');
  await interactionDialog.getByLabel('Poll Timeout').fill('30s');
  await interactionDialog.getByLabel('Responded By').fill('approval-operator');
  await page.screenshot({
    path: `${screenshotDirectory}/first-time-ntfy.jpg`,
    type: 'jpeg',
    quality: 90,
    fullPage: true
  });

  await interactionDialog.getByRole('button', { name: 'Cancel' }).click();
  await page.getByRole('button', { name: 'Agents', exact: true }).click();
  await expect(page.getByRole('heading', { level: 1, name: 'Agents', exact: true })).toBeVisible();
  await page.getByRole('button', { name: 'Add Agent', exact: true }).click();
  const addAgentDialog = page.getByRole('dialog', { name: 'Add Agent' });
  await addAgentDialog.getByLabel('Display name').fill('Documentation Agent');
  await addAgentDialog.getByLabel('Agent JWT Signing Key', { exact: true }).fill(screenshotKey);
  await addAgentDialog.getByRole('button', { name: 'Save Agent' }).click();
  const createdAgentDialog = page.getByRole('dialog', { name: 'Agent created' });
  await expect(createdAgentDialog).toBeVisible();
  await createdAgentDialog.getByLabel('Agent ID', { exact: true }).evaluate((element, value) => {
    (element as HTMLInputElement).value = value;
  }, screenshotAgentID);
  await expect(createdAgentDialog.getByLabel('Agent ID', { exact: true })).toHaveValue(screenshotAgentID);
  const sanitizedHarnessConfiguration = JSON.stringify({
    agent_id: screenshotAgentID,
    agent_jwt_signing_key: '<redacted for documentation>'
  }, null, 2);
  await createdAgentDialog.getByLabel('Harness agent configuration').evaluate((element, value) => {
    (element as HTMLTextAreaElement).value = value;
  }, sanitizedHarnessConfiguration);
  await expect(createdAgentDialog.getByLabel('Harness agent configuration')).not.toHaveValue(screenshotKey);
  await expect(createdAgentDialog.getByLabel('Harness agent configuration')).toHaveValue(sanitizedHarnessConfiguration);
  await page.screenshot({
    path: `${screenshotDirectory}/first-time-agent.jpg`,
    type: 'jpeg',
    quality: 90,
    fullPage: true
  });
  await createdAgentDialog.getByRole('button', { name: 'Done' }).click();

  await page.route('**/v1/user/targets/test-credentials', async (route) => {
    const request = route.request();
    const contentType = request.headers()['content-type'] ?? '';
    let approvedFingerprint = '';
    if (contentType.includes('application/json')) {
      approvedFingerprint = ((request.postDataJSON() as { approved_host_key_fingerprint?: string } | null)?.approved_host_key_fingerprint) ?? '';
    }
    if (!approvedFingerprint) {
      await route.fulfill({
        status: 409,
        contentType: 'application/json',
        body: JSON.stringify({
          error: 'SSH host key is not trusted',
          code: 'ssh_host_key_unknown',
          host_key: {
            host: '10.10.1.80',
            port: 22,
            algorithm: 'ssh-ed25519',
            fingerprint: 'SHA256:documentation-test'
          }
        })
      });
      return;
    }
    await route.fulfill({
      contentType: 'application/json',
      body: JSON.stringify({
        status: 'success',
		message: 'SSH connection verified'
      })
    });
  });

  // The target form contains every connection and assignment field, so give the
  // modal enough vertical room for its action buttons to remain visible.
  await page.setViewportSize({ width: 1440, height: 1200 });
  await page.getByRole('button', { name: 'Targets', exact: true }).click();
  await expect(page.getByRole('heading', { level: 1, name: 'Targets', exact: true })).toBeVisible();
  await page.getByRole('button', { name: 'Add Target', exact: true }).click();
  const addTargetDialog = page.getByRole('dialog', { name: 'Add Target' });
  await addTargetDialog.getByLabel('Policy Name').selectOption({ index: 1 });
  await addTargetDialog.locator('details.multi-select summary').click();
  await addTargetDialog.getByRole('group', { name: 'Agent Name' }).getByRole('checkbox').first().check();
  await addTargetDialog.getByLabel('IP address').fill('10.10.1.80');
  await addTargetDialog.getByLabel('Username').fill('docs-runner');
  await addTargetDialog.getByRole('textbox', { name: 'Password' }).fill('documentation-only-password');
  await addTargetDialog.getByRole('button', { name: 'Test credentials' }).click();
  await expect(addTargetDialog.getByRole('alertdialog', { name: 'Unknown SSH host key' })).toBeVisible();
  await addTargetDialog.getByRole('button', { name: 'Trust and retry' }).click();
  await expect(addTargetDialog.getByText('SSH connection verified')).toBeVisible();
  await page.screenshot({
    path: `${screenshotDirectory}/first-time-target.jpg`,
    type: 'jpeg',
    quality: 90,
    fullPage: true
  });
});

async function login(page: import('@playwright/test').Page) {
  await page.getByLabel('Username').fill('demo-admin');
  await page.getByLabel('Password', { exact: true }).fill('correct-password');
  await page.getByRole('button', { name: 'Sign In' }).click();
}
