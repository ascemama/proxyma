import { spawn, spawnSync } from 'node:child_process';
import { existsSync, mkdirSync, readFileSync, rmSync, writeFileSync } from 'node:fs';
import net from 'node:net';
import { dirname, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';

const e2eDir = dirname(fileURLToPath(import.meta.url));
const frontendRoot = resolve(e2eDir, '..');
const vitePidPath = resolve(frontendRoot, 'test-results-production', 'vite-e2e.pid');
const vitePort = 15183;

async function globalSetup() {
  stopProcess(vitePidPath);
  mkdirSync(dirname(vitePidPath), { recursive: true });

  const build = process.platform === 'win32'
    ? spawnSync('cmd.exe', ['/d', '/s', '/c', 'npm.cmd run build'], { cwd: frontendRoot, stdio: 'inherit' })
    : spawnSync('npm', ['run', 'build'], { cwd: frontendRoot, stdio: 'inherit' });
  if (build.status !== 0) {
    throw new Error(`frontend build failed with status ${build.status}`);
  }

  const viteArgs = ['preview', '--host', '127.0.0.1', '--port', String(vitePort)];
  const vite = process.platform === 'win32'
    ? spawn('cmd.exe', ['/d', '/s', '/c', `node_modules\\.bin\\vite.cmd ${viteArgs.join(' ')}`], {
        cwd: frontendRoot,
        detached: true,
        stdio: 'ignore',
        windowsHide: true
      })
    : spawn(resolve(frontendRoot, 'node_modules', '.bin', 'vite'), viteArgs, {
        cwd: frontendRoot,
        detached: true,
        stdio: 'ignore'
      });
  if (!vite.pid) {
    throw new Error('Vite production preview did not start');
  }
  writeFileSync(vitePidPath, String(vite.pid), 'utf8');
  vite.unref();
  await waitForPort(vitePort, 120_000);
}

function waitForPort(port: number, timeoutMs: number) {
  const started = Date.now();
  return new Promise<void>((resolveReady, rejectReady) => {
    const check = () => {
      const socket = net.connect(port, '127.0.0.1');
      socket.once('connect', () => {
        socket.end();
        resolveReady();
      });
      socket.once('error', () => {
        socket.destroy();
        if (Date.now() - started > timeoutMs) {
          rejectReady(new Error(`Timed out waiting for Vite preview on port ${port}`));
          return;
        }
        setTimeout(check, 250);
      });
    };
    check();
  });
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

export default globalSetup;
