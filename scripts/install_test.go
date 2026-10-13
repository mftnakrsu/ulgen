//go:build !windows

// Package scripts holds the test for scripts/install.sh, the Unix installer.
// It builds an invented release in a temporary directory, serves it from a
// local HTTP server and runs the installer against it.
package scripts

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

const testVersion = "9.9.9"

// scriptPath finds scripts/install.sh through this file's own location, not
// the working directory.
func scriptPath(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	return filepath.Join(filepath.Dir(thisFile), "install.sh")
}

// buildArchive makes a tar.gz carrying an executable named ulgen that prints
// the invented version.
func buildArchive(t *testing.T, scriptBody string) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	body := []byte(scriptBody)
	hdr := &tar.Header{
		Name: "ulgen",
		Mode: 0o755,
		Size: int64(len(body)),
	}
	if err := tw.WriteHeader(hdr); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write(body); err != nil {
		t.Fatal(err)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// sha256hex returns the lowercase hexadecimal SHA-256 of b.
func sha256hex(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// release is the invented set of files served from the test server.
type release struct {
	archive  []byte
	deb      []byte
	checksum []string // lines of checksums.txt
	bundle   []byte   // checksums.txt.sigstore.json; nil means the release has none
}

// releaseHandler answers a release's files under /v<version>/.
func releaseHandler(rel *release) http.Handler {
	prefix := "/v" + testVersion + "/"
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, prefix) {
			http.NotFound(w, r)
			return
		}
		name := strings.TrimPrefix(r.URL.Path, prefix)
		switch {
		case name == "checksums.txt.sigstore.json" && rel.bundle != nil:
			_, _ = w.Write(rel.bundle)
		case name == "checksums.txt":
			_, _ = w.Write([]byte(strings.Join(rel.checksum, "\n") + "\n"))
		case strings.HasSuffix(name, ".tar.gz"):
			_, _ = w.Write(rel.archive)
		case strings.HasSuffix(name, ".deb"):
			_, _ = w.Write(rel.deb)
		default:
			http.NotFound(w, r)
		}
	})
	return mux
}

// serve starts a test server that answers under /v<version>/ and counts the
// requests that reach it.
func serve(t *testing.T, rel *release) (*httptest.Server, *int32) {
	t.Helper()
	var hits int32
	counter := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		releaseHandler(rel).ServeHTTP(w, r)
	})
	srv := httptest.NewServer(counter)
	t.Cleanup(srv.Close)
	return srv, &hits
}

// runResult is what a run of the installer produced.
type runResult struct {
	exit   int
	stdout string
	stderr string
}

// runOptions describe one run of the installer.
type runOptions struct {
	baseURL    string
	version    string
	installAt  string // ULGEN_INSTALL_DIR
	home       string
	workDir    string
	tmpDir     string
	lang       string
	unameM     string
	pathPrefix string // prepended to PATH, for a canary
	verify     bool   // ULGEN_TEST_VERIFY: check the signature although the server is local
	args       []string
}

// runInstaller runs scripts/install.sh with a controlled environment and a
// 60 second deadline.
func runInstaller(t *testing.T, o runOptions) runResult {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	path := os.Getenv("PATH")
	if o.pathPrefix != "" {
		path = o.pathPrefix + ":" + path
	}

	cmd := exec.CommandContext(ctx, "sh", scriptPath(t))
	cmd.Args = append(cmd.Args, o.args...)
	cmd.Dir = o.workDir
	cmd.Env = []string{
		"PATH=" + path,
		"HOME=" + o.home,
		"TMPDIR=" + o.tmpDir,
		"LANG=" + o.lang,
		"LC_ALL=",
		"LC_MESSAGES=",
		"ULGEN_BASE_URL=" + o.baseURL,
		"ULGEN_VERSION=" + o.version,
		"ULGEN_INSTALL_DIR=" + o.installAt,
		"ULGEN_TEST_UNAME_M=" + o.unameM,
	}
	if o.verify {
		cmd.Env = append(cmd.Env, "ULGEN_TEST_VERIFY=1")
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	res := runResult{stdout: stdout.String(), stderr: stderr.String()}
	if err == nil {
		res.exit = 0
	} else {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			res.exit = ee.ExitCode()
		} else {
			res.exit = -1
		}
	}
	return res
}

