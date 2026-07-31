import { spawnSync } from 'node:child_process';
import { existsSync, readFileSync, rmSync } from 'node:fs';
import { dirname, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';

const e2eDir = dirname(fileURLToPath(import.meta.url));
const frontendRoot = resolve(e2eDir, '..');
const vitePidPath = resolve(frontendRoot, 'test-results-production', 'vite-e2e.pid');

async function globalTeardown() {
  if (!existsSync(vitePidPath)) {
    return;
  }
  const pid = readFileSync(vitePidPath, 'utf8').trim();
  if (pid) {
    if (process.platform === 'win32') {
      spawnSync('taskkill', ['/PID', pid, '/T', '/F'], { stdio: 'ignore' });
    } else {
      try {
        process.kill(Number(pid), 'SIGTERM');
      } catch {
        // The process may already be gone.
      }
    }
  }
  rmSync(vitePidPath, { force: true });
}

export default globalTeardown;
