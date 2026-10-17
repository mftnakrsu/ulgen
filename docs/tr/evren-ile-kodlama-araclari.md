# EVREN'i kodlama araçlarınızda kullanma

## Bu nedir

Ülgen, Savunma Sanayii Başkanlığı'nın (SSB) işlettiği EVREN model API'sini,
zaten kullandığınız açık kaynak kodlama araçlarına bağlar.
EVREN'e e-Devlet üzerinden girer, API anahtarınızı işletim sisteminin kimlik
bilgisi deposunda saklar, kullanım koşullarının kabulünü kaydeder ve kalan
günlük kotayı izler. Ayrıca seçtiğiniz aracın ayar dosyasına EVREN sağlayıcısını
yazar.

Ülgen bu araçların yerini almaz ve yeni bir kodlama ajanı değildir: sizin
kullandığınız aracı ayarlar, anahtarı ortam değişkeni olarak vererek başlatır ve
gerisini araca bırakır.

## Neye ihtiyacınız var

- Bir EVREN hesabı. Giriş e-Devlet ile yapılır; her kimlik numarasına tek
  hesap açılır. API anahtarları sitenin API anahtarları sayfasında oluşturulur ve
  `evren_llm_...` biçimindedir. Koşullar anahtar paylaşmayı yasaklar; herkesin
  kendi hesabı ve kendi anahtarı olmalıdır.
- Şu dört araçtan biri: Qwen Code, opencode, Codex CLI, Claude Code. Ülgen bu
  dördünü destekler.

Adını verdiğiniz bir araç kurulu değilse, `ulgen setup` size kurulum satırını
yazdırır. Bu dört araç npm ile kurulur:

```sh
npm install -g @qwen-code/qwen-code       # Qwen Code
npm install -g opencode-ai                # opencode
npm install -g @openai/codex              # Codex CLI
npm install -g @anthropic-ai/claude-code  # Claude Code
```

Sürümler ve sistem gereksinimleri için her aracın kendi belgelerine bakın.

## Ülgen'i edinme

Linux ve macOS:

```sh
curl -fsSL https://raw.githubusercontent.com/mftnakrsu/ulgen/main/scripts/install.sh | sh
```

Windows (PowerShell):

```powershell
irm https://raw.githubusercontent.com/mftnakrsu/ulgen/main/scripts/install.ps1 | iex
```

Betikler indirdikleri dosyanın sağlama toplamını denetler, `sudo` çalıştırmaz
ve parola sormaz. Pardus için `.deb` paketi, kaynaktan derleme ve indirilen
dosyayı elle doğrulama [kurulum belgesinde](../install.md) anlatılır.

## En kısa yol

Terminalde yalnızca şunu yazın:

```sh
ulgen
```

Ülgen sırayla şunları yapar ve her adımda ne yaptığını söyler:

1. Giriş yapılmamışsa giriş yaptırır (aşağıdaki "Giriş yapma" bölümündeki
   adımların aynısı).
2. Kurulu araçları bulur. Yalnızca biri kuruluysa onu seçer; birden fazlaysa
   hangisini istediğinizi sorar. Hiçbiri kurulu değilse kurulum satırlarını
   yazdırır.
3. Seçilen araç henüz EVREN için ayarlanmamışsa ayarlar (`ulgen setup` ile
   aynı iş; değişen dosyaların önce yedeği alınır).
4. Aracı başlatır.

Sonraki çalıştırmalarda `ulgen` soru sormadan son kullandığınız aracı başlatır.
Başka bir araca geçmek için aracın adını yazın; argümanlar araca iletilir:

```sh
ulgen opencode
ulgen qwen -p "bu depoyu özetle"
```

Aşağıdaki bölümler bu adımların her birini tek tek, bütün seçenekleriyle
anlatır. Rehber yeni bir denetim eklemez ve hiçbirini atlamaz: `ulgen login`,
`ulgen setup` ve `ulgen run` komutlarını sizin yerinize sırayla çağırır.

## Giriş yapma

E-Devlet adımı atlanamaz; ondan sonrası tek komuttur.

```sh
ulgen login
```

`ulgen login` şunları yapar:

1. EVREN'in kendi API anahtarları sayfasını tarayıcıda açar ve şunu yazar:
   `EVREN'e e-Devlet ile giriş yapıp burada bir API anahtarı oluşturun:`.
   Tarayıcıyı açmamak için `ulgen login --no-browser` kullanın; bu seçenek
   adresi yalnızca yazdırır.
