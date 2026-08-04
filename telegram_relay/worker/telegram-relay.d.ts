export function handleTelegramRelay(
  request: Request,
  env: { TELEGRAM_RELAY_TOKEN?: string },
  fetchImpl?: typeof fetch,
): Promise<Response | null>;
