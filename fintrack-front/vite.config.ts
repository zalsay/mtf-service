import path from 'path';
import { defineConfig, loadEnv } from 'vite';
import react from '@vitejs/plugin-react';

export default defineConfig(({ mode }) => {
    const env = loadEnv(mode, '.', '');
    const apiProxyTarget = env.VITE_API_PROXY_TARGET || 'http://127.0.0.1:59000';
    const devBasePath = '/dev/ai-fin/fintrack-front';
    return {
      server: {
        port: 3000,
        host: '0.0.0.0',
        allowedHosts: true,
        proxy: {
          [`${devBasePath}/api/v1`]: {
            target: apiProxyTarget,
            changeOrigin: true,
            rewrite: (path) => path.replace(devBasePath, ''),
          },
          '/api/v1': {
            target: apiProxyTarget,
            changeOrigin: true,
          },
        },
      },
      plugins: [react()],
      define: {
        'process.env.API_KEY': JSON.stringify(env.GEMINI_API_KEY),
        'process.env.GEMINI_API_KEY': JSON.stringify(env.GEMINI_API_KEY)
      },
      resolve: {
        alias: {
          '@': path.resolve(__dirname, '.'),
        }
      }
    };
});
