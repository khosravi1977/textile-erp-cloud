# Google Apps Script relay

This fallback relay is used when the production VPS cannot reach Telegram or the
primary relay. It exposes only the four Telegram methods required by Textile ERP.

Deployment requirements:

1. Create a Google Apps Script project and paste `Code.gs`.
2. Replace `SET_IN_DEPLOYED_COPY_ONLY` only in the deployed copy with the SHA-256
   digest of `TEXTILE_TELEGRAM_RELAY_TOKEN`. Never place the raw token in source.
3. Deploy as a web app that executes as the project owner and allows anonymous
   access.
4. Store the resulting HTTPS `/exec` URL in the existing
   `TEXTILE_TELEGRAM_RELAY_URL` production secret.

The financial service automatically detects Google Apps Script URLs and sends a
signed JSON envelope. Existing standard relay URLs remain backward compatible.
