# AGENTS.md

## Mission

This repository follows the Viora cloud delivery path: Codex Cloud prepares and verifies changes, GitHub stores the approved source, and the production workflow deploys an immutable release to the VPS.

## Working rules

- Make changes on a branch and present them for review. Production is updated only after the user approves and the change reaches `main`.
- Before finishing, run the relevant tests and production build documented in this repository. Report any test that could not run.
- Never commit passwords, tokens, private keys, `.env` files, databases, user uploads, backups, or generated runtime data.
- Keep production configuration compatible with the files under `deploy/production`.
- Do not delete or recreate production volumes, databases, uploads, or runtime directories.
- Database changes must be backward compatible and include a safe migration and rollback plan.
- Prefer small, reviewable changes. Preserve Persian/English behavior and existing public URLs unless the task explicitly changes them.
- A task is done only when tests pass, the diff is explained in plain language, and deployment/rollback implications are stated.

