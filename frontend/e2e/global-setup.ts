import { spawn, spawnSync } from 'node:child_process';
import { cpSync, existsSync, mkdirSync, readFileSync, rmSync, writeFileSync } from 'node:fs';
import net from 'node:net';
import { dirname, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';

const e2eDir = dirname(fileURLToPath(import.meta.url));
const frontendRoot = resolve(e2eDir, '..');
const repoRoot = resolve(e2eDir, '..', '..');
const ProxymaPidPath = resolve(frontendRoot, 'test-results', 'proxyma-e2e.pid');
const vitePidPath = resolve(frontendRoot, 'test-results', 'vite-e2e.pid');
const runtimeFixtures = resolve(frontendRoot, 'test-results', 'runtime-fixtures');
const proxymaPort = 18083;
const vitePort = 15183;

async function globalSetup() {
  stopProcess(vitePidPath);
  stopProcess(ProxymaPidPath);

  const goCache = resolve(repoRoot, '.gocache');
  const goTmp = resolve(repoRoot, '.gotmp');
  mkdirSync(goCache, { recursive: true });
  mkdirSync(goTmp, { recursive: true });
  mkdirSync(dirname(ProxymaPidPath), { recursive: true });
  rmSync(runtimeFixtures, { recursive: true, force: true });
  cpSync(resolve(e2eDir, 'fixtures'), runtimeFixtures, { recursive: true });
  mkdirSync(resolve(runtimeFixtures, 'interactions'), { recursive: true });
  mkdirSync(resolve(runtimeFixtures, 'user_sessions'), { recursive: true });

  const env = {
    ...process.env,
    GOCACHE: goCache,
    GOTMPDIR: goTmp
  };
  const binary = resolve(repoRoot, 'bin', 'proxyma-e2e.exe');
  const build = spawnSync('go', ['build', '-o', binary, './cmd/proxyma'], {
    cwd: repoRoot,
    env,
    stdio: 'inherit'
  });
  if (build.status !== 0) {
    throw new Error(`go build failed with status ${build.status}`);
  }

  const child = spawn(
    binary,
    [
      '--no-config',
      '--listen',
      `127.0.0.1:${proxymaPort}`,
      '--scheme',
      'http',
      '--target-dir',
      './frontend/test-results/runtime-fixtures/targets',
      '--policy-dir',
      './frontend/test-results/runtime-fixtures/policies',
      '--agent-dir',
      './frontend/test-results/runtime-fixtures/agents',
      '--user-dir',
      './frontend/test-results/runtime-fixtures/users',
      '--task-dir',
      './frontend/test-results/runtime-fixtures/tasks',
      '--interaction-dir',
      './frontend/test-results/runtime-fixtures/interactions',
      '--interaction-config-dir',
      './frontend/test-results/runtime-fixtures/interaction_configs',
      '--user-session-dir',
      './frontend/test-results/runtime-fixtures/user_sessions',
      '--require-command-approval=false'
    ],
    {
      cwd: repoRoot,
      env,
      detached: true,
      stdio: 'ignore',
      windowsHide: true
    }
  );
  if (!child.pid) {
    throw new Error('Proxyma test server did not start');
  }
  writeFileSync(ProxymaPidPath, String(child.pid), 'utf8');
  child.unref();
  await waitForPort(proxymaPort, 120_000);

  const vite =
    process.platform === 'win32'
      ? spawn('cmd.exe', ['/d', '/s', '/c', `node_modules\\.bin\\vite.cmd --host 127.0.0.1 --port ${vitePort}`], {
          cwd: frontendRoot,
          env: { ...process.env, PROXYMA_API_BASE_URL: `http://127.0.0.1:${proxymaPort}` },
          detached: true,
          stdio: 'ignore',
          windowsHide: true
        })
      : spawn(resolve(frontendRoot, 'node_modules', '.bin', 'vite'), ['--host', '127.0.0.1', '--port', String(vitePort)], {
          cwd: frontendRoot,
          env: { ...process.env, PROXYMA_API_BASE_URL: `http://127.0.0.1:${proxymaPort}` },
          detached: true,
          stdio: 'ignore'
        });
  if (!vite.pid) {
    throw new Error('Vite test server did not start');
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
          rejectReady(new Error(`Timed out waiting for Proxyma on port ${port}`));
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
