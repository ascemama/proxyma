import { expect, test } from '@playwright/test';

test('desktop login and dashboard screenshots are visually usable', async ({ page }) => {
  const consoleErrors: string[] = [];
  page.on('console', (message) => {
    if (message.type() === 'error') {
      consoleErrors.push(message.text());
    }
  });

  await page.goto('/');
  await expect(page.getByRole('heading', { name: 'Proxyma' })).toBeVisible();
  await page.screenshot({ fullPage: true, path: 'test-results/login-desktop.png' });

  await page.getByLabel('Username').fill('antoine');
  await page.getByLabel('Password', { exact: true }).fill('correct-password');
  await page.getByRole('button', { name: 'Sign In' }).click();

  await expect(page.getByRole('heading', { name: 'Dashboard' })).toBeVisible();
  await expect(page.getByText('Recent Command History')).toBeVisible();
  await expect(page.getByRole('heading', { name: 'Targets', exact: true })).toBeVisible();
  await expect(page.getByText('Agents')).toBeVisible();
  await page.screenshot({ fullPage: true, path: 'test-results/dashboard-desktop.png' });

  await page.getByRole('button', { name: 'Open settings for Antoine' }).click();
  await expect(page.getByRole('dialog', { name: 'Settings' })).toBeVisible();
  await page.screenshot({ path: 'test-results/settings-desktop.png' });
  await page.getByRole('button', { name: 'Close settings' }).click();

  await page.getByRole('button', { name: 'Open settings for Antoine' }).click();
  const appearanceSettings = page.getByRole('dialog', { name: 'Settings' });
  await appearanceSettings.getByRole('button', { name: 'Appearance', exact: true }).click();
  await appearanceSettings.getByRole('radio', { name: /Dark/ }).check();
  await expect(page.locator('html')).toHaveAttribute('data-theme', 'dark');
  await page.screenshot({ path: 'test-results/settings-appearance-dark-desktop.png' });
  const settingsBackdrop = page.locator('.modal-backdrop');
  await settingsBackdrop.evaluate((element) => {
    element.style.display = 'none';
  });
  await expect(page.getByRole('row', { name: /^WEB-SRV-01 SSH password/ })).toBeVisible();
  await page.screenshot({ path: 'test-results/dashboard-dark-desktop.png' });
  await settingsBackdrop.evaluate((element) => {
    element.style.removeProperty('display');
  });
  await appearanceSettings.getByRole('button', { name: 'Cancel' }).click();
  await expect(page.locator('html')).toHaveAttribute('data-theme', 'light');

  await page.getByRole('button', { name: 'Target', exact: true }).click();
  await expect(page.getByRole('heading', { name: 'Target', exact: true })).toBeVisible();
  await expect(page.getByRole('heading', { name: 'Targets', exact: true })).toBeVisible();
  await page.screenshot({ fullPage: true, path: 'test-results/target-desktop.png' });

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
				host_key: { host: '10.10.1.80', port: 22, algorithm: 'ssh-ed25519', fingerprint: 'SHA256:visual-test' }
			})
		});
		return;
	}
    await route.fulfill({
      contentType: 'application/json',
	  body: JSON.stringify({ status: 'success', message: 'SSH connection verified', credential_test_token: 'visual-proof-token' })
    });
  });
  await page.getByRole('button', { name: 'Add Target' }).click();
  const addTargetDialog = page.getByRole('dialog', { name: 'Add Target' });
  await expect(addTargetDialog.getByRole('heading', { name: 'Add Target' })).toBeVisible();
  await addTargetDialog.getByLabel('Policy Name').selectOption({ index: 1 });
  await addTargetDialog.locator('details.multi-select summary').click();
  await addTargetDialog.getByRole('group', { name: 'Agent Name' }).getByRole('checkbox').first().check();
  await expect(addTargetDialog.getByRole('button', { name: 'Test credentials' })).toBeDisabled();
	await expect(addTargetDialog.getByRole('button', { name: 'Save Target' })).toBeDisabled();
  await addTargetDialog.getByLabel('IP address').fill('10.10.1.80');
  await addTargetDialog.getByLabel('Username').fill('fixture-new');
  await addTargetDialog.getByRole('textbox', { name: 'Password' }).fill('new-target-secret');
  await expect(addTargetDialog.getByRole('button', { name: 'Test credentials' })).toBeEnabled();
  await addTargetDialog.getByRole('button', { name: 'Test credentials' }).click();
	await expect(addTargetDialog.getByRole('alertdialog', { name: 'Unknown SSH host key' })).toBeVisible();
	await expect(addTargetDialog.getByText('SHA256:visual-test')).toBeVisible();
	await addTargetDialog.getByRole('button', { name: 'Trust and retry' }).click();
  await expect(addTargetDialog.getByText('SSH connection verified')).toBeVisible();
	await expect(addTargetDialog.getByRole('button', { name: 'Save Target' })).toBeEnabled();
  await page.screenshot({ fullPage: true, path: 'test-results/add-target-test-credentials-desktop.png' });

  await addTargetDialog.getByLabel('Auth type').selectOption('private_key');
  await expect(addTargetDialog.getByText('SSH connection verified')).toHaveCount(0);
  await expect(addTargetDialog.getByLabel('Private key file')).toBeVisible();
  await expect(page.getByText('Private key path')).toHaveCount(0);
  await page.screenshot({ fullPage: true, path: 'test-results/add-target-private-key-desktop.png' });

  expect(consoleErrors.filter((message) => !message.includes('server responded with a status of 409 (Conflict)'))).toEqual([]);
});

