---
title: EVREN
description: EVREN nedir; API'si, koşulların kabulü, modeller, düşünme düzeyi, sınırlar ve fiyat, gözlemlendiği tarihlerle; Ülgen'in nasıl giriş yaptığı.
---

EVREN, Savunma Sanayii Başkanlığı'nın (SSB) `evren.ssyz.org.tr` adresinde işlettiği bir yapay zekâ platformudur. Platformun LLM ağ geçidi, Türkiye'de barındırılan GPU'larda çalışan açık ağırlıklı modelleri OpenAI uyumlu bir API üzerinden sunar.

Aksi belirtilmedikçe aşağıdakilerin hepsi 2026-10-11'de gözlemlendi. Bunlara dayanmadan önce yeniden denetleyin.

## Erişim

- Giriş e-Devlet ile yapılır; her kimlik numarasına tek hesap açılır.
- API anahtarları sitenin API anahtarları sayfasında oluşturulur ve `evren_llm_...` biçimindedir. Bir anahtar yalnızca bir kez gösterilir.
- Koşullar anahtar paylaşmayı yasaklar. Herkesin kendi hesabı ve kendi anahtarı olmalıdır.
- Sitenin koduna göre (2026-10-17) her anahtarın kendi ayarları vardır: istek belirtmediğinde modellerin düşünüp düşünmeyeceği (`thinking_enabled`), kullanabileceği modeller (`allowed_models`, `allow_auto`), bir istek hızı (`rpm_limit`), günlük bir token sınırı ve bir son kullanma tarihi. Yenilenen bir anahtar bir geçiş süresi boyunca çalışmayı sürdürür.

## API

- Temel URL: `https://evren-llmapi.ssyz.org.tr/v1`
- Kimlik doğrulama: `X-API-Key: <anahtar>` ya da `Authorization: Bearer <anahtar>`. Ülgen Bearer biçimini gönderir. İki başlığı farklı anahtarlar taşıyan bir istek `400` ve `bad_request` koduyla reddedilir ("Authorization ve X-API-Key farklı anahtarlar içeriyor").
- **Anthropic biçiminde bir uç nokta (2026-10-17).** `POST /v1/messages` ve `POST /v1/messages/count_tokens` artık yanıt veriyor; 2026-10-11'de 404 dönüyorlardı. Yayımlanan [OpenAPI belgesinde](https://evren-llmapi.ssyz.org.tr/openapi.json) yer alırlar, sitenin belgelerinde yer almazlar. Claude Code 2.1.302 bunlara yöneltildiğinde `deepseek-v4.1-flash`, `glm-5.3`, `gemma-4-31b` ve `mimo-v2.6-pro` yanıt verdi; bir araçla dosya okudular ve düşündüler. `qwen3.8-flash-next`, Claude Code'un varsayılan düşünme düzeyini reddetti ("Unexpected reasoning effort high"); `qwen3-vl-30b` ise `thinking` alanını reddetti. Ülgen, bu iki modelle de çalışan çeviricisini korur.

| Yöntem | Yol | Amaç |
|---|---|---|
| POST | `/v1/chat/completions` | Sohbet, akış (streaming) dahil |
| POST | `/v1/responses` | OpenAI Responses biçimi |
| POST | `/v1/messages`, `/v1/messages/count_tokens` | Anthropic Messages (2026-10-17, yukarıya bakın) |
| GET | `/v1/models` | Hesabın modelleri ve fiyatları |
| GET | `/v1/quota` | Günlük ve 5 dakikalık pencerede kullanım; token harcamaz |
| POST | `/v1/embeddings` | Gömmeler (embeddings) |
| POST | `/v1/rerank` | Yeniden sıralama (Cohere/Jina tarzı sözleşme) |
| POST | `/v1/ocr` | Belge OCR'ı |
| POST | `/v1/audio/transcriptions` | Konuşmadan metne |
| GET | `/v1/terms/status` | Güncel koşulların kabul edilip edilmediği |
| GET | `/v1/terms/text` | Koşulların metni |
| POST | `/v1/terms/accept` | `{"version": N}` gövdesiyle kabul |

OpenAPI belgesi (2026-10-17) ayrıca `/v1/completions`, `/v1/systemone` ve `/v1/systemone/batch` (karar modelleri), `/v1/media` altında bir yükleme akışı, `GET /v1/requests/{id}` ve `GET`/`DELETE /v1/responses/{id}` uç noktalarını listeler. Bunların hiçbiri denenmedi.

## Koşulların kabulü

Hesap güncel koşulları kabul edene kadar her çağrı, `code: "terms_not_accepted"` ve istenen sürümle birlikte `403` döner. Kabul hesap başına bir kez yapılır ve yalnızca esaslı bir değişiklik içeren yeni bir sürümde yeniden istenir.

Kaydedilen yanıt biçimleri:

- `GET /v1/terms/status` → `{"current_version":1,"is_material":true,"doc_path":"docs/legal/LLM_TERMS_OF_SERVICE_v1.md","accepted":false}`.
- `GET /v1/terms/text` → `{"version":1,"doc_path":"...","content":"<Markdown, yaklaşık 3.500 karakter, Türkçe>"}`.
- `{"version":1}` ile `POST /v1/terms/accept` → `200 {"accepted_version":1,"accepted_at":"2026-10-11T18:08:07+00:00"}`. Yanlış bir sürüm → `terms_version_mismatch` koduyla `400`.

## Modeller

| Model | Görev | Bağlam | En büyük çıktı | Yetenekler | `reasoning_effort` (2026-10-17) |
|---|---|---|---|---|---|
| `deepseek-v4.1-flash` | chat | 1.048.576 | 65.536 | araçlar, düşünme, görsel girdi, yapılandırılmış çıktı | evet |
| `deepseek-v4-flash` | chat | 1.048.576 | 16.384 | araçlar, düşünme, yapılandırılmış çıktı | evet |
| `mimo-v2.6-pro` | chat | 1.048.576 | 16.384 | araçlar, düşünme, görsel girdi, yapılandırılmış çıktı | listelenen: none, minimal, low, medium, high, xhigh |
| `glm-5.3` | chat | 524.288 | 16.384 | araçlar, düşünme, yapılandırılmış çıktı | evet |
| `qwen3.8-flash-next` | chat | 262.144 | 16.384 | araçlar, düşünme, görsel girdi, yapılandırılmış çıktı | evet |
| `gemma-4-31b` | chat | 262.144 | 16.384 | araçlar, görsel girdi, yapılandırılmış çıktı; düşünme 2026-10-17'den beri listede | hayır |
| `qwen3-vl-30b` | vision_chat | 262.144 | 16.384 | araçlar, görsel girdi, yapılandırılmış çıktı | hayır |
| `auto` | yönlendirme kipi | 32.768 | 16.384 | modeli ağ geçidi seçer; yanıt seçilen modelin adını verir | hayır |

Ayrıca sunulanlar: `dots-ocr`, `deepseek-ocr-2`, `qwen3-embedding-8b`, `qwen3-reranker-8b`, `qwen3-asr-1.7b`, `qwen3-guard-4b` ve karar modelleri `laya` ile `clef`.

Sohbet modellerinde `task` alanı `chat` ya da `vision_chat` değerini taşır.

Modeller kısa kimlikleriyle (`glm-5.3`) ya da depo adlarıyla (`zai/glm-5.3-fp8`) belirtilebilir.

## Düşünme düzeyi

2026-10-17'de chat completions ile ölçüldü. `/v1/models` bu yeteneği `capabilities.reasoning_effort` olarak, modelde varsa `reasoning_effort_values` ve `reasoning_effort_aliases` ile birlikte listeler.

| Model | Reddettiği |
|---|---|
| `deepseek-v4.1-flash`, `deepseek-v4-flash`, `glm-5.3` | hiçbirini (none, minimal, low, medium, high, xhigh, max) |
| `mimo-v2.6-pro` | `max` |
| `qwen3.8-flash-next` | `minimal`, `max` ("Supported types are xhigh (default), medium, and low"; `high`, `xhigh` olarak alınır) |
| `gemma-4-31b`, `qwen3-vl-30b` | alanın kendisini: `400`, "model '…' 'reasoning_effort' parametresini desteklemiyor" |

`none` düşünmeyi kapatır. Bu değerlerin dışındaki her değeri ağ geçidinin kendi denetimi reddeder. Bir modelin çıktı sınırının üstündeki bir `max_tokens` kabul edilir (`glm-5.3` için 32.000).

## Sınırlar ve fiyat

- 2026-11-01'e kadar ücretsiz: her model 0 CR olarak listelenir. Sonrasında kullanım platform kredisiyle (CR) ücretlendirilir; token başına fiyat yayımlanmamıştı.
- Günlük kota: hesap başına, bütün modellerde ve bütün anahtarlarda toplam 15.000.000 token; İstanbul saatiyle 03:00'te sıfırlanır. Site bu kotanın 2026-11-01'e kadar geçerli olduğunu belirtiyor.
- Ön ek önbelleğinden (prefix cache) karşılanan istem tokenları kotaya %3 oranında sayılır. İstemin başını sabit tutmak kotayı çok daha uzağa götürür.
- Site dakikada 500.000 token ve 32 eşzamanlı istek belirtiyor. Yanıt başlıklarında `x-ratelimit-limit-requests: 1200` ve `x-ratelimit-limit-tokens: 2500000` görüldü.
- `429` hesabın kotası doldu, `503` sunucu meşgul demektir.
- **2026-10-17:** token başına fiyat hâlâ yok. Sitenin herkese açık istatistikleri (`https://api.ssyz.org.tr/api/v1/public/llm-stats`) `daily_token_quota_until: 2026-11-01T03:00:00+03:00` veriyor; sonrasında ne olacağını söyleyen bir şey yok. Sitenin kendi koduna göre (denenmedi): kredi, katkılar için verilen ödüllerden (etiketleme, veri yükleme, model ve veri kümesi yayımlama; günde en çok 100 CR), bir yöneticinin incelediği taleplerden, kampanya kuponlarından ve bir kuruluşa yapılan tahsislerden gelir. Platformun web API'si `INSUFFICIENT_CREDITS` ile `402` yanıtı veriyor; LLM API'sinin kredisi olmayan bir hesaba ne yanıt verdiği görülmedi.

