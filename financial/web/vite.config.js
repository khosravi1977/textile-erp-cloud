import { defineConfig } from 'vite';
import react from '@vitejs/plugin-react';
import { accountingAuditAppTransformPlugin } from './auditAppTransform.js';

export default defineConfig({
  plugins: [accountingAuditAppTransformPlugin(), react()],
  base: './',
});
