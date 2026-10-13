# install.ps1 — install the ulgen command-line tool on Windows.
#
# It finds the processor, picks a release, downloads the archive and
# checksums.txt from a release page, checks the signature of checksums.txt
# when cosign is installed, checks the SHA-256 of the archive against
# checksums.txt, unpacks it and puts ulgen.exe into
# %LOCALAPPDATA%\Programs\ulgen (or ULGEN_INSTALL_DIR), replacing an older one.
# It also adds that directory to the user's PATH unless -NoPathUpdate is given;
# the machine's PATH is never touched.
#
# Run it in one line:
#
#   irm https://raw.githubusercontent.com/mftnakrsu/ulgen/main/scripts/install.ps1 | iex
#
# Environment variables:
#
#   ULGEN_VERSION       the release to install, for example 1.2.3 or v1.2.3
#   ULGEN_BASE_URL      where the release files live, by default
#                       https://github.com/mftnakrsu/ulgen/releases/download
#   ULGEN_INSTALL_DIR   where to put the binary, by default
#                       %LOCALAPPDATA%\Programs\ulgen
#   ULGEN_TEST_ALLOW_FILE  allows a file:// address in ULGEN_BASE_URL; the
#                       tests set it, nothing else should
#
# The script never runs an elevated command and never writes outside
# ULGEN_INSTALL_DIR, a temporary directory of its own, and the user's PATH.

#Requires -Version 5.1

