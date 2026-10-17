# EVREN

EVREN is an AI platform run by Türkiye's Presidency of Defence Industries (SSB) at `evren.ssyz.org.tr`. Its LLM gateway serves open-weight models from GPUs hosted in Türkiye through an OpenAI-compatible API.

Everything below was observed on 2026-10-11 unless marked otherwise. Re-check before relying on it.

## Access

- Sign-in is through e-Devlet (national e-government identity); one account per national ID.
- API keys are created on the site's API keys page and look like `evren_llm_...`. A key is shown once.
- The terms forbid sharing keys. Each person needs their own account and key.
- Going by the site's code (2026-10-17), a key has settings of its own: whether models think when the request does not say (`thinking_enabled`), the models it may use (`allowed_models`, `allow_auto`), a request rate (`rpm_limit`), a daily token cap, and an expiry date. A rotated key keeps working for a grace period.

## API

- Base URL: `https://evren-llmapi.ssyz.org.tr/v1`
- Authentication: `X-API-Key: <key>` or `Authorization: Bearer <key>`. Ülgen sends the Bearer form. A request whose two headers carry different keys is rejected with `400` and code `bad_request` ("Authorization ve X-API-Key farklı anahtarlar içeriyor").
- **An Anthropic-style endpoint (2026-10-17).** `POST /v1/messages` and `POST /v1/messages/count_tokens` now answer; on 2026-10-11 they were 404. They are in the published [OpenAPI document](https://evren-llmapi.ssyz.org.tr/openapi.json), not in the site's documentation. With Claude Code 2.1.302 pointed at them, `deepseek-v4.1-flash`, `glm-5.3`, `gemma-4-31b` and `mimo-v2.6-pro` answered, reading a file with a tool and thinking. `qwen3.8-flash-next` refused Claude Code's default effort ("Unexpected reasoning effort high"), and `qwen3-vl-30b` refused the `thinking` field. Ülgen keeps its translator, which handles both models.

| Method | Path | Purpose |
|---|---|---|
| POST | `/v1/chat/completions` | Chat, including streaming |
| POST | `/v1/responses` | OpenAI Responses format |
| POST | `/v1/messages`, `/v1/messages/count_tokens` | Anthropic Messages (2026-10-17, see above) |
| GET | `/v1/models` | Models and prices for the account |
| GET | `/v1/quota` | Daily and 5-minute window usage; costs no tokens |
| POST | `/v1/embeddings` | Embeddings |
| POST | `/v1/rerank` | Reranking (Cohere/Jina-style contract) |
| POST | `/v1/ocr` | Document OCR |
| POST | `/v1/audio/transcriptions` | Speech to text |
| GET | `/v1/terms/status` | Whether the current terms are accepted |
| GET | `/v1/terms/text` | The terms text |
| POST | `/v1/terms/accept` | Accept, with body `{"version": N}` |

The OpenAPI document (2026-10-17) also lists `/v1/completions`, `/v1/systemone` and `/v1/systemone/batch` (the decision models), an upload flow under `/v1/media`, `GET /v1/requests/{id}`, and `GET`/`DELETE /v1/responses/{id}`. None of them has been tried.

## Terms gate

Until the account accepts the current terms, every call returns `403` with `code: "terms_not_accepted"` and the required version. Acceptance is once per account and is requested again only for a material new version.

The recorded shapes:

- `GET /v1/terms/status` → `{"current_version":1,"is_material":true,"doc_path":"docs/legal/LLM_TERMS_OF_SERVICE_v1.md","accepted":false}`.
- `GET /v1/terms/text` → `{"version":1,"doc_path":"...","content":"<Markdown, about 3,500 characters, in Turkish>"}`.
- `POST /v1/terms/accept` with `{"version":1}` → `200 {"accepted_version":1,"accepted_at":"2026-10-11T18:08:07+00:00"}`. A wrong version → `400` with code `terms_version_mismatch`.

## Models

| Model | Task | Context | Max output | Capabilities | `reasoning_effort` (2026-10-17) |
|---|---|---|---|---|---|
| `deepseek-v4.1-flash` | chat | 1,048,576 | 65,536 | tools, thinking, image input, structured output | yes |
| `deepseek-v4-flash` | chat | 1,048,576 | 16,384 | tools, thinking, structured output | yes |
| `mimo-v2.6-pro` | chat | 1,048,576 | 16,384 | tools, thinking, image input, structured output | listed: none, minimal, low, medium, high, xhigh |
| `glm-5.3` | chat | 524,288 | 16,384 | tools, thinking, structured output | yes |
| `qwen3.8-flash-next` | chat | 262,144 | 16,384 | tools, thinking, image input, structured output | yes |
| `gemma-4-31b` | chat | 262,144 | 16,384 | tools, image input, structured output; thinking listed since 2026-10-17 | no |
| `qwen3-vl-30b` | vision_chat | 262,144 | 16,384 | tools, image input, structured output | no |
| `auto` | routing mode | 32,768 | 16,384 | the gateway picks a model; the response names it | no |

Also served: `dots-ocr`, `deepseek-ocr-2`, `qwen3-embedding-8b`, `qwen3-reranker-8b`, `qwen3-asr-1.7b`, `qwen3-guard-4b`, and the decision models `laya` and `clef`.

The `task` field holds `chat` or `vision_chat` for chat models.

Models may be addressed by short id (`glm-5.3`) or repository name (`zai/glm-5.3-fp8`).

## Reasoning effort

Measured on 2026-10-17 with chat completions. `/v1/models` lists the capability as `capabilities.reasoning_effort`, with `reasoning_effort_values` and `reasoning_effort_aliases` where a model has them.

| Model | Refuses |
|---|---|
| `deepseek-v4.1-flash`, `deepseek-v4-flash`, `glm-5.3` | nothing of none, minimal, low, medium, high, xhigh, max |
| `mimo-v2.6-pro` | `max` |
| `qwen3.8-flash-next` | `minimal`, `max` ("Supported types are xhigh (default), medium, and low"; `high` is taken as `xhigh`) |
| `gemma-4-31b`, `qwen3-vl-30b` | the field itself: `400`, "model '…' 'reasoning_effort' parametresini desteklemiyor" |

`none` turns reasoning off. Any other value is refused by the gateway's own check. A `max_tokens` above a model's output limit is accepted (32,000 for `glm-5.3`).

## Limits and price

- Free until 2026-11-01: every model is listed at 0 CR. After that, usage is billed in platform credits (CR); the per-token price had not been published.
- Daily quota: 15,000,000 tokens per account, across all models and all keys, resetting at 03:00 Istanbul time. The site states this quota is valid until 2026-11-01.
- Prompt tokens served from the prefix cache count at 3% toward the quota. Keeping the start of the prompt stable makes the quota go much further.
- The site states 500,000 tokens per minute and 32 concurrent requests. Response headers showed `x-ratelimit-limit-requests: 1200` and `x-ratelimit-limit-tokens: 2500000`.
- `429` means the account quota; `503` means the server is busy.
- **2026-10-17:** still no per-token price. The site's public statistics (`https://api.ssyz.org.tr/api/v1/public/llm-stats`) give `daily_token_quota_until: 2026-11-01T03:00:00+03:00`, and nothing says what follows. Going by the site's own code, not tried: credits come from rewards for contributions (labelling, uploading data, publishing models and datasets; at most 100 CR a day), from requests an administrator reviews, from campaign coupons and from grants to an organisation. The platform's web API answers `402` with `INSUFFICIENT_CREDITS`; what the LLM API answers to an account without credits has not been seen.

## Behaviour worth handling

- **Quota headers on every response:** `x-evren-daily-limit-tokens`, `x-evren-daily-remaining-tokens`, `x-evren-daily-reset` (Unix time), `x-evren-credits-remaining`, `x-evren-routed-model`.
- **Extra usage fields:** `usage.evren` carries `credits_held_cr`, `credits_remaining_cr` and `routed_model`.
- **Reasoning:** thinking models return it in `message.reasoning`. Thinking and the final answer share the `max_tokens` budget; the site recommends at least 2,048 and suggests 4,096.
- **Keep-alive:** during long thinking the server sends a comment line (starting with `:`) every 10 seconds on the event stream. A parser must skip these.
- **Images:** base64 data URLs only, at most 4 per request, 4 MiB each and 8 MiB in total. Remote image URLs are rejected with `400`.

## Data handling

The terms state that prompt and completion text is processed under Turkish data-protection law (KVKK) and mention a corpus-consent mechanism and a right to be forgotten; the details are in documents that were not available. The service is provided as is, with no service-level commitment. Do not send confidential material.

## Sign-in flow in Ülgen

The e-Devlet step cannot be skipped. Everything after it is one command:

1. `ulgen login` opens EVREN's own API keys page in the browser (unless `--no-browser` is given) and asks for the key; when stdin is a terminal the key is read without echo.
2. Ülgen validates the key with `GET /v1/terms/status`; a rejected key stores nothing.
3. If the terms are not yet accepted, Ülgen prints them and, only after the user agrees, records acceptance through `POST /v1/terms/accept`.
4. Ülgen stores the key in the operating system's credential store, then prints the models and the remaining daily quota (the same report as `ulgen status`).

Sign-in without copy and paste would need support from EVREN. The site's two candidate pages were examined on 2026-10-17, and neither is such support. `sso-login` is where e-Devlet's sign-in (OpenID Connect, client `evren`) returns to the site, at a fixed address. `env-handoff` moves an administrator from the site to its test environment.

## Existing community bridges

Not reviewed; listed for reference.

- `github.com/berkaycari/evren-codex-proxy` — EVREN for OpenAI Codex CLI
- `github.com/bykemalh/evren-llm-opencode` — EVREN provider setup for opencode
- `github.com/mustafacoshkun/evren` — unofficial EVREN command-line client
- `github.com/macbyclp/evren-fleet` — a Claude Code skill that uses EVREN workers
- `github.com/speker/evren-sdk` — an SDK the site's own sample code imports; it expects `402` or `insufficient_quota` for missing credits and sends an `Idempotency-Key`
- `github.com/Kscl1/evren-bridge` — a local proxy that tells the per-minute `429` from the daily one; it rotates several keys, which the terms forbid
- `github.com/heimdilon/kalfa` — its `research/evren-probe` notes that the last chunk of a stream carries no `usage.evren`
- `github.com/s-kali/evren-claude-code`, `github.com/acarbayk/Evren-Claude-Test-` — Claude Code on EVREN through LiteLLM

## Not verified

- Whether the API works with a key from outside Türkiye. Without a key, probes from nine countries in Europe and Asia got `401` on 2026-10-17 (through check-host.net), so nothing blocks a foreign address before the key is checked.
- Prices and quotas after 2026-11-01.