2. Anahtarı sorar. Yapıştırdığınız anahtar ekranda görünmez
   (`API anahtarını yapıştırın (ekranda görünmez):`). Girişi gizleyemeyen bir
   terminalde (örneğin Windows'ta Git Bash penceresinde) anahtarın görüneceğini
   açıkça söyler ve gizlemek için komutu `winpty ulgen login` olarak
   başlatmanızı önerir.
3. Anahtarı EVREN'e sorarak doğrular. Reddedilen bir anahtar hiçbir şey
   kaydetmez.
4. Koşullar henüz kabul edilmediyse metni gösterir
   (`EVREN, kullanım koşullarını (sürüm <n>) kabul etmenizi istiyor. Metin
   aşağıda.`) ve `Koşulları kabul ediyor musunuz? [e/H]` diye sorar. Evet
   anlamına gelen bir yanıt (örneğin "e") olmazsa anahtar kaydedilmez.
5. Anahtarı saklar ve kalan günlük kotayı ve modelleri yazdırır (aynı raporu
   `ulgen status` de verir).

Anahtar nerede saklanır: işletim sisteminin kimlik bilgisi deposunda (sistem
anahtarlığı). Sistemde anahtarlık yoksa, yalnızca sizin okuyabileceğiniz bir
dosyaya — `credentials.json`, 0600 izniyle, yapılandırma dizininin içine.
Kaydettikten sonra Ülgen nerede sakladığını söyler: `Anahtar sistem
anahtarlığına kaydedildi.` ya da `Anahtar <dosya> dosyasına kaydedildi (yalnızca
siz okuyabilirsiniz).`

Ortamda `EVREN_API_KEY` tanımlıysa, Ülgen bunu bir notla söyler ve bu değişken
tanımlı kaldığı sürece kayıtlı anahtarın yerine geçer.

```sh
ulgen status    # kalan günlük token, sıfırlanma zamanı, kredi, modeller
ulgen logout    # kayıtlı anahtarı siler
```

`ulgen status` çıktısı şuna benzer (sayılar örnektir):

```
EVREN: giriş yapılmış (anahtar sistem anahtarlığından)
Günlük token: 15.000.000 içinden 14.532.144 kaldı (%96); sıfırlanma 03:00 (4 sa 12 dk sonra)
Kredi: 0 CR kalan, 0 CR bloke
Modeller:
  deepseek-v4.1-flash  1.048.576 bağlam  65.536 çıktı  araçlar düşünme görme
  ...
```

`ulgen status --json` aynı raporu tek bir JSON nesnesi olarak yazdırır
(betikler için). `ulgen logout` kayıtlı anahtarı siler; anahtar yoksa
`Kayıtlı anahtar yoktu.` der.

## Araçları ayarlama

### Listeleme

Tek başına `ulgen setup` hiçbir şey yazmaz; araçları ve durumlarını listeler:

```
$ ulgen setup
Araç        Kurulu    EVREN
qwen        0.25.0    ayarlı değil
opencode    1.18.35   ayarlı değil
codex       0.162.1   ayarlı değil
claude      2.1.296   ayarlı

Bir aracı ayarlamak için:  ulgen setup <araç>
```

"Kurulu" sütununda sürüm, sürüm okunamıyorsa `evet`, araç yoksa `bulunamadı`
yazar. "EVREN" sütununda `ayarlı`, `ayarlı değil`, `dikkat gerekiyor` (dosya
okunabiliyor ama ayarlamayı Ülgen reddediyor: sizin kendi girdiniz EVREN adında,
eski biçimde seçilmiş bir profil, aracın kabul etmediği bir anahtar biçimi) ya
da `dosyası okunamıyor` (dosya ayrıştırılamıyor ya da açılamıyor) yazar. Son
iki durumda tam iletiyi `ulgen setup <araç>` verir.

Claude Code için bir sağlayıcı dosyası yazılmaz. `ulgen setup claude` bunun
yerine tek bir dosya kurar: Claude Code'a bir EVREN işçisine görev vermeyi
öğreten bir beceri dosyası. Dosya, Claude dizininin altındaki
`skills/evren-workers/SKILL.md` yolundadır; Claude dizini `$CLAUDE_CONFIG_DIR`
tanımlıysa orası (mutlak bir yol olmalı, değilse çalışma reddedilir), yoksa
`~/.claude` olur. Dosya, diğer araçların dosyaları gibi planlama, denetim ve
yazma yolundan geçer: değişen bir dosya önce yedeklenir, `--dry-run` hiçbir
şey yazmaz, ikinci çalışma bir şey değiştirmez. Dosyanın ilk satırlarında
`installed by ulgen setup` yazan bir yorum yoksa dosya sizin kendi becerinizdir
ve Ülgen ona dokunmaz, bunu bir notla söyler. Liste, Claude Code'u ancak
çalıştırılabilir dosya bulunduğunda ve bu beceri dosyası Ülgen'in işaretiyle durduğunda
`ayarlı` gösterir. Kurulu değilse "EVREN" sütununda `ayarlı değil` yazar.

### Ayarlama

```sh
ulgen setup qwen                 # yalnızca Qwen Code
ulgen setup qwen codex           # birkaç araç
ulgen setup all                  # desteklenen ve kurulu her araç
```

Adını verdiğiniz araçlar kurulu olsun olmasın ayarlanır. `all` yalnızca kurulu
olanları ayarlar; kurulu olmayanı `codex: kurulu değil, atlandı` diye anar.
Hiçbiri kurulu değilse bunu bir hata ile söyler ve hiçbir şey yapmaz. `all` tek
başına kullanılmalıdır, bir araç adıyla birlikte verilemez.

`ulgen setup all` çıktısı şuna benzer:

```
$ ulgen setup all
Qwen Code 0.25.0
  ~/.qwen/settings.json  oluşturuldu
  Kullanım istatistikleri kapatıldı (privacy.usageStatisticsEnabled); Qwen Code bunları üçüncü bir tarafa gönderiyor.
  Kendi adıyla başlatmak isterseniz önce anahtarı kabuğunuza verin:  export EVREN_API_KEY="$(ulgen token)"
  Başlatmak için:  ulgen run qwen

opencode 1.18.35
  ~/.config/opencode/opencode.json  oluşturuldu
  Kendi adıyla başlatmak isterseniz önce anahtarı kabuğunuza verin:  export EVREN_API_KEY="$(ulgen token)"
  Başlatmak için:  ulgen run opencode

Codex CLI 0.162.1
  ~/.codex/evren.config.toml  oluşturuldu
  Kendi adıyla başlatmak isterseniz önce anahtarı kabuğunuza verin:  export EVREN_API_KEY="$(ulgen token)"
  EVREN, Codex'te bir profildir; kendi adıyla şöyle başlatılır:  codex --profile evren
  Başlatmak için:  ulgen run codex
```

Bir dosya zaten varsa ve değişmesi gerekiyorsa satır `güncellendi (+X -Y)`
olur ve altına `yedek: <yol>` düşer. Hiçbir şey değişmiyorsa araç için yalnızca
`zaten ayarlı; değişecek bir şey yok` yazılır.

### Seçenekler

- `--dry-run`: değişiklikleri fark (diff) olarak gösterir, hiçbir şey yazmaz ve
  sonunda `hiçbir şey yazılmadı (--dry-run)` der. Anahtarınız ve adından gizli
  olduğu anlaşılan değerler çıktıda gizlenir.
- `--model <kimlik>`: araçların seçeceği modeli belirler. Verilen model
  EVREN'in araç kullanabilen sohbet modelleri arasında olmalıdır.
  Örnek: `ulgen setup qwen --model glm-5.3`.
- `--default`: aracın başka bir varsayılanı olsa da EVREN'i varsayılan yapar.
  Örnek: `ulgen setup qwen --default`. Codex için geçerli değildir; Codex'in
  kendi varsayılanı kalır ve EVREN bir profil olarak durur.
- `--base-url <adres>`: sağlayıcının temel URL'sini değiştirir (OpenAI uyumlu
  bir `/v1` uç noktası). https olmalı ya da yalnızca bu makinede (localhost)
  http olmalıdır; başka bir ana makine verilirse anahtarınızın oraya
  gönderileceği bir notla söylenir. Örnek:
  `ulgen setup qwen --base-url https://vekil.example/v1`.

