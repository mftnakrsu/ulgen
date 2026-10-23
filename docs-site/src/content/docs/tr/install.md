---
title: Ülgen'i kurma
description: Ülgen'i kurulum betikleriyle, .deb ya da .rpm paketi olarak ya da kaynaktan kurma, indirilen dosyayı elle doğrulama ve Ülgen'i kaldırma.
sidebar:
  label: Kurulum
---

## Kurulum betikleri

Linux ve macOS:

```sh
curl -fsSL https://raw.githubusercontent.com/mftnakrsu/ulgen/main/scripts/install.sh | sh
```

Windows (PowerShell):

```powershell
irm https://raw.githubusercontent.com/mftnakrsu/ulgen/main/scripts/install.ps1 | iex
```

İkisi de makineye ve işlemciye uygun arşivi indirir, hiçbir şeyi açmadan önce
SHA-256 değerini `checksums.txt` ile karşılaştırır ve programı kurulum dizinine
koyar: `$HOME/.local/bin`, Windows'ta `%LOCALAPPDATA%\Programs\ulgen`. Hiçbir
zaman `sudo` çalıştırmaz, parola sormaz ve başka bir yere yazmaz. Windows'ta
dizin, makinenin değil sizin kendi `PATH` değişkeninize eklenir.

| Değişken | Anlamı |
|---|---|
| `ULGEN_VERSION` | Kurulacak sürüm, örneğin `1.2.3`; tanımlı değilse en son sürüm |
| `ULGEN_INSTALL_DIR` | Programın konacağı yer |
| `ULGEN_BASE_URL` | Sürüm dosyalarının bulunduğu yer, bir yansı için. Yalnızca `https://`, ya da aynı makinede `http://127.0.0.1:PORT` ve `http://localhost:PORT`. Yansı kullanılırken `ULGEN_VERSION` da verilmelidir |

`install.ps1`, `PATH` değişkenine dokunmaması için `-NoPathUpdate` seçeneğini
alır.

## Pardus, Debian ve Ubuntu

Her sürümde `ulgen_<sürüm>_amd64.deb` ve `ulgen_<sürüm>_arm64.deb` bulunur
(Fedora ve benzerleri için aynıları `.rpm` olarak da vardır; biri
`sudo dnf install ./ulgen_<sürüm>_amd64.rpm` ile kurulur).
`install.sh --deb` bu makineye uygun olanı indirir, denetler, bulunduğunuz
dizinde bırakır ve onu kuracak satırı yazdırır:

```sh
curl -fsSL https://raw.githubusercontent.com/mftnakrsu/ulgen/main/scripts/install.sh | sh -s -- --deb
sudo apt install ./ulgen_<sürüm>_amd64.deb
```

## Kaynaktan derleme

Go 1.26 ya da daha yenisi:

```sh
go install github.com/mftnakrsu/ulgen/cmd/ulgen@latest
```

## İndirilen dosyayı elle doğrulama

Bir sürümdeki `checksums.txt` her dosyanın SHA-256 değerini listeler;
`checksums.txt.sigstore.json` da onun imzasıdır. İmzayı sürüm iş akışı
[cosign](https://docs.sigstore.dev/cosign/system_config/installation/) ile,
iş akışının kendi kimliğini kullanarak oluşturur; sızabilecek bir anahtarımız
yoktur.

Önce sağlama toplamı dosyasının bu deponun sürüm iş akışı tarafından
oluşturulduğunu doğrulayın:

```sh
cosign verify-blob \
  --bundle checksums.txt.sigstore.json \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com \
  --certificate-identity-regexp '^https://github\.com/mftnakrsu/ulgen/\.github/workflows/release\.yml@refs/tags/v' \
  checksums.txt
```

Sonra indirdiğiniz dosyanın, listedeki dosya olduğunu:

```sh
sha256sum -c checksums.txt --ignore-missing   # ya da: shasum -a 256 -c checksums.txt --ignore-missing
```

Windows'ta `Get-FileHash .\ulgen_1.2.3_windows_amd64.zip -Algorithm SHA256`
çıktısı, listede o dosyanın satırındaki değerle aynı olmalıdır.

Kurulum betikleri ikisini de yapar. Sağlama toplamı her zaman denetlenir. İmza,
`cosign` kuruluysa denetlenir; o durumda geçerli imzası olmayan bir sürüm
kurulmaz. `cosign` yoksa betikler imzanın denetlenmediğini söyler ve devam
eder.

## Ülgen'i kaldırma

```sh
ulgen setup --remove all   # Ülgen'in araçlara yazdıklarını geri alır
ulgen logout               # kayıtlı anahtarı siler
```

Sonra programın kendisini silin. `--remove` yalnızca Ülgen'e ait olanı çıkarır
(kendi girdileri, bunları gösteren bir varsayılan, kurduğu profil ve beceri
dosyası) ve sizin yazdığınızı bırakır. Değiştirdiği ya da sildiği her dosyanın
önce yedeğini alır: Ülgen'in yapılandırma dizinindeki `backups` altına.
Yedekleri artık istemediğinizde bu dizini silebilirsiniz.