// TestInstall runs the installer against an invented release.
func TestInstall(t *testing.T) {
	for _, tool := range []string{"sh", "tar"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skipf("%s is not available", tool)
		}
	}
	shaFound := false
	for _, tool := range []string{"sha256sum", "shasum", "openssl"} {
		if _, err := exec.LookPath(tool); err == nil {
			shaFound = true
			break
		}
	}
	if !shaFound {
		t.Skip("no SHA-256 tool (sha256sum, shasum or openssl) is available")
	}

	sysArch := runtime.GOARCH
	if sysArch != "amd64" && sysArch != "arm64" {
		t.Skipf("unsupported processor %s", sysArch)
	}
	if runtime.GOOS != "linux" && runtime.GOOS != "darwin" {
		t.Skipf("unsupported system %s", runtime.GOOS)
	}

	scriptBody := "#!/bin/sh\necho 'ulgen 9.9.9 (test)'\n"
	archive := buildArchive(t, scriptBody)
	debBytes := []byte("not a real package\n")

	archiveName := fmt.Sprintf("ulgen_%s_%s_%s.tar.gz", testVersion, runtime.GOOS, sysArch)
	debName := fmt.Sprintf("ulgen_%s_%s.deb", testVersion, sysArch)

	newRelease := func() *release {
		return &release{
			archive: archive,
			deb:     debBytes,
			checksum: []string{
				sha256hex(archive) + "  " + archiveName,
				sha256hex(debBytes) + "  " + debName,
			},
		}
	}

	// env is a fresh temporary tree for one subtest.
	type env struct {
		home    string
		workDir string
		tmpDir  string
		install string
	}
	setup := func(t *testing.T) env {
		t.Helper()
		root := t.TempDir()
		e := env{
			home:    filepath.Join(root, "home"),
			workDir: filepath.Join(root, "work"),
			tmpDir:  filepath.Join(root, "tmp"),
			install: filepath.Join(root, "install"),
		}
		for _, d := range []string{e.home, e.workDir, e.tmpDir, e.install} {
			if err := os.MkdirAll(d, 0o755); err != nil {
				t.Fatal(err)
			}
		}
		return e
	}

	// assertTmpEmpty checks that the test's TMPDIR was left empty.
	assertTmpEmpty := func(t *testing.T, e env) {
		t.Helper()
		entries, err := os.ReadDir(e.tmpDir)
		if err != nil {
			t.Fatal(err)
		}
		if len(entries) != 0 {
			names := make([]string, 0, len(entries))
			for _, en := range entries {
				names = append(names, en.Name())
			}
			t.Errorf("TMPDIR is not empty: %v", names)
		}
	}

	t.Run("a good archive is installed", func(t *testing.T) {
		e := setup(t)
		srv, _ := serve(t, newRelease())
		res := runInstaller(t, runOptions{
			baseURL: srv.URL, version: testVersion,
			installAt: e.install, home: e.home,
			workDir: e.workDir, tmpDir: e.tmpDir, lang: "C",
		})
		if res.exit != 0 {
			t.Fatalf("exit = %d, stderr = %q", res.exit, res.stderr)
		}
		bin := filepath.Join(e.install, "ulgen")
		info, err := os.Stat(bin)
		if err != nil {
			t.Fatalf("stat: %v", err)
		}
		if info.Mode().Perm()&0o111 == 0 {
			t.Errorf("ulgen is not executable: %v", info.Mode())
		}
		out, err := exec.Command(bin).Output()
		if err != nil {
			t.Fatalf("run ulgen: %v", err)
		}
		if got := string(out); !strings.Contains(got, "9.9.9") {
			t.Errorf("ulgen output = %q, want the invented version", got)
		}
		if !strings.Contains(res.stdout, e.install) {
			t.Errorf("stdout %q does not name the directory", res.stdout)
		}
		assertTmpEmpty(t, e)
	})

	// fakeCosign puts a program named cosign in a directory of its own. It
	// writes its arguments to a file next to it and exits with code.
	fakeCosign := func(t *testing.T, code int) (dir, record string) {
		t.Helper()
		dir = t.TempDir()
		record = filepath.Join(dir, "args")
		body := fmt.Sprintf("#!/bin/sh\nprintf '%%s\\n' \"$@\" > %q\nexit %d\n", record, code)
		if err := os.WriteFile(filepath.Join(dir, "cosign"), []byte(body), 0o755); err != nil {
			t.Fatal(err)
		}
		return dir, record
	}

	t.Run("a good signature is checked with this repository's identity", func(t *testing.T) {
		e := setup(t)
		rel := newRelease()
		rel.bundle = []byte("{}\n")
		srv, _ := serve(t, rel)
		dir, record := fakeCosign(t, 0)
		res := runInstaller(t, runOptions{
			baseURL: srv.URL, version: testVersion, installAt: e.install, home: e.home,
			workDir: e.workDir, tmpDir: e.tmpDir, lang: "C", pathPrefix: dir, verify: true,
		})
		if res.exit != 0 {
			t.Fatalf("exit = %d, stderr = %q", res.exit, res.stderr)
		}
		args, err := os.ReadFile(record)
		if err != nil {
			t.Fatalf("cosign was not run: %v", err)
		}
		for _, want := range []string{
			"verify-blob\n", "--bundle\n",
			"--certificate-oidc-issuer\nhttps://token.actions.githubusercontent.com\n",
			"--certificate-identity-regexp\n^https://github\\.com/mftnakrsu/ulgen/\\.github/workflows/release\\.yml@refs/tags/v\n",
			"/checksums.txt\n",
		} {
			if !strings.Contains(string(args), want) {
				t.Errorf("cosign arguments lack %q:\n%s", want, args)
			}
		}
		if !strings.Contains(res.stdout, "signature") {
			t.Errorf("stdout does not mention the signature: %q", res.stdout)
		}
		assertTmpEmpty(t, e)
	})

	t.Run("a bad signature installs nothing", func(t *testing.T) {
		e := setup(t)
		rel := newRelease()
		rel.bundle = []byte("{}\n")
		srv, _ := serve(t, rel)
		dir, _ := fakeCosign(t, 1)
		res := runInstaller(t, runOptions{
			baseURL: srv.URL, version: testVersion, installAt: e.install, home: e.home,
			workDir: e.workDir, tmpDir: e.tmpDir, lang: "C", pathPrefix: dir, verify: true,
		})
		if res.exit == 0 || !strings.Contains(res.stderr, "signature") {
			t.Fatalf("exit = %d, stderr = %q", res.exit, res.stderr)
		}
		if _, err := os.Stat(filepath.Join(e.install, "ulgen")); !os.IsNotExist(err) {
			t.Errorf("ulgen was installed: %v", err)
		}
		assertTmpEmpty(t, e)
	})

	t.Run("a release without a signature installs nothing when cosign is there", func(t *testing.T) {
		e := setup(t)
		srv, _ := serve(t, newRelease())
		dir, record := fakeCosign(t, 0)
		res := runInstaller(t, runOptions{
			baseURL: srv.URL, version: testVersion, installAt: e.install, home: e.home,
			workDir: e.workDir, tmpDir: e.tmpDir, lang: "C", pathPrefix: dir, verify: true,
		})
		if res.exit == 0 || !strings.Contains(res.stderr, "signature") {
			t.Fatalf("exit = %d, stderr = %q", res.exit, res.stderr)
		}
		if _, err := os.Stat(record); err == nil {
			t.Error("cosign was run without a signature to check")
		}
		if _, err := os.Stat(filepath.Join(e.install, "ulgen")); !os.IsNotExist(err) {
			t.Errorf("ulgen was installed: %v", err)
		}
	})

	t.Run("without cosign the install goes on and says the signature was not checked", func(t *testing.T) {
		if _, err := exec.LookPath("cosign"); err == nil {
			t.Skip("cosign is installed on this machine")
		}
		e := setup(t)
		srv, _ := serve(t, newRelease())
		res := runInstaller(t, runOptions{
			baseURL: srv.URL, version: testVersion, installAt: e.install, home: e.home,
			workDir: e.workDir, tmpDir: e.tmpDir, lang: "C", verify: true,
		})
		if res.exit != 0 || !strings.Contains(res.stdout, "not checked") {
			t.Fatalf("exit = %d, stdout = %q stderr = %q", res.exit, res.stdout, res.stderr)
		}
		if _, err := os.Stat(filepath.Join(e.install, "ulgen")); err != nil {
			t.Errorf("ulgen missing: %v", err)
		}
	})

	t.Run("a version with a leading v works", func(t *testing.T) {
		e := setup(t)
		srv, _ := serve(t, newRelease())
		res := runInstaller(t, runOptions{
			baseURL: srv.URL, version: "v" + testVersion,
			installAt: e.install, home: e.home,
			workDir: e.workDir, tmpDir: e.tmpDir, lang: "C",
		})
		if res.exit != 0 {
			t.Fatalf("exit = %d, stderr = %q", res.exit, res.stderr)
		}
		if _, err := os.Stat(filepath.Join(e.install, "ulgen")); err != nil {
			t.Fatalf("ulgen missing: %v", err)
		}
		assertTmpEmpty(t, e)
	})

	t.Run("a wrong checksum installs nothing", func(t *testing.T) {
		e := setup(t)
		rel := newRelease()
		rel.checksum[0] = strings.Repeat("0", 64) + "  " + archiveName
		srv, _ := serve(t, rel)
		res := runInstaller(t, runOptions{
			baseURL: srv.URL, version: testVersion,
			installAt: e.install, home: e.home,
			workDir: e.workDir, tmpDir: e.tmpDir, lang: "C",
		})
		if res.exit == 0 {
			t.Fatalf("exit = 0, want non-zero")
		}
		if _, err := os.Stat(filepath.Join(e.install, "ulgen")); !os.IsNotExist(err) {
			t.Errorf("ulgen should not exist, err = %v", err)
		}
		if !strings.Contains(res.stderr, "checksum") {
			t.Errorf("stderr %q does not name the checksum", res.stderr)
		}
		assertTmpEmpty(t, e)
	})

	t.Run("an archive missing from checksums installs nothing", func(t *testing.T) {
		e := setup(t)
		rel := newRelease()
		rel.checksum = []string{sha256hex(debBytes) + "  " + debName}
		srv, _ := serve(t, rel)
		res := runInstaller(t, runOptions{
			baseURL: srv.URL, version: testVersion,
			installAt: e.install, home: e.home,
			workDir: e.workDir, tmpDir: e.tmpDir, lang: "C",
		})
		if res.exit == 0 {
			t.Fatalf("exit = 0, want non-zero")
		}
		if _, err := os.Stat(filepath.Join(e.install, "ulgen")); !os.IsNotExist(err) {
			t.Errorf("ulgen should not exist, err = %v", err)
		}
		assertTmpEmpty(t, e)
	})

	t.Run("an unknown processor is refused", func(t *testing.T) {
		e := setup(t)
		srv, _ := serve(t, newRelease())
		res := runInstaller(t, runOptions{
			baseURL: srv.URL, version: testVersion,
			installAt: e.install, home: e.home,
			workDir: e.workDir, tmpDir: e.tmpDir, lang: "C",
			unameM: "mips",
		})
		if res.exit == 0 {
			t.Fatalf("exit = 0, want non-zero")
		}
		if !strings.Contains(res.stderr, "mips") {
			t.Errorf("stderr %q does not name mips", res.stderr)
		}
		assertTmpEmpty(t, e)
	})

	t.Run("--deb leaves the package and prints the apt line", func(t *testing.T) {
		e := setup(t)
		srv, _ := serve(t, newRelease())
		// A directory holding a fake sudo that records its use. It is put
		// first on PATH so that, were the script to call sudo, the canary
		// would notice.
		canaryDir := filepath.Join(e.home, "canary")
		if err := os.MkdirAll(canaryDir, 0o755); err != nil {
			t.Fatal(err)
		}
		sudoMarker := filepath.Join(canaryDir, "sudo-was-run")
		fakeSudo := "#!/bin/sh\ntouch '" + sudoMarker + "'\nexit 0\n"
		if err := os.WriteFile(filepath.Join(canaryDir, "sudo"), []byte(fakeSudo), 0o755); err != nil {
			t.Fatal(err)
		}
		res := runInstaller(t, runOptions{
			baseURL: srv.URL, version: testVersion,
			installAt: e.install, home: e.home,
			workDir: e.workDir, tmpDir: e.tmpDir, lang: "C",
			pathPrefix: canaryDir,
			args:       []string{"--deb"},
		})
		if runtime.GOOS != "linux" {
			if res.exit == 0 {
				t.Fatalf("--deb should be refused off Linux, exit = 0")
			}
			assertTmpEmpty(t, e)
			return
		}
		if res.exit != 0 {
			t.Fatalf("exit = %d, stderr = %q", res.exit, res.stderr)
		}
		debPath := filepath.Join(e.workDir, debName)
		if _, err := os.Stat(debPath); err != nil {
			t.Fatalf("the .deb is not in the working directory: %v", err)
		}
		if !strings.Contains(res.stdout, "sudo apt install ./ulgen_9.9.9_") {
			t.Errorf("stdout %q lacks the apt line", res.stdout)
		}
		if _, err := os.Stat(sudoMarker); !os.IsNotExist(err) {
			t.Errorf("something was run with sudo: %v", err)
		}
		assertTmpEmpty(t, e)
	})

	t.Run("an http address of another host is refused", func(t *testing.T) {
		e := setup(t)
		// The release server stays up as a canary: the refused address must
		// be turned down before anything is downloaded, so no request may
		// reach it.
		_, hits := serve(t, newRelease())
		res := runInstaller(t, runOptions{
			baseURL: "http://example.invalid/x", version: testVersion,
			installAt: e.install, home: e.home,
			workDir: e.workDir, tmpDir: e.tmpDir, lang: "C",
		})
		if res.exit == 0 {
			t.Fatalf("exit = 0, want non-zero")
		}
		if n := atomic.LoadInt32(hits); n != 0 {
			t.Errorf("the test server received %d requests", n)
		}
		assertTmpEmpty(t, e)
	})

	t.Run("an http loopback address with userinfo is refused", func(t *testing.T) {
		e := setup(t)
		// The release server stays up as a canary: a userinfo part makes
		// the host name the real one (evil.com), so the address must be
		// turned down before anything is downloaded.
		_, hits := serve(t, newRelease())
		res := runInstaller(t, runOptions{
			baseURL: "http://localhost:x@evil.com/", version: testVersion,
			installAt: e.install, home: e.home,
			workDir: e.workDir, tmpDir: e.tmpDir, lang: "C",
		})
		if res.exit == 0 {
			t.Fatalf("exit = 0, want non-zero")
		}
		if n := atomic.LoadInt32(hits); n != 0 {
			t.Errorf("the test server received %d requests", n)
		}
		assertTmpEmpty(t, e)
	})

	t.Run("a mirror without a version is refused", func(t *testing.T) {
		e := setup(t)
		srv, _ := serve(t, newRelease())
		res := runInstaller(t, runOptions{
			baseURL: srv.URL, version: "",
			installAt: e.install, home: e.home,
			workDir: e.workDir, tmpDir: e.tmpDir, lang: "C",
		})
		if res.exit == 0 {
			t.Fatalf("exit = 0, want non-zero")
		}
		if !strings.Contains(res.stderr, "ULGEN_VERSION") {
			t.Errorf("stderr %q does not mention ULGEN_VERSION", res.stderr)
		}
		assertTmpEmpty(t, e)
	})

	t.Run("an unknown argument exits 2", func(t *testing.T) {
		e := setup(t)
		res := runInstaller(t, runOptions{
			version:   testVersion,
			installAt: e.install, home: e.home,
			workDir: e.workDir, tmpDir: e.tmpDir, lang: "C",
			args: []string{"--nonsense"},
		})
		if res.exit != 2 {
			t.Fatalf("exit = %d, want 2", res.exit)
		}
	})

	t.Run("Turkish messages", func(t *testing.T) {
		e := setup(t)
		srv, _ := serve(t, newRelease())
		res := runInstaller(t, runOptions{
			baseURL: srv.URL, version: testVersion,
			installAt: e.install, home: e.home,
			workDir: e.workDir, tmpDir: e.tmpDir, lang: "tr_TR.UTF-8",
		})
		if res.exit != 0 {
			t.Fatalf("exit = %d, stderr = %q", res.exit, res.stderr)
		}
		if !strings.Contains(res.stdout, "kuruldu") {
			t.Errorf("stdout %q lacks the Turkish word", res.stdout)
		}
		if strings.Contains(res.stdout, "was installed") {
			t.Errorf("stdout %q holds the English word", res.stdout)
		}
		assertTmpEmpty(t, e)
	})

	t.Run("the temporary directory is removed after a run", func(t *testing.T) {
		e := setup(t)
		srv, _ := serve(t, newRelease())
		res := runInstaller(t, runOptions{
			baseURL: srv.URL, version: testVersion,
			installAt: e.install, home: e.home,
			workDir: e.workDir, tmpDir: e.tmpDir, lang: "C",
		})
		if res.exit != 0 {
			t.Fatalf("exit = %d, stderr = %q", res.exit, res.stderr)
		}
		assertTmpEmpty(t, e)
	})

	t.Run("an older ulgen is replaced", func(t *testing.T) {
		e := setup(t)
		old := filepath.Join(e.install, "ulgen")
		if err := os.WriteFile(old, []byte("#!/bin/sh\necho old\n"), 0o755); err != nil {
			t.Fatal(err)
		}
		srv, _ := serve(t, newRelease())
		res := runInstaller(t, runOptions{
			baseURL: srv.URL, version: testVersion,
			installAt: e.install, home: e.home,
			workDir: e.workDir, tmpDir: e.tmpDir, lang: "C",
		})
		if res.exit != 0 {
			t.Fatalf("exit = %d, stderr = %q", res.exit, res.stderr)
		}
		out, err := exec.Command(old).Output()
		if err != nil {
			t.Fatalf("run ulgen: %v", err)
		}
		if !strings.Contains(string(out), "9.9.9") {
			t.Errorf("ulgen output = %q, want the invented version", out)
		}
		assertTmpEmpty(t, e)
	})
}