### Her araçta ne yazılır

| Araç | Dosya | İçine konan |
|---|---|---|
| Qwen Code | `~/.qwen/settings.json` (ya da `$QWEN_HOME/settings.json`) | `modelProviders.openai` altına `envKey: EVREN_API_KEY` taşıyan bir girdi (her EVREN modeli için), varsayılan model ve (yoksa) `privacy.usageStatisticsEnabled` |
| opencode | `$OPENCODE_CONFIG_DIR/opencode.json` ya da `~/.config/opencode/opencode.json` (varsa `opencode.jsonc`) | `provider.evren`: `options.baseURL`, `options.apiKey` = `{env:EVREN_API_KEY}`, modeller ve varsayılan model |
| Codex CLI | `~/.codex/evren.config.toml` (ya da `$CODEX_HOME/evren.config.toml`) | `model`, `model_provider = "evren"`, `[model_providers.evren]` altında `base_url`, `wire_api = "responses"`, `env_key = "EVREN_API_KEY"` |
| Claude Code | `$CLAUDE_CONFIG_DIR/skills/evren-workers/SKILL.md` ya da `~/.claude/skills/evren-workers/SKILL.md` | EVREN işçileri becerisi: Claude Code'a bir işçiye ne zaman görev verileceğini ve nasıl başlatılacağını öğreten metin. Sağlayıcı dosyası yazılmaz: Claude Code yalnızca Anthropic'in kendi API biçimini konuşur, Ülgen onu `ulgen run claude` ile çevirici üzerinden başlatır ve anahtarı Claude Code'a hiç vermez. |

### Yedekler

Ülgen değiştirdiği her dosyanın önceki hâlini yedeğe alır. Yedekler şuraya
gider:

```
<Ülgen yapılandırma dizini>/backups/<tarih-saat>/<araç>/<dosya adı>
```

Yapılandırma dizini `$ULGEN_CONFIG_DIR`, yoksa işletim sisteminin kullanıcı
yapılandırma dizini altındaki `ulgen` klasörüdür (Linux'ta `~/.config/ulgen`,
macOS'ta `~/Library/Application Support/ulgen`, Windows'ta
`%AppData%\ulgen`). Bir yedeğin üzerine asla yazılmaz; aynı saniyedeki bir
sonraki çalışma `-2`, `-3` gibi yeni bir dizin adı alır.

## Bir aracı başlatma

```sh
ulgen run qwen
ulgen run opencode
ulgen run codex
```

`ulgen run`, anahtarı aracın ortamına `EVREN_API_KEY` olarak verir ve aracı
başlatır. Araç adından sonra yazdığınız her şey araca olduğu gibi iletilir,
Ülgen'in seçeneği olarak okunmaz:

```sh
ulgen run qwen --help
```

Qwen Code ve opencode argümanları aynen alır. Codex'te Ülgen, `--profile evren`
seçeneğini argümanlarınızın önüne ekler; böylece Codex EVREN profilini okur.
Kendi profilinizi zaten seçtiyseniz ya da `--profile` kabul etmeyen bir alt
komut verdiyseniz Ülgen araya girmez.