test('activation page screenshots are visually usable', async ({ page }) => {
  await page.goto('/');
  await page.getByLabel('Username').fill('antoine');
  await page.getByLabel('Password', { exact: true }).fill('correct-password');
  await page.getByRole('button', { name: 'Sign In' }).click();
  await page.getByRole('button', { name: 'Users', exact: true }).click();
  await page.getByRole('button', { name: 'Add User' }).click();
  await page.getByLabel('Username').fill('visual-activation');
  await page.getByLabel('Name', { exact: true }).fill('Visual Activation');
  await page.getByRole('button', { name: 'Save User' }).click();
  const activationURL = await page.getByRole('textbox', { name: /Activation link/ }).inputValue();
  await page.getByRole('button', { name: 'Close', exact: true }).click();
  await page.getByRole('button', { name: 'Logout' }).click();
  await page.goto(activationURL);
  await expect(page.getByRole('heading', { name: 'Activate your account' })).toBeVisible();
  await page.screenshot({ fullPage: true, path: 'test-results/activation-light-desktop.png' });
  await page.locator('html').evaluate((element) => { element.dataset.theme = 'dark'; });
  await page.screenshot({ fullPage: true, path: 'test-results/activation-dark-desktop.png' });

  await page.getByRole('textbox', { name: 'New password', exact: true }).fill('visual-password');
  await page.getByRole('textbox', { name: 'Confirm new password', exact: true }).fill('visual-password');
  await page.getByRole('button', { name: 'Activate account' }).click();
  await page.getByRole('button', { name: 'Return to sign in' }).click();
  await page.locator('html').evaluate((element) => { element.dataset.theme = 'light'; });
  await page.getByLabel('Username').fill('antoine');
  await page.getByLabel('Password', { exact: true }).fill('correct-password');
  await page.getByRole('button', { name: 'Sign In' }).click();
  await page.getByRole('button', { name: 'Users', exact: true }).click();
  await page.getByRole('button', { name: 'Reset password for Visual Activation' }).click();
  await page.getByRole('button', { name: 'Create reset link' }).click();
  const resetURL = await page.getByRole('textbox', { name: /Password reset link/ }).inputValue();
  await page.getByRole('button', { name: 'Close', exact: true }).click();
  await page.getByRole('button', { name: 'Logout' }).click();
  await page.goto(resetURL);
  await expect(page.getByRole('heading', { name: 'Reset your password' })).toBeVisible();
  await page.screenshot({ fullPage: true, path: 'test-results/password-reset-light-desktop.png' });
  await page.locator('html').evaluate((element) => { element.dataset.theme = 'dark'; });
  await page.screenshot({ fullPage: true, path: 'test-results/password-reset-dark-desktop.png' });
});