## Ele alınması gereken davranışlar

- **Her yanıtta kota başlıkları:** `x-evren-daily-limit-tokens`, `x-evren-daily-remaining-tokens`, `x-evren-daily-reset` (Unix zamanı), `x-evren-credits-remaining`, `x-evren-routed-model`.
- **Ek kullanım alanları:** `usage.evren`, `credits_held_cr`, `credits_remaining_cr` ve `routed_model` alanlarını taşır.
- **Düşünme:** düşünen modeller bunu `message.reasoning` içinde döndürür. Düşünme ve son yanıt aynı `max_tokens` bütçesini paylaşır; site en az 2.048 öneriyor ve 4.096'yı tavsiye ediyor.
- **Bağlantıyı canlı tutma:** uzun düşünme sırasında sunucu olay akışına her 10 saniyede bir yorum satırı (`:` ile başlayan) gönderir. Ayrıştırıcı bunları atlamalıdır.
- **Görseller:** yalnızca base64 veri URL'leri; istek başına en çok 4 tane, her biri en çok 4 MiB, toplamda en çok 8 MiB. Uzak görsel adresleri `400` ile reddedilir.

## Verilerin işlenmesi

Koşullar, istem ve yanıt metinlerinin Kişisel Verilerin Korunması Kanunu'na (KVKK) göre işlendiğini belirtir; bir derlem onayı mekanizmasından ve unutulma hakkından söz eder. Ayrıntılar erişilemeyen belgelerdedir. Hizmet olduğu gibi sunulur; bir hizmet düzeyi taahhüdü yoktur. Gizli bilgi göndermeyin.

## Ülgen'de giriş akışı

E-Devlet adımı atlanamaz. Ondan sonrası tek komuttur:

1. `ulgen login`, EVREN'in kendi API anahtarları sayfasını tarayıcıda açar (`--no-browser` verilmediyse) ve anahtarı sorar; standart girdi bir terminalse anahtar ekranda görünmeden okunur.
2. Ülgen anahtarı `GET /v1/terms/status` ile doğrular; reddedilen bir anahtar hiçbir şey kaydetmez.
3. Koşullar henüz kabul edilmediyse Ülgen metni yazdırır ve kabulü ancak kullanıcı onayladıktan sonra `POST /v1/terms/accept` ile kaydeder.
4. Ülgen anahtarı işletim sisteminin kimlik bilgisi deposunda saklar, ardından modelleri ve kalan günlük kotayı yazdırır (`ulgen status` ile aynı rapor).

Kopyala-yapıştır olmadan giriş, EVREN'in desteğini gerektirir. Sitenin aday iki sayfası 2026-10-17'de incelendi; ikisi de böyle bir destek değil. `sso-login`, e-Devlet girişinin (OpenID Connect, istemci `evren`) siteye sabit bir adreste döndüğü yerdir. `env-handoff` ise bir yöneticiyi siteden test ortamına geçirir.

## Topluluğun mevcut köprüleri

İncelenmedi; başvuru için listelenmiştir.

- `github.com/berkaycari/evren-codex-proxy`: OpenAI Codex CLI için EVREN
- `github.com/bykemalh/evren-llm-opencode`: opencode için EVREN sağlayıcısı kurulumu
- `github.com/mustafacoshkun/evren`: resmî olmayan bir EVREN komut satırı istemcisi
- `github.com/macbyclp/evren-fleet`: EVREN işçilerini kullanan bir Claude Code becerisi
- `github.com/speker/evren-sdk`: sitenin kendi örnek kodunun içe aktardığı bir SDK; eksik kredi için `402` ya da `insufficient_quota` bekler ve bir `Idempotency-Key` gönderir
- `github.com/Kscl1/evren-bridge`: dakikalık `429` ile günlük `429`'u ayırt eden yerel bir vekil; birden çok anahtarı sırayla kullanır, koşullar bunu yasaklar
- `github.com/heimdilon/kalfa`: `research/evren-probe` notları, bir akışın son parçasının `usage.evren` taşımadığını söyler
- `github.com/s-kali/evren-claude-code`, `github.com/acarbayk/Evren-Claude-Test-`: LiteLLM üzerinden EVREN'de Claude Code

## Doğrulanmayanlar

- Türkiye dışından, bir anahtarla API'nin kullanılıp kullanılamadığı. 2026-10-17'de Avrupa ve Asya'daki dokuz ülkeden anahtarsız yapılan denemeler (check-host.net üzerinden) `401` aldı; yani anahtar denetlenmeden önce yabancı bir adresi engelleyen bir şey yok.
- 2026-11-01'den sonraki fiyatlar ve kotalar.
