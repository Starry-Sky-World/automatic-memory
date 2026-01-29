# Repository Guidelines

This guide helps new agents contribute to DeepSeek2API safely and consistently. Review it before opening a pull request.

## Project Structure & Module Organization
`app.py` hosts the FastAPI entrypoint, DeepSeek account rotation, and OpenAI-compatible endpoints. Runtime assets stay beside it: `config.json` for credentials, `requirements.txt` for Python deps, and `version.txt` for release metadata. HTML responses render from `templates/`, while deployment manifests (`Dockerfile`, `docker-compose.yml`, `vercel.json`) live in the root. Keep new modules grouped by responsibility and co-locate helpers with the feature they serve.

## Build, Test, and Development Commands
Create an isolated environment before hacking: `python -m venv .venv && source .venv/bin/activate`. Install dependencies with `pip install -r requirements.txt`. Run the API locally via `uvicorn app:app --reload --host 0.0.0.0 --port 5001`. For container smoke tests, use `docker compose up -d` and `docker logs -f deepseek2api` to watch streaming output. Snapshot dependencies after upgrades with `pip freeze > requirements.txt`.

## Coding Style & Naming Conventions
Follow standard PEP 8 rules (4-space indents, 120-char soft limit) and mirror existing logger usage. Modules and functions use `snake_case`; classes use `PascalCase`; constants stay in `UPPER_SNAKE_CASE`. Keep HTTP paths and query keys aligned with the official OpenAI API schema. When adding complex logic, include concise docstrings or inline comments describing the protocol nuance being handled.

## Testing Guidelines
Add or extend pytest suites under a new `tests/` directory; name files `test_<feature>.py` and functions `test_<behavior>`. Exercise both streaming and non-streaming paths with `httpx.AsyncClient` fixtures, and include regression cases for account failover. Run `pytest -q` before pushing, and document any manual steps (e.g., mock DeepSeek responses) in the PR description.

## Commit & Pull Request Guidelines
Adopt the existing conventional prefixes (`feat:`, `fix:`, `docs:`, `chore:`) and keep subjects under 72 characters. Group related changes into a single commit when feasible. Every PR should describe the motivation, summarize testing, and call out config or deployment impacts. Link relevant issues, attach screenshots for template tweaks, and request review from a maintainer familiar with the touched module.

## Security & Configuration Tips
Never commit real account credentials; ship redacted samples only. When altering `config.json`, update companion docs and remind users to rotate tokens. Validate inbound headers and payloads before forwarding to DeepSeek, and prefer environment variables over plaintext when adding new secrets or toggles.