Aracı kendi adıyla başlatmak isterseniz, önce anahtarı kabuğunuza verin.
`ulgen setup` bu satırı size yazdırır:

```sh
# Linux ve macOS, POSIX kabukları
export EVREN_API_KEY="$(ulgen token)"
```

```powershell
# Windows, PowerShell
$env:EVREN_API_KEY = (ulgen token)
```

Codex için kendi adıyla başlatma `codex --profile evren` biçimindedir.

`ulgen token` kayıtlı anahtarı başka bir programa verir. Çıktı bir ekrana
gidiyorsa Ülgen anahtarı yazdırmaz ve `--show` eklemenizi söyler; böylece
anahtar yanlışlıkla ekrana basılmaz. Anahtar bu komutun standart çıktısı
dışında hiçbir yerde metin olarak görünmez.

## Claude Code

Claude Code yalnızca Anthropic'in kendi API biçimini konuşur; EVREN ise OpenAI
uyumlu bir API sunar. Bu yüzden `ulgen run claude`, Claude Code çalıştığı sürece bu
makinede küçük bir çevirici çalıştırır ve Claude Code'u o çeviriciye bağlar.
Anahtarı EVREN'e yalnızca çevirici gönderir; EVREN anahtarı Claude Code'a hiç
ulaşmaz. Claude Code yalnızca bu çalışmaya özel, rastgele üretilmiş bir jeton
alır. Modeli `ulgen run --model <kimlik> claude` ile
seçersiniz; verilmezse Ülgen'in varsayılan EVREN modeli kullanılır ve küçük
işler için EVREN'de varsa `deepseek-v4-flash` seçilir. Ülgen, aracı başlatmadan
önce bir kez şu satırı standart hataya yazar: `Claude Code, Ülgen'in çeviricisi
üzerinden bir EVREN modeliyle çalışıyor; Anthropic bu kullanımı desteklemiyor.`
Bu satırı görmek istemiyorsanız `ULGEN_QUIET_NOTICE=1` ortam değişkenini
tanımlayın. Anthropic'in Claude Code'u başka modellerle çalıştırmayı
desteklemediğini açıkça söylemek gerekir; bu kullanım sizin sorumluluğunuzdadır.

```sh
$ ulgen run claude -p "Tek kelimeyle selam ver."
ulgen: Claude Code, Ülgen'in çeviricisi üzerinden bir EVREN modeliyle çalışıyor; Anthropic bu kullanımı desteklemiyor.
[claude-code:unrecognized_model] {"model":"deepseek-v4.1-flash","query_source":"sdk"}
merhaba
```

Bilmeniz gereken üç şey daha var. Claude Code EVREN'in model adlarını tanımaz.
Ülgen ona seçilen modelin bağlam penceresini `CLAUDE_CODE_MAX_CONTEXT_TOKENS`
ile bildirir; böylece Claude Code 200 bin token varsaymaz ve açılışta bunun
için uyarı göstermez (değişkeni kendiniz tanımladıysanız sizin değeriniz
kalır). Tek seferlik bir çalıştırmada (`-p`) ise Claude Code, tanımadığı her
model için standart hataya bir kez yukarıdaki `[claude-code:unrecognized_model]`
satırını yazar. Bu bir hata değil, Claude Code'un kendi bilgi satırıdır; istek
olduğu gibi gider. EVREN anahtarınızı kabul etmezse (süresi dolmuş ya da
silinmiş bir anahtar) Claude Code'un içinde
`EVREN did not accept the key; leave Claude Code and run: ulgen login` iletisini
görürsünüz: Claude Code'dan çıkın ve `ulgen login` çalıştırın.

Claude Code kapanırken oturumu sürdürmek için `claude --resume <kimlik>`
komutunu önerir. Bu komutu olduğu gibi çalıştırırsanız Claude Code çeviriciye
değil, kendi ayarlarındaki hizmete (örneğin Anthropic hesabınıza) bağlanır.
Oturumu EVREN üzerinde sürdürmek için başına `ulgen` ekleyin:
`ulgen claude --resume <kimlik>`.

`ulgen proxy`, Ülgen'in başlatmadığı, ama Anthropic API'sini konuşan bir
program için aynı çeviriciyi tek başına çalıştırır. Seçenekleri:

- `--port <sayı>`: çeviricinin dinleyeceği port; verilmezse (ya da `0`
  verilirse) sistem boş bir port seçer.
- `--model <kimlik>`: çeviricinin istekleri yönlendireceği EVREN modeli;
  verilmezse Ülgen'in varsayılan modeli.
- `--base-url <adres>`: anahtarın gönderileceği EVREN API kökü; https olmalı
  ya da yalnızca bu makinede (localhost) http olmalı.
- `--print-env`: çeviriciye bağlanacak bir kabuğun kullanacağı satırları
  standart çıktıya yazar (jeton tam olarak görünür).

Çevirici yalnızca `127.0.0.1` üzerinde dinler ve dinlediği adresi standart
hataya yazar. Jetonu `ULGEN_PROXY_TOKEN` ortam değişkeniyle kendiniz
verebilirsiniz (en az 16 karakter); vermezseniz Ülgen rastgele bir jeton üretir
ve `--print-env` kullanmadığınız sürece bu jetonu hiçbir yerde göstermez.