# Install-Ulgen does the work. It is a function, not top-level code, so the
# script works when it is run as "irm URL | iex": there is no top-level param
# block and no top-level exit that would close the caller's session. A failure
# throws, which ends the run with a non-zero code without ending the session.
function Install-Ulgen {
    [CmdletBinding()]
    param(
        [switch]$NoPathUpdate,
        [switch]$Help
    )

    $ErrorActionPreference = 'Stop'
    Set-StrictMode -Version Latest
    $ProgressPreference = 'SilentlyContinue'

# ---------------------------------------------------------------------------
# Messages. The language is Turkish when the user interface culture begins
# with "tr", English otherwise. Every message lives here, in this table.
# ---------------------------------------------------------------------------

$Lang = 'en'
if ((Get-UICulture).Name -like 'tr*') {
    $Lang = 'tr'
}

$Messages = @{
    usage = @{
        en = (@(
                'Usage: install.ps1 [-NoPathUpdate] [-Help]',
                '',
                'Installs the ulgen command-line tool from a release archive.',
                'Environment variables:',
                '  ULGEN_VERSION          the release to install (for example 1.2.3 or v1.2.3)',
                '  ULGEN_BASE_URL         where the release files live',
                '  ULGEN_INSTALL_DIR      where to put the binary (default %LOCALAPPDATA%\Programs\ulgen)',
                '  ULGEN_TEST_ALLOW_FILE  allows a file:// base URL (the tests use it only)'
            ) -join "`n")
        tr = (@(
                'Kullanım: install.ps1 [-NoPathUpdate] [-Help]',
                '',
                'ulgen programını bir sürüm arşivinden kurar.',
                'Ortam değişkenleri:',
                '  ULGEN_VERSION          kurulacak sürüm (örneğin 1.2.3 veya v1.2.3)',
                '  ULGEN_BASE_URL         sürüm dosyalarının bulunduğu yer',
                '  ULGEN_INSTALL_DIR      programın konacağı klasör (varsayılan %LOCALAPPDATA%\Programs\ulgen)',
                '  ULGEN_TEST_ALLOW_FILE  file:// temel adresine izin verir (yalnızca testler kullanır)'
            ) -join "`n")
    }
    arch_unsupported = @{
        en = 'unsupported processor: {0}'
        tr = 'desteklenmeyen işlemci: {0}'
    }
    version_bad = @{
        en = 'refused version "{0}": a version starts with a digit and holds digits and letters'
        tr = 'sürüm reddedildi "{0}": sürüm bir rakamla başlar ve rakam ile harflerden oluşur'
    }
    release_unknown = @{
        en = 'no release was found; set ULGEN_VERSION to name one'
        tr = 'hiçbir sürüm bulunamadı; ULGEN_VERSION ile bir sürüm belirtin'
    }
    mirror_needs_version = @{
        en = 'a mirror (ULGEN_BASE_URL) needs ULGEN_VERSION'
        tr = 'bir yansı (ULGEN_BASE_URL) için ULGEN_VERSION gerekir'
    }
    base_url_bad = @{
        en = 'refused address "{0}": use https, or http://127.0.0.1 or http://localhost'
        tr = 'adres reddedildi "{0}": https, ya da http://127.0.0.1 veya http://localhost kullanın'
    }
    download_failed = @{
        en = 'could not download {0}'
        tr = '{0} indirilemedi'
    }
    checksum_missing = @{
        en = '{0} is not listed in checksums.txt'
        tr = '{0} checksums.txt içinde yok'
    }
    signature_missing = @{
        en = 'this release has no signature for checksums.txt; nothing was installed'
        tr = 'bu sürümde checksums.txt dosyasının imzası yok; hiçbir şey kurulmadı'
    }
    signature_bad = @{
        en = 'the signature of checksums.txt could not be verified; nothing was installed'
        tr = 'checksums.txt dosyasının imzası doğrulanamadı; hiçbir şey kurulmadı'
    }
    signature_ok = @{
        en = 'the signature of checksums.txt was verified (cosign)'
        tr = 'checksums.txt dosyasının imzası doğrulandı (cosign)'
    }
    signature_unchecked = @{
        en = 'note: the signature was not checked because cosign is not installed; the checksum is checked'
        tr = 'not: cosign kurulu olmadığı için imza denetlenmedi; sağlama toplamı denetleniyor'
    }
    checksum_mismatch = @{
        en = 'the checksum of {0} does not match checksums.txt'
        tr = '{0} sağlama toplamı checksums.txt ile uyuşmuyor'
    }
    extract_failed = @{
        en = 'could not unpack {0}'
        tr = '{0} açılamadı'
    }
    binary_missing = @{
        en = 'the archive did not contain the ulgen binary'
        tr = 'arşivde ulgen programı yok'
    }
    installed = @{
        en = 'ulgen was installed in {0}'
        tr = 'ulgen şuraya kuruldu: {0}'
    }
    binary_version = @{
        en = 'version: {0}'
        tr = 'sürüm: {0}'
    }
    path_added = @{
        en = 'the directory was added to your PATH; open a new terminal before running ulgen'
        tr = 'klasör PATH değişkenine eklendi; ulgen komutunu kullanmadan önce yeni bir terminal açın'
    }
}

# Text returns a message in the current language, with {0} and the like filled
# from $Values.
function Text {
    param([string]$Name, [object[]]$Values = @())
    $template = $Messages[$Name][$Lang]
    if ($Values.Count -gt 0) {
        return ($template -f $Values)
    }
    return $template
}

# Say writes a message to standard output.
function Say {
    param([string]$Name, [object[]]$Values = @())
    Write-Output (Text $Name $Values)
}

# The temporary directory is remembered so that a failure can still remove it.
$Script:TmpDir = $null

# Fail removes the temporary directory and throws, so a failure ends the run
# with an error without closing the session that called the function.
function Fail {
    param([string]$Name, [object[]]$Values = @())
    if ($Script:TmpDir) {
        Remove-Item -LiteralPath $Script:TmpDir -Recurse -Force -ErrorAction SilentlyContinue
        $Script:TmpDir = $null
    }
    throw ('ulgen-install: ' + (Text $Name $Values))
}

if ($Help) {
    Write-Output (Text 'usage')
    return
}

# ---------------------------------------------------------------------------
# Working over TLS 1.2, which Windows PowerShell 5.1 does not offer unless it
# is asked for.
# ---------------------------------------------------------------------------

if ($PSVersionTable.PSVersion.Major -lt 6) {
    try {
        [System.Net.ServicePointManager]::SecurityProtocol =
            [System.Net.ServicePointManager]::SecurityProtocol -bor [System.Net.SecurityProtocolType]::Tls12
    }
    catch {
        # An older .NET without TLS 1.2 stays as it is; the download will say
        # if the site cannot be reached.
    }
}

# ---------------------------------------------------------------------------
# The processor.
# ---------------------------------------------------------------------------

$ProcessorValue = $env:PROCESSOR_ARCHITEW6432
if (-not $ProcessorValue) {
    $ProcessorValue = $env:PROCESSOR_ARCHITECTURE
}
switch ($ProcessorValue) {
    'AMD64' { $Arch = 'amd64' }
    'ARM64' { $Arch = 'arm64' }
    default { Fail 'arch_unsupported' @($ProcessorValue) }
}

# ---------------------------------------------------------------------------
# The address of the release page.
# ---------------------------------------------------------------------------

$BaseUrl = $env:ULGEN_BASE_URL
$BaseWasSet = $false
if ($BaseUrl) {
    $BaseWasSet = $true
} else {
    $BaseUrl = 'https://github.com/mftnakrsu/ulgen/releases/download'
}

$BaseOk = $false
$BaseIsFile = $false
if ($BaseUrl.StartsWith('https://')) {
    $BaseOk = $true
} elseif ($BaseUrl -match '^http://(localhost|127\.0\.0\.1):[0-9]+(/|$)') {
    # A loopback mirror may use plain http, but only as
    # http://localhost:PORT or http://127.0.0.1:PORT (digits, then a slash
    # or the end). A userinfo part such as http://localhost:x@evil.com/
    # names another host and is refused.
    $BaseOk = $true
} elseif ($BaseUrl.StartsWith('file://') -and $env:ULGEN_TEST_ALLOW_FILE -eq '1') {
    $BaseOk = $true
    $BaseIsFile = $true
}
if (-not $BaseOk) {
    Fail 'base_url_bad' @($BaseUrl)
}

# ---------------------------------------------------------------------------
# The temporary directory and the version.
# ---------------------------------------------------------------------------

$Script:TmpDir = Join-Path ([System.IO.Path]::GetTempPath()) ('ulgen-install-' + [System.Guid]::NewGuid().ToString('N'))
New-Item -ItemType Directory -Path $Script:TmpDir -Force | Out-Null

$Version = $env:ULGEN_VERSION
if ($Version -and $Version.StartsWith('v')) {
    $Version = $Version.Substring(1)
}

if (-not $Version) {
    if ($BaseWasSet) {
        Fail 'mirror_needs_version'
    }
    # Ask for the releases/latest page without following the redirect and read
    # the Location header; it ends with /tag/v<version>.
    $LatestUrl = 'https://github.com/mftnakrsu/ulgen/releases/latest'
    $Location = $null
    try {
        # Not following the redirect: the answer itself carries the address.
        $Response = Invoke-WebRequest -UseBasicParsing -Method Head -MaximumRedirection 0 -Uri $LatestUrl
        if ($Response.Headers['Location']) {
            $Location = $Response.Headers['Location']
        }
    }
    catch {
        $WebResponse = $_.Exception.Response
        if ($WebResponse -and $WebResponse.Headers) {
            if ($WebResponse.Headers['Location']) {
                $Location = $WebResponse.Headers['Location']
            } elseif ($WebResponse.Headers.Location) {
                $Location = $WebResponse.Headers.Location.ToString()
            }
        }
    }
    if ($Location -is [array] -and $Location.Count -gt 0) {
        $Location = $Location[0]
    }
    if ($Location -and ($Location -match '/tag/v')) {
        $Version = $Location.Substring($Location.LastIndexOf('/tag/v') + 6)
    }
    if (-not $Version) {
        Fail 'release_unknown'
    }
}

if ($Version -notmatch '^[0-9][0-9A-Za-z.+~-]*$') {
    Fail 'version_bad' @($Version)
}

# ---------------------------------------------------------------------------
# Fetch puts a release file at a local path, from the base address.
# ---------------------------------------------------------------------------

function Fetch {
    param([string]$Name, [string]$Destination)
    if ($BaseIsFile) {
        # file://<path>: the tests serve the release from a local directory.
        $LocalRoot = $BaseUrl.Substring(7)
        $LocalRoot = $LocalRoot -replace '/', '\'
        $LocalRoot = $LocalRoot.TrimEnd('\')
        # file:///C:/x becomes \C:\x here; the leading slash before a drive
        # letter or a UNC share name is dropped.
        if ($LocalRoot -match '^\\[A-Za-z]:' -or $LocalRoot -match '^\\\\') {
            $LocalRoot = $LocalRoot.Substring(1)
        }
        $LocalRoot = [System.Uri]::UnescapeDataString($LocalRoot)
        $Source = Join-Path (Join-Path $LocalRoot ('v' + $Version)) $Name
        if (-not (Test-Path -LiteralPath $Source -PathType Leaf)) {
            Fail 'download_failed' @($Name)
        }
        Copy-Item -LiteralPath $Source -Destination $Destination -Force
        return
    }
    $Url = $BaseUrl.TrimEnd('/') + '/v' + $Version + '/' + $Name
    try {
        Invoke-WebRequest -UseBasicParsing -Uri $Url -OutFile $Destination
    }
    catch {
        Fail 'download_failed' @($Name)
    }
}

# Everything from here to the end runs inside a try so that the temporary
# directory is removed in the finally block, also after a failure.
try {

    $ArchiveName = "ulgen_${Version}_windows_${Arch}.zip"
    $ArchivePath = Join-Path $Script:TmpDir $ArchiveName
    $ChecksumsPath = Join-Path $Script:TmpDir 'checksums.txt'

    Fetch $ArchiveName $ArchivePath
    Fetch 'checksums.txt' $ChecksumsPath

    # ---------------------------------------------------------------------
    # The signature of checksums.txt. The release workflow signs the file
    # with cosign, with the workflow's own identity. With cosign installed
    # the signature must be there and must be this repository's release
    # workflow's; without cosign the install goes on with the checksum alone
    # and says so. A server on this machine or a local directory (a mirror, a
    # test) is not asked for a signature.
    # ---------------------------------------------------------------------

    if ($BaseUrl.StartsWith('https://')) {
        if (Get-Command cosign -ErrorAction SilentlyContinue) {
            $BundlePath = Join-Path $Script:TmpDir 'checksums.txt.sigstore.json'
            try {
                Invoke-WebRequest -UseBasicParsing -Uri ($BaseUrl.TrimEnd('/') + '/v' + $Version + '/checksums.txt.sigstore.json') -OutFile $BundlePath
            }
            catch {
                Fail 'signature_missing'
            }
            # Start-Process, not the call operator: cosign reports on standard
            # error, which Windows PowerShell 5.1 turns into a failure when a
            # native command's streams are redirected.
            $CosignArgs = @(
                'verify-blob',
                '--bundle', ('"' + $BundlePath + '"'),
                '--certificate-oidc-issuer', 'https://token.actions.githubusercontent.com',
                '--certificate-identity-regexp', '"^https://github\.com/mftnakrsu/ulgen/\.github/workflows/release\.yml@refs/tags/v"',
                ('"' + $ChecksumsPath + '"')
            )
            $Verified = $false
            try {
                $Cosign = Start-Process -FilePath (Get-Command cosign).Source -ArgumentList $CosignArgs `
                    -Wait -PassThru -NoNewWindow `
                    -RedirectStandardOutput (Join-Path $Script:TmpDir 'cosign.out') `
                    -RedirectStandardError (Join-Path $Script:TmpDir 'cosign.err')
                $Verified = ($Cosign.ExitCode -eq 0)
            }
            catch {
                $Verified = $false
            }
            if (-not $Verified) {
                Fail 'signature_bad'
            }
            Say 'signature_ok'
        } else {
            Say 'signature_unchecked'
        }
    }

    # ---------------------------------------------------------------------
    # The SHA-256 check, before anything is unpacked.
    # ---------------------------------------------------------------------

    $Actual = (Get-FileHash -LiteralPath $ArchivePath -Algorithm SHA256).Hash

    $Expected = $null
    foreach ($Line in (Get-Content -LiteralPath $ChecksumsPath)) {
        $Trimmed = $Line.Trim()
        if (-not $Trimmed) {
            continue
        }
        $Fields = $Trimmed -split '\s+', 2
        if ($Fields.Count -eq 2 -and $Fields[1].TrimStart('*') -eq $ArchiveName) {
            $Expected = $Fields[0]
            break
        }
    }

    if (-not $Expected) {
        Fail 'checksum_missing' @($ArchiveName)
    }
    if ($Expected.ToLowerInvariant() -ne $Actual.ToLowerInvariant()) {
        Fail 'checksum_mismatch' @($ArchiveName)
    }

    # ---------------------------------------------------------------------
    # Unpack and install the binary.
    # ---------------------------------------------------------------------

    $UnpackDir = Join-Path $Script:TmpDir 'unpacked'
    try {
        Expand-Archive -LiteralPath $ArchivePath -DestinationPath $UnpackDir -Force
    }
    catch {
        Fail 'extract_failed' @($ArchiveName)
    }

    $BinarySource = Join-Path $UnpackDir 'ulgen.exe'
    if (-not (Test-Path -LiteralPath $BinarySource -PathType Leaf)) {
        Fail 'binary_missing'
    }

    $InstallDir = $env:ULGEN_INSTALL_DIR
    if (-not $InstallDir) {
        $InstallDir = Join-Path $env:LOCALAPPDATA 'Programs\ulgen'
    }

    New-Item -ItemType Directory -Path $InstallDir -Force | Out-Null

    $Staged = Join-Path $InstallDir ('.ulgen.tmp.' + [System.Guid]::NewGuid().ToString('N'))
    Copy-Item -LiteralPath $BinarySource -Destination $Staged -Force
    Move-Item -LiteralPath $Staged -Destination (Join-Path $InstallDir 'ulgen.exe') -Force

    Say 'installed' @($InstallDir)

    try {
        $VersionText = & (Join-Path $InstallDir 'ulgen.exe') --version 2>$null
        if ($VersionText) {
            Say 'binary_version' @($VersionText)
        }
    }
    catch {
        # A binary that cannot be run says nothing; the install itself is done.
    }

    # ---------------------------------------------------------------------
    # The user's PATH. The machine's PATH is never touched.
    # ---------------------------------------------------------------------

    if (-not $NoPathUpdate) {
        $NormalizedDir = $InstallDir.TrimEnd('\')
        $OnPath = $false
        $UserPath = [Environment]::GetEnvironmentVariable('Path', 'User')
        if ($UserPath) {
            foreach ($Entry in $UserPath.Split(';')) {
                if (-not $Entry) {
                    continue
                }
                if ($Entry.TrimEnd('\') -ieq $NormalizedDir) {
                    $OnPath = $true
                    break
                }
            }
        }
        if (-not $OnPath) {
            if ($UserPath) {
                $NewPath = $UserPath.TrimEnd(';') + ';' + $InstallDir
            } else {
                $NewPath = $InstallDir
            }
            [Environment]::SetEnvironmentVariable('Path', $NewPath, 'User')
            Say 'path_added'
        }
    }
}
finally {
    if ($Script:TmpDir) {
        Remove-Item -LiteralPath $Script:TmpDir -Recurse -Force -ErrorAction SilentlyContinue
        $Script:TmpDir = $null
    }
}
}

# Run only when the script itself is invoked, not when it is dot-sourced.
# Tests dot-source this file to reach Install-Ulgen without running it.
if ($MyInvocation.InvocationName -ne '.') {
    # @args forwards the arguments the script was called with (-NoPathUpdate,
    # -Help); powershell.exe takes the rest.
    Install-Ulgen @args
}