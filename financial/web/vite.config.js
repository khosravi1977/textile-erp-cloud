import { defineConfig } from 'vite';
import react from '@vitejs/plugin-react';
import { financialIntegrityPlugin } from './vite-financial-integrity-v2.js';

export default defineConfig({
  plugins: [financialIntegrityPlugin(), react()],
  base: './',
});