### EVREN işçileri

Bir projede Claude Code ile çalışırken, herkesin yapabileceği, kararları
zaten verilmiş işleri ücretsiz bir EVREN modeline bırakabilirsiniz. `ulgen
worker`, tek bir etkileşimsiz (headless) Claude Code sürecini bir EVREN
modelinde, gözetimsiz olarak çalıştırır: görevi siz bir dosyaya yazarsınız,
işçi o görevi yapar ve ne yaptığını kısa bir özetle bildirir. İşçi EVREN'e
bu makinedeki Ülgen çeviricisi üzerinden ulaşır; EVREN anahtarını hiç görmez.
Bir çalışma için tek bir işçi süreci başlar ve işçi size soru soramaz,
bu yüzden görev baştan eksiksiz olmalıdır.

```sh
ulgen worker [seçenekler] <görev-dosyası | ->
```

Görev, dosyanın metnidir; `-` yazarsanız görev standart girdiden okunur. Görev
boş olamaz ve en çok 1 MB olabilir.

Seçenekler:

- `--model <kimlik>`: işçinin çalışacağı EVREN modeli; verilmezse Ülgen'in
  varsayılan modeli kullanılır.
- `--dir <dizin>`: işçinin çalışacağı dizin; varsayılan, komutu çalıştırdığınız
  dizindir. Dizin var olmalı ve bir proje olmalıdır: ev dizininiz, onun
  üstündeki bir dizin ya da bir dosya sistemi kökü kabul edilmez. Yol bir
  sembolik bağ ise bağın gösterdiği gerçek dizine bakılır. İşçi dosyaları
  sormadan değiştirir, bu yüzden ona her şey değil bir proje gösterilir.
- `--allow <kural-ya-da-ön-ayar>`: varsayılan izinlere ekleme yapar; birden çok
  kez verilebilir. Değer ya bir ön ayar adıdır (`go`, `node`, `python`, `read`)
  ya da Claude Code'un kendi yazımıyla tek bir izin kuralıdır:
  `Bash(make test:*)`, `Read(~/kaynaklar/**)`, `WebSearch` gibi. Kural `Bash`,
  `Read`, `WebFetch` ya da `WebSearch` araçlarından biri için olmalıdır. Şunlar
  reddedilir:
  - bir kabuk ya da başka komutları çalıştıran bir komut için kural (`sh`,
    `bash`, `env`, `xargs`, `sudo`, `timeout` ve benzerleri) ve her komuta izin
    veren kural (`Bash`, `Bash(*)`): bunlar her şeye izin verirdi;
  - komutları birleştiren ya da yönlendiren bir karakter içeren kural (`;`,
    `&`, `|`, `<`, `>`, ters tırnak): bir kural tek bir komutu adlandırır;
  - düzenleme araçları (`Edit`, `Write`) için kural: işçi kendi dizininde
    zaten düzenler, bu izin genişletilemez;
  - içinde virgül ya da ikinci bir parantez olan değer: tek değerle birden çok
    kural verilemez.

  Reddedilen değer için ileti, değerin kaçıncı olduğunu ve nedenini söyler.
- `--timeout <süre>`: süre dolunca işçiyi, başlattığı her şeyle birlikte
  bitirir; varsayılan `30m`, `0` süre sınırı koymaz. Süre dolduğunda çıkış
  kodu 124, Ctrl+C ile kesildiğinde 130 olur.
- `--max-turns <n>`: Claude Code'a `--max-turns n` verir; verilmezse sınırsız.
- `--out <dosya>`: işçinin bütün JSON çıktısını (yanıtını) bu dosyaya yazar.
  Dosya işçi başlamadan önce, yalnızca sizin okuyabileceğiniz izinlerle
  oluşturulur; oluşturulamıyorsa işçi başlatılmaz ve komut 1 koduyla biter.
- `--no-sandbox`: Claude Code'un kendi kum havuzunu açmaz; kabuk komutları
  artık `--dir` diziniyle sınırlı kalmaz. Elle çalıştırmayacağınız bir görevde
  ya da depoda kullanmayın.
- `--base-url <adres>`: anahtarın gönderileceği EVREN API kökü; https olmalı
  ya da yalnızca bu makinede (localhost) http olmalı.

İşçi bir projede dosyaları sormadan değiştirdiği için varsayılan izinler
dardır. Her işçi, dosya araçlarıyla yalnızca `--dir` altındaki dosyaları okur,
düzenler ve oluşturur; kabukta ise yalnızca şu komutları çalıştırır: `git
status`, `git diff`, `git log`, `ls`, `mkdir`. Ön ayarlar bunların üstüne
ekler:

| Ön ayar | İzin verdiği komutlar | Kum havuzunda ayrıca yazılabilen yer |
|---|---|---|
| `go` | `go`, `gofmt`, `GOOS=windows go`, `GOOS=linux go`, `GOOS=darwin go` | Go'nun derleme ve modül önbellekleri |
| `node` | `node`, `npm test`, `npm run` | `~/.npm` |
| `python` | `python3`, `python`, `pytest` | yok |
| `read` | `cat`, `head`, `tail`, `grep`, `rg`, `wc`, `sed -n` | yok |

