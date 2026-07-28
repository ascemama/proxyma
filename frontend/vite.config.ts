import { defineConfig } from 'vite';
import react from '@vitejs/plugin-react';

const apiTarget = process.env.PROXYMA_API_BASE_URL ?? 'http://127.0.0.1:8080';

export default defineConfig({
  plugins: [react()],
  server: {
    port: 5173,
    proxy: {
      '/v1': {
        target: apiTarget,
        changeOrigin: true
      }
    }
  }
});
