import { existsSync, readFileSync, rmSync } from 'node:fs';
import { spawnSync } from 'node:child_process';
import { dirname, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';

const e2eDir = dirname(fileURLToPath(import.meta.url));
const frontendRoot = resolve(e2eDir, '..');
const ProxymaPidPath = resolve(frontendRoot, 'test-results', 'proxyma-e2e.pid');
const vitePidPath = resolve(frontendRoot, 'test-results', 'vite-e2e.pid');

async function globalTeardown() {
  stopProcess(vitePidPath);
  stopProcess(ProxymaPidPath);
}

function stopProcess(pidPath: string) {
  if (!existsSync(pidPath)) {
	return;
  }
  const pid = readFileSync(pidPath, 'utf8').trim();
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
  rmSync(pidPath, { force: true });
}

export default globalTeardown;