İşçi, siz bir kural vermedikçe commit ya da push yapamaz; `--allow` ile
böyle bir kural vermeyin.

İşçinin çevresindeki sınır iki katmandır:

- **İzin kuralları.** Dosya araçları yalnızca `--dir` altında çalışır.
  `--allow "Read(...)"` ile başka bir yeri okumasına izin verebilirsiniz.
  Anahtarların ve kimlik bilgilerinin olağan yerleri (`~/.ssh`, `~/.aws`,
  `~/.netrc`, kodlama araçlarının ayar dizinleri, Ülgen'in kendi dizini ve
  benzerleri) ise ne izin verirseniz verin kapalı kalır.
- **Kum havuzu.** Claude Code'un kendi kum havuzudur (macOS'ta Seatbelt,
  Linux'ta bubblewrap). Kabuk komutları yalnızca `--dir` dizininin ve geçici
  dizinin altına yazabilir ve yukarıdaki anahtar yerlerini okuyamaz. Kum
  havuzu izin kurallarının yerine geçmez: bir kabuk komutu, kum havuzunda
  çalışacak olsa bile yalnızca bir kural izin veriyorsa çalışır.

Kum havuzu, kabuk komutlarının makinedeki başka dosyaları okumasını
engellemez: `go` ya da `python` gibi bir ön ayar kod çalıştırır ve o kod sizin
okuyabildiğiniz her şeyi okuyabilir. Bu yüzden bir işçiye, elle
çalıştırmayacağınız bir depoda görev vermeyin.

Kum havuzu bulunmayan bir sistemde işçi başlar başlamaz durur. Windows'ta
Claude Code'un kum havuzu yoktur; orada `ulgen worker` yalnızca `--no-sandbox`
ile başlar ve kabuk komutlarının sınırlanmadığını bir satırla söyler (Claude
Code'un kum havuzu WSL2 altında çalışır).

Bir örnek: görevi `task.md` dosyasına yazdınız ve işi ayrı bir kopyada
yapmasını istiyorsunuz.

```sh
ulgen worker --dir ~/isler/proje-kopya --allow go --timeout 60m --out sonuc.json task.md
```

Ülgen, işçi bittiğinde standart çıktıya kısa bir özet yazar (nasıl bittiği,
kaç saniye sürdüğü, model, izinler nedeniyle reddedilen araç çağrıları ve
işçinin son metni) ve `--out` verdiyseniz işçinin bütün JSON çıktısını dosyaya
yazar. Birden çok bağımsız görevi aynı anda
çalıştırabilirsiniz; her işçinin kendi çeviricisi ve portu vardır.

## Ülgen neye dokunmaz

- Anahtar hiçbir aracın dosyasına yazılmaz. Araçların dosyalarında anahtar bir
  değişkene atıfla durur: opencode'da `{env:EVREN_API_KEY}`, Codex'te
  `env_key = "EVREN_API_KEY"`, Qwen Code'da `envKey: "EVREN_API_KEY"`. Anahtar
  aracın ortamına `ulgen run` sırasında konur.
- Codex'in kendi `config.toml` dosyası asla yazılmaz; yalnızca okunur. Ülgen
  Codex'te ayrı bir profil dosyası yazar (`evren.config.toml`).
- Bütünüyle okunamayan bir dosya asla yeniden yazılmaz. Ayrıştırılamayan bir
  JSON ya da TOML dosyası için Ülgen düzeltmenizi ister ve dosyaya dokunmaz.
- Salt okunur bir dosya olduğu gibi bırakılır; Ülgen dosyanın izinlerini
  değiştirmez.

## Güvenlik ve gizlilik

### Kullanım istatistikleri

Qwen Code'un kullanım istatistikleri ayarlama sırasında kapatılır: ayar
dosyasında `privacy.usageStatisticsEnabled` yoksa `false` yazılır. Bu
istatistikler üçüncü bir tarafa gönderilir. Değer zaten varsa — ne olursa
olsun — sizinki sayılır ve değiştirilmez.

### Proje denetimi (`ulgen run`)

Qwen Code ve opencode, kullanıcı dizininin yanı sıra projeden de ayar dosyası
okur ve projedeki dosya kazanır. Böyle bir dosya, `EVREN_API_KEY` okuyan bir
model tanımlayıp anahtarınızı başka bir adrese gönderebilir. `ulgen run` aracı
başlatmadan önce bu yüzden projeye bakar: başlama dizininden yukarı, ev
dizinine (ya da dışındaysa dosya sisteminin köküne) kadar. Qwen Code için
`.qwen/settings.json`, opencode için `opencode.json`, `opencode.jsonc`,
`.opencode/opencode.json` ve `.opencode/opencode.jsonc` denetlenir.

Projedeki bir dosya EVREN anahtarının ya da EVREN sağlayıcısının adını
içeriyorsa araç başlatılmaz:

```
$ ulgen run qwen
ulgen: ./.qwen/settings.json dosyasında EVREN anahtarının ya da EVREN sağlayıcısının adı geçiyor. Projedeki bir dosya anahtarınızı başka bir adrese gönderebilir; bu yüzden Qwen Code başlatılmadı. Yine de başlatmak için:  ulgen run --trust-project qwen
```

(Gerçek iletide dosyanın tam yolu görünür.) opencode'un proje dosyası
ayarlarının bir bölümünü başka bir dosyadan alıyorsa (`{file:…}`) da araç
başlatılmaz: Ülgen o dosyanın içeriğini denetlemez, oradan gelen metin EVREN
sağlayıcısının adı ya da anahtarı tutan bir dosya olabilir. Projeye
güveniyorsanız `ulgen run --trust-project qwen` ile bu denetimi atlarsınız. Codex için böyle bir denetim
yoktur; Codex'in proje dosyaları sağlayıcı tanımlayamaz.

### Windows'ta `.cmd` olarak kurulmuş araçlar

npm, araçları Windows'ta `.cmd` dosyası olarak kurar. npm'in yazdığı bu dosya
yalnızca `node <betik> <argümanlar>` komutunu çalıştırır; Ülgen dosyayı tanırsa
aynı komutu `cmd.exe` olmadan kendisi başlatır ve argümanlarınız olduğu gibi
iletilir.

Dosya npm'in yazdığıyla harfi harfine aynı değilse (başka bir kurucu, elle
değiştirilmiş bir dosya) onu yalnızca `cmd.exe` çalıştırabilir. O durumda
`ulgen run` komut satırını kendisi kurar ve `cmd.exe`'nin yorumlayacağı bir
karakter (`"`, `%`, `^`, `!` ya da satır sonu) taşıyan bir argümanı iletmek
yerine reddeder. İleti hangi argümanda hangi karakterin olduğunu söyler ve
aracın o argüman olmadan başlatılabileceğini hatırlatır.

