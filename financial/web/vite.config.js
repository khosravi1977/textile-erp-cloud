import { defineConfig } from 'vite';
import react from '@vitejs/plugin-react';
import { financialIntegrityPlugin } from './vite-financial-integrity.js';

export default defineConfig({
  plugins: [financialIntegrityPlugin(), react()],
  base: './',
});
