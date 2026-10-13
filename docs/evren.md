# EVREN

EVREN is an AI platform run by Türkiye's Presidency of Defence Industries (SSB) at `evren.ssyz.org.tr`. Its LLM gateway serves open-weight models from GPUs hosted in Türkiye through an OpenAI-compatible API.

Everything below was observed on 2026-10-11 unless marked otherwise. Re-check before relying on it.

## Access

- Sign-in is through e-Devlet (national e-government identity); one account per national ID.
- API keys are created on the site's API keys page and look like `evren_llm_...`. A key is shown once.
- The terms forbid sharing keys. Each person needs their own account and key.

## API

- Base URL: `https://evren-llmapi.ssyz.org.tr/v1`
- Authentication: `X-API-Key: <key>` or `Authorization: Bearer <key>`. Ülgen sends the Bearer form. A request whose two headers carry different keys is rejected with `400` and code `bad_request` ("Authorization ve X-API-Key farklı anahtarlar içeriyor").
- There is no Anthropic-style `/v1/messages` endpoint (404).

| Method | Path | Purpose |
|---|---|---|
| POST | `/v1/chat/completions` | Chat, including streaming |
| POST | `/v1/responses` | OpenAI Responses format |
| GET | `/v1/models` | Models and prices for the account |
| GET | `/v1/quota` | Daily and 5-minute window usage; costs no tokens |
| POST | `/v1/embeddings` | Embeddings |
| POST | `/v1/rerank` | Reranking (Cohere/Jina-style contract) |
| POST | `/v1/ocr` | Document OCR |
| POST | `/v1/audio/transcriptions` | Speech to text |
| GET | `/v1/terms/status` | Whether the current terms are accepted |
| GET | `/v1/terms/text` | The terms text |
| POST | `/v1/terms/accept` | Accept, with body `{"version": N}` |

## Terms gate

Until the account accepts the current terms, every call returns `403` with `code: "terms_not_accepted"` and the required version. Acceptance is once per account and is requested again only for a material new version.

The recorded shapes:

- `GET /v1/terms/status` → `{"current_version":1,"is_material":true,"doc_path":"docs/legal/LLM_TERMS_OF_SERVICE_v1.md","accepted":false}`.
- `GET /v1/terms/text` → `{"version":1,"doc_path":"...","content":"<Markdown, about 3,500 characters, in Turkish>"}`.
- `POST /v1/terms/accept` with `{"version":1}` → `200 {"accepted_version":1,"accepted_at":"2026-10-11T18:08:07+00:00"}`. A wrong version → `400` with code `terms_version_mismatch`.

## Models

| Model | Task | Context | Max output | Capabilities |
|---|---|---|---|---|
| `deepseek-v4.1-flash` | chat | 1,048,576 | 65,536 | tools, thinking, image input, structured output |
| `deepseek-v4-flash` | chat | 1,048,576 | 16,384 | tools, thinking, structured output |
| `mimo-v2.6-pro` | chat | 1,048,576 | 16,384 | tools, thinking, image input, structured output |
| `glm-5.3` | chat | 524,288 | 16,384 | tools, thinking, structured output |
| `qwen3.8-flash-next` | chat | 262,144 | 16,384 | tools, thinking, image input, structured output |
| `gemma-4-31b` | chat | 262,144 | 16,384 | tools, image input, structured output |
| `qwen3-vl-30b` | vision_chat | 262,144 | 16,384 | tools, image input, structured output |
| `auto` | routing mode | 32,768 | 16,384 | the gateway picks a model; the response names it |

Also served: `dots-ocr`, `deepseek-ocr-2`, `qwen3-embedding-8b`, `qwen3-reranker-8b`, `qwen3-asr-1.7b`, `qwen3-guard-4b`, and the decision models `laya` and `clef`.

The `task` field holds `chat` or `vision_chat` for chat models.

Models may be addressed by short id (`glm-5.3`) or repository name (`zai/glm-5.3-fp8`).

## Limits and price

- Free until 2026-11-01: every model is listed at 0 CR. After that, usage is billed in platform credits (CR); the per-token price had not been published.
- Daily quota: 15,000,000 tokens per account, across all models and all keys, resetting at 03:00 Istanbul time. The site states this quota is valid until 2026-11-01.
- Prompt tokens served from the prefix cache count at 3% toward the quota. Keeping the start of the prompt stable makes the quota go much further.
- The site states 500,000 tokens per minute and 32 concurrent requests. Response headers showed `x-ratelimit-limit-requests: 1200` and `x-ratelimit-limit-tokens: 2500000`.
- `429` means the account quota; `503` means the server is busy.

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

Sign-in without copy and paste would need support from EVREN. The site has pages named `env-handoff` and `sso-login` that have not been examined.

## Existing community bridges

Not reviewed; listed for reference.

- `github.com/berkaycari/evren-codex-proxy` — EVREN for OpenAI Codex CLI
- `github.com/bykemalh/evren-llm-opencode` — EVREN provider setup for opencode
- `github.com/mustafacoshkun/evren` — unofficial EVREN command-line client
- `github.com/macbyclp/evren-fleet` — a Claude Code skill that uses EVREN workers

## Not verified

- Whether the API is reachable from outside Türkiye.
- Prices and quotas after 2026-11-01.