## Kurulumu geri alma

```sh
ulgen setup --remove qwen            # tek bir araç
ulgen setup --remove all             # hepsi
ulgen setup --remove qwen --dry-run  # önce ne değişeceğini görmek için
```

`--remove`, `ulgen setup` komutunun o araca yazdıklarını geri alır ve yalnızca
Ülgen'e ait olana dokunur:

- **Qwen Code:** EVREN modelleri; varsayılan model, yalnızca bu modellerden
  birini gösteriyorsa. Kullanım istatistiği ayarı yerinde kalır: Ülgen onu
  sizin seçiminizden ayırt edemez.
- **opencode:** `provider.evren` girdisi ve EVREN'i gösteren `model` ile
  `small_model` satırları.
- **Codex CLI:** `evren.config.toml` profil dosyası. `config.toml` dosyanıza
  dokunulmaz.
- **Claude Code:** EVREN işçileri beceri dosyası.

Sizin yazdığınız girdiler ve yorumlar yerinde kalır.
Değişen ya da silinen her dosyanın önce yedeği alınır. Bu komut için giriş
yapmış olmanız ya da internete bağlı olmanız gerekmez.

Tek bir dosyayı eski hâline döndürmek isterseniz yedeğini de kullanabilirsiniz:
`<yapılandırma dizini>/backups/<tarih-saat>/<araç>/<dosya adı>` konumundaki
yedeği asıl dosyanın üzerine kopyalamak yeter.

Kayıtlı anahtarı silmek için `ulgen logout` vardır; bu yalnızca anahtarı siler,
araçların dosyalarını değiştirmez. Ortamda `EVREN_API_KEY` hâlâ tanımlıysa
`ulgen logout` bunu bir notla söyler.

## Bir şey çalışmıyorsa

```sh
ulgen doctor
```

Anahtarın kayıtlı olup olmadığını, EVREN'in yanıt verip anahtarı kabul edip
etmediğini, günlük kotayı ve her aracın kurulu ve ayarlı olup olmadığını
denetler. Her denetim tek satırdır: `ok` yolunda, `--` bilgi, `!!` düzeltilmesi
gereken bir sorun demektir; sorunların altında ne yapılacağı yazar. Hiçbir şeyi
değiştirmez ve anahtarı yazdırmaz. Betikler için `ulgen doctor --json`; bir
sorun bulunduğunda çıkış kodu 1 olur.

## Bir şey reddedildiğinde

Aşağıdaki tabloda en sık karşılaşacağınız iletiler ve ne yapmanız gerektiği
vardır. İletiler arayüzden alınmıştır; uzun olanlar "…" ile kısaltılmıştır. Açılı
ayraç içindeki yerlere (`<dosya>`, `<araç>` gibi) gerçek adlar gelir.

