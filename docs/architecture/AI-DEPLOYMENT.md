# Secure AI analysis deployment

The deterministic management report works without any external AI service. The
optional executive narrative is generated only by the financial API container.
The browser never receives the provider API key.

## Server variables

Configure these values only in the VPS deployment environment or GitHub
environment secrets:

```dotenv
VIORA_AI_ENABLED=true
OPENAI_API_KEY=replace_on_the_server
VIORA_AI_MODEL=gpt-5.6-luna
VIORA_AI_BASE_URL=https://api.openai.com/v1
VIORA_AI_MONTHLY_REQUEST_LIMIT=100
```

`VIORA_AI_ENABLED=false` is the safe default. `OPENAI_API_KEY` must never be
committed. The monthly limit is enforced per company.

## Data boundary and billing

Only aggregate figures, short deterministic priorities, and declared data gaps
are sent to the provider. Passwords, authentication tokens, raw invoices, and
database files are excluded. Every attempt is recorded in
`ai_analysis_usage` with company, user, model, token counts, status, and
timestamp. Prompts and business payloads are not stored in the usage table.

AI output is advisory. It cannot send messages, change prices, post accounting
entries, or execute another business action without a separate user-approved
workflow.