| Ne oldu | İleti | Ne yapmalı |
|---|---|---|
| Giriş yapılmamış | `giriş yapılmamış; ulgen login komutunu çalıştırın` | `ulgen login` |
| Kayıtlı anahtar artık geçerli değil | `kayıtlı anahtar artık geçerli değil; ulgen login komutunu çalıştırın` | `ulgen login` ile yeniden girin |
| Koşullar kabul edilmemiş | `kullanım koşulları kabul edilmemiş; ulgen login komutunu çalıştırın` | `ulgen login` |
| Araç kurulu değil | `<araç> kurulu değil` | Aracı kurun (yukarıdaki npm satırları) |
| Araç EVREN için ayarlı değil | `<araç> içinde EVREN ayarlı değil; ulgen setup <araç> komutunu çalıştırın` | `ulgen setup <araç>` |
| Bir dosya ayrıştırılamıyor | `<dosya> geçerli bir <biçim> dosyası değil (<neden>); düzeltin ya da başka yere taşıyın, sonra ulgen setup komutunu yeniden çalıştırın` | Dosyayı düzeltin ya da taşıyın |
| Dosya beklenen düzende değil | `<dosya> beklenen düzende değil (<neden>); …` | Dosyayı düzeltin |
| EVREN adında sizin kendi girdiniz | `<dosya> dosyası <girdi> girdisini tanımlıyor ve bu girdi ulgen'e ait değil; ulgen onun üzerine yazmaz …` | Girdiyi kaldırın ya da adını değiştirin |
| Başka bir varsayılan model çakışması | `<dosya>, Qwen Code'un varsayılan modeli olarak "<model>" adını veriyor ve modelProviders altında bu kimlikte bir girdi yok. EVREN de aynı adda bir model sunuyor; …` | EVREN'i varsayılan yapmak için `ulgen setup qwen --default`; kendi modelinizi korumak için önce `modelProviders.openai` altına modeliniz için bir girdi ekleyin |
| Codex'te profili seçmenin eski yolu | `<dosya> dosyasında <ayar> var; bu, profil seçmenin ya da tanımlamanın eski yoludur ve orada durdukça Codex evren.config.toml gibi bir profil dosyasını kullanamaz; …` | Eski `profile`/`profiles` girdisini kaldırın |
| Dosya salt okunur | `<dosya> salt okunur olduğu için olduğu gibi bırakıldı; yazılabilir yapın, sonra ulgen setup komutunu yeniden çalıştırın` | Dosyayı yazılabilir yapın |
| Dosya Ülgen çalışırken değişti | `<dosya>, ulgen setup çalışırken değişti; bu dosyaya hiçbir şey yazılmadı, ulgen setup komutunu yeniden çalıştırın` | Komutu yeniden çalıştırın |
| Projedeki bir dosya EVREN'den söz ediyor | `<dosya> dosyasında EVREN anahtarının ya da EVREN sağlayıcısının adı geçiyor. … Yine de başlatmak için:  ulgen run --trust-project <araç>` | Dosyayı inceleyin; güveniyorsanız `--trust-project` |
| Aracın adı verilmedi | `başlatılacak aracın adını verin` | `ulgen run <araç>` |
| Bilinmeyen araç | `<ad> aracı başlatılamıyor; desteklenenler: <liste>` | Adı düzeltin |
| Bilinmeyen model | `EVREN'de <model> adında, araç kullanabilen bir sohbet modeli yok; şunlardan birini seçin: <liste>` | Listeden birini seçin |
| Çevirici dışındaki bir araca `--model`/`--base-url` verildi | `--model ve --base-url yalnızca <araç> için geçerli; Ülgen onu çevirici üzerinden başlatır` | Bu seçenekleri yalnızca `ulgen run claude` ile kullanın |
| Çeviricinin portu geçersiz | `<sayı> portu 0 ile 65535 arasında değil` | 0 ile 65535 arasında bir port verin |
| Çevirici jetonu çok kısa | `<değişken>, <sayı> karakterden kısa; daha uzun bir jeton verin ya da değişkeni kaldırın` | Daha uzun bir jeton verin ya da `ULGEN_PROXY_TOKEN` değişkenini kaldırın |
| Çevirici dinlemeye başlayamadı | `çevirici dinlemeye başlayamadı: <neden>` | Nedeni giderin ya da başka bir port verin |
| Bilinmeyen dil | `bilinmeyen dil "<değer>"; tr, en ya da keep kullanın` | Değeri düzeltin |
| Hiçbir araç kurulu değil (`all`) | `desteklenen araçların hiçbiri kurulu değil (<liste>); birini kurun ya da kurmadan önce ayarlamak için aracın adını verin` | Bir araç kurun ya da adını verin |
| Windows'ta argümanda özel karakter | `<n>. argümanda <karakter> karakteri var; Windows'un cmd.exe üzerinden başlattığı bir araca bu karakter güvenle iletilemez. …` | Argümanı çıkarın; metni aracı başlattıktan sonra aracın içinde yazın |
| Güvenli olmayan adres | `--base-url https kullanmalı ya da yalnızca bu makinede (localhost) http olmalı` | https ya da localhost kullanın |

## Kota ve modeller

EVREN'de günlük kota hesap başına 15.000.000 tokendır ve İstanbul saatiyle
03:00'te sıfırlanır; bu kota 2026-11-01'e kadar geçerlidir ve o tarihe kadar
kullanım ücretsizdir. Ön ek önbelleğinden (prefix cache) karşılanan istem
tokenları kotaya %3 oranında sayılır; istemin başını sabit tutmak kotayı çok
daha uzağa götürür. EVREN açık ağırlıklı modeller sunar; bugün
bunların arasında `deepseek-v4.1-flash`, `mimo-v2.6-pro`, `glm-5.3`,
`qwen3.8-flash-next`, `gemma-4-31b` ve `qwen3-vl-30b` vardır. `auto` bir model
değil, yönlendirme kipidir: isteği hangi modelin yanıtlayacağını ağ geçidi
seçer. Güncel liste için `ulgen status` çıktısına bakın.

`ulgen status` kalan günlük kotayı, sıfırlanma zamanını, kalan krediyi ve
modelleri (bağlam uzunluğu, en büyük çıktı ve araçlar/düşünme/görme
yetenekleriyle birlikte) gösterir.
