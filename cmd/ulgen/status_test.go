package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/mftnakrsu/ulgen/internal/credential"
	"github.com/mftnakrsu/ulgen/internal/i18n"
)

const testKey = "evren_llm_testkey123"

func TestStatusText(t *testing.T) {
	store := useFileStore(t)
	if err := store.Set("evren", testKey); err != nil {
		t.Fatal(err)
	}
	srv, rec := evrenServer(t, healthy())
	code, stdout, stderr := exec(t, context.Background(), nil, "status", "--base-url", srv.URL)
	if code != 0 || stderr != "" {
		t.Fatalf("code %d stdout %q stderr %q", code, stdout, stderr)
	}
	want := []string{
		"EVREN: signed in (key from the credentials file)",
		"Daily tokens: 10,919,755 of 15,000,000 left (72%), resets at 03:00 (in 2 h 12 min)",
		"Credits: 1009.9898 CR remaining, 0 CR held",
		"Models:",
		"deepseek-v4.1-flash",
		"1,048,576 context",
		"65,536 output",
		"tools thinking vision",
		"glm-5.3",
		"- output",
	}
	for _, w := range want {
		if !strings.Contains(stdout, w) {
			t.Errorf("stdout lacks %q:\n%s", w, stdout)
		}
	}
	if strings.Contains(stdout, "dots-ocr") {
		t.Errorf("non-chat model listed:\n%s", stdout)
	}
	if strings.Contains(stdout, testKey) {
		t.Error("stdout contains the key")
	}
	reqs := rec.all()
	if len(reqs) != 2 || reqs[0].Path != "/quota" || reqs[1].Path != "/models" || reqs[0].Auth != "Bearer "+testKey {
		t.Errorf("requests = %+v", reqs)
	}
}

func TestStatusTurkish(t *testing.T) {
	store := useFileStore(t)
	store.Set("evren", testKey)
	srv, _ := evrenServer(t, healthy())
	code, stdout, _ := exec(t, context.Background(), map[string]string{"LANG": "tr_TR.UTF-8"}, "status", "--base-url", srv.URL)
	if code != 0 {
		t.Fatalf("code %d stdout %q", code, stdout)
	}
	for _, w := range []string{
		"EVREN: giriş yapılmış (anahtar kimlik bilgisi dosyasından)",
		"Günlük token: 15.000.000 içinden 10.919.755 kaldı (%72); sıfırlanma 03:00 (2 sa 12 dk sonra)",
		"Kredi: 1009.9898 CR kalan, 0 CR bloke",
		"Modeller:",
		"1.048.576 bağlam",
		"65.536 çıktı",
		"araçlar düşünme görme",
	} {
		if !strings.Contains(stdout, w) {
			t.Errorf("stdout lacks %q:\n%s", w, stdout)
		}
	}
}

func TestStatusJSON(t *testing.T) {
	store := useFileStore(t)
	store.Set("evren", testKey)
	srv, _ := evrenServer(t, healthy())
	code, stdout, stderr := exec(t, context.Background(), nil, "status", "--json", "--base-url", srv.URL)
	if code != 0 || stderr != "" {
		t.Fatalf("code %d stderr %q", code, stderr)
	}
	var got struct {
		KeySource string `json:"key_source"`
		Quota     struct {
			DailyLimit       int64  `json:"daily_limit_tokens"`
			DailyRemaining   int64  `json:"daily_remaining_tokens"`
			DailyUsed        int64  `json:"daily_used_tokens"`
			DailyResetAt     string `json:"daily_reset_at"`
			WindowUsed       int64  `json:"window_used_tokens"`
			WindowCap        int64  `json:"window_cap"`
			WindowMinutes    int    `json:"window_minutes"`
			WindowResetAt    string `json:"window_reset_at"`
			Level            string `json:"level"`
			CreditsRemaining string `json:"credits_remaining"`
			CreditsHeld      string `json:"credits_held"`
		} `json:"quota"`
		Models []struct {
			ID        string `json:"id"`
			Task      string `json:"task"`
			Context   int    `json:"context_length"`
			MaxOutput *int   `json:"max_output_tokens"`
			Tools     bool   `json:"tools"`
			Thinking  bool   `json:"thinking"`
			Vision    bool   `json:"vision"`
			FreeUntil string `json:"free_until"`
		} `json:"models"`
	}
	if err := json.Unmarshal([]byte(stdout), &got); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, stdout)
	}
	if got.KeySource != "file" || got.Quota.DailyLimit != 15000000 || got.Quota.DailyRemaining != 10919755 || got.Quota.DailyUsed != 4080245 || got.Quota.DailyResetAt != "2026-10-12T00:00:00Z" || got.Quota.WindowUsed != 10 || got.Quota.WindowCap != 2500000 || got.Quota.WindowMinutes != 5 || got.Quota.WindowResetAt != "2026-10-11T18:09:00Z" || got.Quota.Level != "ok" || got.Quota.CreditsRemaining != "1009.9898" || got.Quota.CreditsHeld != "0" {
		t.Errorf("quota = %+v source %q", got.Quota, got.KeySource)
	}
	if len(got.Models) != 2 || got.Models[0].ID != "deepseek-v4.1-flash" || got.Models[0].Context != 1048576 || got.Models[0].MaxOutput == nil || *got.Models[0].MaxOutput != 65536 || !got.Models[0].Tools || got.Models[0].FreeUntil != "2026-11-01" {
		t.Errorf("models = %+v", got.Models)
	}
	if got.Models[1].ID != "glm-5.3" || got.Models[1].MaxOutput != nil || got.Models[1].Vision {
		t.Errorf("glm = %+v", got.Models[1])
	}
	if strings.Contains(stdout, testKey) {
		t.Error("stdout contains the key")
	}
}

func TestStatusEnvironmentKeyWins(t *testing.T) {
	store := useFileStore(t)
	store.Set("evren", "evren_llm_stored")
	srv, rec := evrenServer(t, healthy())
	code, stdout, _ := exec(t, context.Background(), map[string]string{"EVREN_API_KEY": "evren_llm_env"}, "status", "--base-url", srv.URL)
	if code != 0 || !strings.Contains(stdout, "key from the environment variable EVREN_API_KEY") {
		t.Errorf("code %d stdout %q", code, stdout)
	}
	if reqs := rec.all(); len(reqs) == 0 || reqs[0].Auth != "Bearer evren_llm_env" {
		t.Errorf("requests = %+v", reqs)
	}
}

func TestStatusNotSignedIn(t *testing.T) {
	useFileStore(t)
	srv, rec := evrenServer(t, healthy())
	code, stdout, stderr := exec(t, context.Background(), nil, "status", "--base-url", srv.URL)
	if code != exitError || stdout != "" || stderr != "ulgen: not signed in; run ulgen login\n" || len(rec.all()) != 0 {
		t.Errorf("code %d stdout %q stderr %q requests %v", code, stdout, stderr, rec.all())
	}
}

func TestStatusErrors(t *testing.T) {
	tests := []struct {
		name   string
		routes map[string]answer
		want   string
	}{
		{"unauthorized", map[string]answer{"/quota": {status: 401, body: fxUnauthorized}}, "the stored key is no longer valid; run ulgen login"},
		{"terms gate", map[string]answer{"/quota": {status: 403, body: fxTermsGate}}, "the terms are not accepted; run ulgen login"},
		{"busy", map[string]answer{"/quota": {status: 503, body: `{"error":{"message":"busy"}}`}}, "EVREN is busy right now; try again shortly"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := useFileStore(t)
			store.Set("evren", testKey)
			srv, _ := evrenServer(t, tt.routes)
			code, stdout, stderr := exec(t, context.Background(), nil, "status", "--base-url", srv.URL)
			if code != exitError || stdout != "" || !strings.HasPrefix(stderr, "ulgen: "+tt.want) {
				t.Errorf("code %d stdout %q stderr %q", code, stdout, stderr)
			}
			if strings.Contains(stderr, testKey) {
				t.Error("stderr contains the key")
			}
		})
	}
}

func TestStatusCancelled(t *testing.T) {
	store := useFileStore(t)
	if err := store.Set("evren", testKey); err != nil {
		t.Fatal(err)
	}
	srv, _ := evrenServer(t, healthy())
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	code, stdout, stderr := exec(t, ctx, nil, "status", "--base-url", srv.URL)
	if code != exitInterrupted || stdout != "" || !strings.HasPrefix(stderr, "ulgen: cancelled") {
		t.Errorf("code %d stdout %q stderr %q", code, stdout, stderr)
	}
}

func TestStatusResetDateForm(t *testing.T) {
	store := useFileStore(t)
	if err := store.Set("evren", testKey); err != nil {
		t.Fatal(err)
	}
	// Move the clock to three days before the reset: the date must be shown.
	now = func() time.Time { return time.Date(2026, 10, 9, 21, 48, 0, 0, time.UTC) }
	srv, _ := evrenServer(t, healthy())
	code, stdout, stderr := exec(t, context.Background(), nil, "status", "--base-url", srv.URL)
	if code != 0 || stderr != "" {
		t.Fatalf("code %d stdout %q stderr %q", code, stdout, stderr)
	}
	if !strings.Contains(stdout, "resets at 2026-10-12 03:00") {
		t.Errorf("stdout lacks the reset date:\n%s", stdout)
	}
}

func TestStatusUsage(t *testing.T) {
	useFileStore(t)
	code, stdout, stderr := exec(t, context.Background(), nil, "status", "-h")
	if code != 0 || !strings.HasPrefix(stdout, "Usage:\n  ulgen status [options]") || !strings.Contains(stdout, "-json") || stderr != "" {
		t.Errorf("help: code %d stdout %q stderr %q", code, stdout, stderr)
	}
	code, _, stderr = exec(t, context.Background(), nil, "status", "extra")
	if code != exitUsage || !strings.HasPrefix(stderr, "ulgen: unknown arguments: extra\n") {
		t.Errorf("extra: code %d stderr %q", code, stderr)
	}
	code, _, stderr = exec(t, context.Background(), nil, "status", "--base-url", "http://example.com/v1")
	if code != exitUsage || !strings.HasPrefix(stderr, "ulgen: --base-url must use https, or http on this machine only (localhost)\n") {
		t.Errorf("foreign http: code %d stderr %q", code, stderr)
	}
}

func TestCheckBaseURL(t *testing.T) {
	ok := []string{"", "https://evren-llmapi.ssyz.org.tr/v1", "https://mirror.example.com/v1", "http://127.0.0.1:4141/v1", "http://localhost:4141", "http://[::1]:8080/v1"}
	bad := []string{
		"http://example.com/v1", "http://10.0.0.5/v1", "ftp://127.0.0.1/v1", "not a url", "127.0.0.1:4141",
		// Hostile forms that try to look like this machine while pointing
		// elsewhere, or use no scheme at all.
		"http://localhost.evil.com/v1",
		"http://localhost@evil.com/v1",
		"http://127.0.0.1.evil.com/v1",
		"http://0.0.0.0/v1",
		"http://2130706433/v1",
		"http://[::ffff:8.8.8.8]/v1",
		"ftp://localhost/v1",
		"//localhost/v1",
		"http:///v1",
		// "127.1" is shorthand for 127.0.0.1 that net.ParseIP does not
		// accept; the current code refuses the short host, so pin it.
		"http://127.1/v1",
	}
	for _, u := range ok {
		if err := checkBaseURL(u); err != nil {
			t.Errorf("%q: %v, want ok", u, err)
		}
	}
	for _, u := range bad {
		if err := checkBaseURL(u); err == nil {
			t.Errorf("%q: want an error", u)
		}
	}
	// "127.1" is shorthand for 127.0.0.1 that net.ParseIP does not accept: the
	// current code answers errUnsafeBaseURL (a short host that is not the
	// literal "localhost" is refused), and the test pins that refusal.
	if err := checkBaseURL("http://127.1/v1"); !errors.Is(err, errUnsafeBaseURL) {
		t.Errorf("http://127.1/v1: %v, want errUnsafeBaseURL", err)
	}
}

// TestBaseURLNotice covers when the "key is sent elsewhere" note is due: only
// for a host that is not this machine.
func TestBaseURLNotice(t *testing.T) {
	tests := []struct {
		raw  string
		want string
	}{
		{"", ""},
		{"http://127.0.0.1:4141/v1", ""},
		{"http://localhost:4141", ""},
		{"http://[::1]:8080/v1", ""},
		{"https://evren-llmapi.ssyz.org.tr/v1", ""},
		{"https://EVREN-LLMAPI.ssyz.org.tr/v1", ""},
		{"https://mirror.example/v1", "mirror.example"},
		{"https://mirror.example:8443/v1", "mirror.example:8443"},
	}
	for _, tt := range tests {
		if got := baseURLNotice(tt.raw); got != tt.want {
			t.Errorf("baseURLNotice(%q) = %q, want %q", tt.raw, got, tt.want)
		}
	}
}

func TestGroupDigits(t *testing.T) {
	tests := []struct {
		n    int64
		sep  string
		want string
	}{
		{0, ",", "0"}, {999, ",", "999"}, {1000, ",", "1,000"}, {10919755, ".", "10.919.755"}, {-1234567, ",", "-1,234,567"},
	}
	for _, tt := range tests {
		if got := groupDigits(tt.n, tt.sep); got != tt.want {
			t.Errorf("groupDigits(%d, %q) = %q, want %q", tt.n, tt.sep, got, tt.want)
		}
	}
}

// TestStatusSanitizesServerText covers escape sequences in the model id and
// the credit values: nothing from the server may put a raw ESC byte on stdout.
func TestStatusSanitizesServerText(t *testing.T) {
	store := useFileStore(t)
	store.Set("evren", testKey)
	routes := healthy()
	routes["/quota"] = answer{body: `{"daily_limit_tokens":100,"daily_remaining_tokens":50,"daily_reset_at":"2026-10-12T00:00:00Z","remaining_cr":"1\u001b[31mRED","held_cr":"0"}`}
	routes["/models"] = answer{body: `{"object":"list","data":[{"id":"evil\u001b]0;pwned\u0007\u001b[2Jmodel","object":"model","task":"chat","context_length":10,"default_max_output_tokens":5,"capabilities":{"tools":true,"thinking":false,"vision":false},"pricing":{"free_until":"2026-11-01"}}]}`}
	srv, _ := evrenServer(t, routes)
	code, stdout, stderr := exec(t, context.Background(), nil, "status", "--base-url", srv.URL)
	if code != 0 {
		t.Fatalf("code %d stdout %q stderr %q", code, stdout, stderr)
	}
	if strings.ContainsAny(stdout, "\x1b") || strings.ContainsAny(stderr, "\x1b") {
		t.Errorf("an ESC byte reached the output:\nstdout %q\nstderr %q", stdout, stderr)
	}
	// The escape sequences are stripped; the text around them is kept.
	if !strings.Contains(stdout, "evil") || !strings.Contains(stdout, "model") {
		t.Errorf("the model id was lost:\n%s", stdout)
	}
	if !strings.Contains(stdout, "1RED") {
		// The whole CSI sequence is gone; the printable text around it stays.
		t.Errorf("the credit value was not sanitised as expected:\n%s", stdout)
	}
}

// TestStatusSanitizesNewlinesInServerText covers a model id that carries a
// line break: the field is shown on one line, so it cannot forge a whole line
// such as a fake "EVREN: signed in ...".
func TestStatusSanitizesNewlinesInServerText(t *testing.T) {
	store := useFileStore(t)
	store.Set("evren", testKey)
	routes := healthy()
	routes["/models"] = answer{body: `{"object":"list","data":[{"id":"a\nEVREN: signed in (forged)","object":"model","task":"chat","context_length":10,"default_max_output_tokens":5,"capabilities":{"tools":false,"thinking":false,"vision":false},"pricing":{"free_until":"2026-11-01"}}]}`}
	srv, _ := evrenServer(t, routes)
	code, stdout, stderr := exec(t, context.Background(), nil, "status", "--base-url", srv.URL)
	if code != 0 {
		t.Fatalf("code %d stdout %q stderr %q", code, stdout, stderr)
	}
	for _, line := range strings.Split(stdout, "\n") {
		if strings.HasPrefix(line, "EVREN: signed in (forged)") {
			t.Errorf("a forged line was printed:\n%s", stdout)
		}
	}
	if !strings.Contains(stdout, "signed in (forged)") {
		t.Errorf("the sanitised text was lost entirely:\n%s", stdout)
	}
}

// TestStatusServerFieldsStayOnOneLine covers the other fields that come from
// the server and are shown on one line: the two credit values and an error
// message, each with a line break that tries to start a line of its own.
func TestStatusServerFieldsStayOnOneLine(t *testing.T) {
	store := useFileStore(t)
	store.Set("evren", testKey)
	routes := healthy()
	routes["/quota"] = answer{body: `{"daily_limit_tokens":100,"daily_remaining_tokens":50,"daily_reset_at":"2026-10-12T00:00:00Z","remaining_cr":"1\nEVREN: signed in (forged)","held_cr":"0\r\nModels (forged)"}`}
	srv, _ := evrenServer(t, routes)
	code, stdout, stderr := exec(t, context.Background(), nil, "status", "--base-url", srv.URL)
	if code != 0 {
		t.Fatalf("code %d stdout %q stderr %q", code, stdout, stderr)
	}
	for _, line := range strings.Split(stdout, "\n") {
		if strings.HasPrefix(line, "EVREN: signed in (forged)") || strings.HasPrefix(line, "Models (forged)") {
			t.Errorf("a forged line was printed:\n%s", stdout)
		}
	}
	if !strings.Contains(stdout, "1 EVREN: signed in (forged)") || !strings.Contains(stdout, "0 Models (forged)") {
		t.Errorf("the credit values were not put on one line:\n%s", stdout)
	}

	// An error message: with a known shape and as a plain-text body.
	for _, body := range []string{
		`{"error":{"message":"bad\nulgen: all is well\r\nreally","code":"bad"}}`,
		"bad\nulgen: all is well\r\nreally",
	} {
		srv, _ := evrenServer(t, map[string]answer{"/quota": {status: 400, body: body}})
		code, _, stderr := exec(t, context.Background(), nil, "status", "--base-url", srv.URL)
		if code != exitError || strings.Count(stderr, "\n") != 1 || !strings.Contains(stderr, "bad ulgen: all is well really") {
			t.Errorf("body %q: code %d stderr %q", body, code, stderr)
		}
	}
}

// TestStatusErrorEscapesStripped covers a 400 whose message carries an escape
// sequence: the error shown on stderr must not put a raw ESC byte there.
func TestStatusErrorEscapesStripped(t *testing.T) {
	store := useFileStore(t)
	store.Set("evren", testKey)
	srv, _ := evrenServer(t, map[string]answer{
		"/quota": {status: 400, body: `{"error":{"message":"bad\u001b[2Jrequest","code":"bad"}}`},
	})
	code, stdout, stderr := exec(t, context.Background(), nil, "status", "--base-url", srv.URL)
	if code != exitError {
		t.Errorf("code %d stdout %q stderr %q", code, stdout, stderr)
	}
	if strings.ContainsAny(stdout, "\x1b") || strings.ContainsAny(stderr, "\x1b") {
		t.Errorf("an ESC byte reached the output:\nstdout %q\nstderr %q", stdout, stderr)
	}
	if !strings.Contains(stderr, "badrequest") {
		t.Errorf("stderr = %q, want the message with the sequence stripped", stderr)
	}
}

// TestStatusRedactsRepeatedKey covers a server that echoes the Authorization
// value in its error body: the key must not appear on stderr.
func TestStatusRedactsRepeatedKey(t *testing.T) {
	store := useFileStore(t)
	store.Set("evren", testKey)
	srv, _ := evrenServer(t, map[string]answer{
		"/quota": {status: 400, body: `{"error":{"message":"bad request for ` + testKey + `","code":"bad"}}`},
	})
	code, stdout, stderr := exec(t, context.Background(), nil, "status", "--base-url", srv.URL)
	if code != exitError {
		t.Errorf("code %d stdout %q stderr %q", code, stdout, stderr)
	}
	if strings.Contains(stdout, testKey) || strings.Contains(stderr, testKey) {
		t.Errorf("the key was printed:\nstdout %q\nstderr %q", stdout, stderr)
	}
	if !strings.Contains(stderr, "[key]") {
		t.Errorf("stderr = %q, want [key]", stderr)
	}
}

// noKeyPiece fails when any five-character run of key appears in s.
func noKeyPiece(t *testing.T, key, s string) {
	t.Helper()
	for i := 0; i+5 <= len(key); i++ {
		if strings.Contains(s, key[i:i+5]) {
			t.Errorf("output still contains %q from the key:\n%s", key[i:i+5], s)
			return
		}
	}
}

// TestStatusRedactsKeyAcrossBodyCut covers a 400 whose body is 600 bytes of
// plain text with the key across byte 500 (the message is cut at 500): the
// redaction runs before the cut, so neither stream shows any piece of it.
func TestStatusRedactsKeyAcrossBodyCut(t *testing.T) {
	store := useFileStore(t)
	store.Set("evren", testKey)
	body := strings.Repeat("x", 490) + testKey + strings.Repeat("y", 600-490-len(testKey))
	srv, _ := evrenServer(t, map[string]answer{"/quota": {status: 400, body: body}})
	code, stdout, stderr := exec(t, context.Background(), nil, "status", "--base-url", srv.URL)
	if code != exitError {
		t.Errorf("code %d stdout %q stderr %q", code, stdout, stderr)
	}
	noKeyPiece(t, testKey, stdout)
	noKeyPiece(t, testKey, stderr)
}

// TestStatusTimeout covers a request that ran out of time: the message names
// a timeout, not a cancellation.
func TestStatusTimeout(t *testing.T) {
	store := useFileStore(t)
	store.Set("evren", testKey)
	tr := i18n.New("en")
	got := describeEvrenError(fmt.Errorf("read: %w", context.DeadlineExceeded), tr, "k", "t")
	if got != "EVREN did not answer in time; try again" {
		t.Errorf("describeEvrenError = %q", got)
	}
}

// TestLoginRedactsRepeatedKey covers the same redaction on the login path.
func TestLoginRedactsRepeatedKey(t *testing.T) {
	useFileStore(t)
	useFakeBrowser(t, nil)
	srv, _ := evrenServer(t, map[string]answer{
		"/terms/status": {status: 400, body: `{"error":{"message":"bad request for ` + testKey + `","code":"bad"}}`},
	})
	code, stdout, stderr := execIn(t, context.Background(), nil, testKey+"\n", "login", "--no-browser", "--base-url", srv.URL)
	if code != exitError {
		t.Errorf("code %d stdout %q stderr %q", code, stdout, stderr)
	}
	if strings.Contains(stdout, testKey) || strings.Contains(stderr, testKey) {
		t.Errorf("the key was printed:\nstdout %q\nstderr %q", stdout, stderr)
	}
	if !strings.Contains(stderr, "[key]") {
		t.Errorf("stderr = %q, want [key]", stderr)
	}
}

// What the store says when it cannot be opened or read is English and may
// carry anything; it is put on one line inside a sentence in the user's
// language, in every command that needs the key.
func TestStoreFailuresAreReportedInTheUsersLanguage(t *testing.T) {
	useFileStore(t)
	useTerminalStdout(t, false)
	useFakeBrowser(t, nil)
	saved := openStore
	t.Cleanup(func() { openStore = saved })
	tr := map[string]string{"LANG": "tr_TR.UTF-8"}

	openStore = func(string, func(string) string) (credential.Store, error) {
		return nil, errors.New("no config\x1b[2J dir\nulgen: all is well")
	}
	for _, args := range [][]string{{"status"}, {"token"}, {"logout"}} {
		code, stdout, stderr := exec(t, context.Background(), nil, args...)
		if code != exitError || stdout != "" || stderr != "ulgen: the saved key could not be read: no config dir ulgen: all is well\n" {
			t.Errorf("%v: code %d stdout %q stderr %q", args, code, stdout, stderr)
		}
		if _, _, stderr := exec(t, context.Background(), tr, args...); stderr != "ulgen: kayıtlı anahtar okunamadı: no config dir ulgen: all is well\n" {
			t.Errorf("%v in Turkish: %q", args, stderr)
		}
	}

	// The store opens, and reading from it fails.
	openStore = func(string, func(string) string) (credential.Store, error) {
		return brokenStore{}, nil
	}
	for _, args := range [][]string{{"status"}, {"token"}} {
		code, _, stderr := exec(t, context.Background(), tr, args...)
		if code != exitError || stderr != "ulgen: kayıtlı anahtar okunamadı: the keychain is locked\n" {
			t.Errorf("%v: code %d stderr %q", args, code, stderr)
		}
	}
}

// brokenStore cannot be read.
type brokenStore struct{ credential.Store }

func (brokenStore) Get(string) (string, error) { return "", errors.New("the keychain is locked") }

// writeFailStore opens, and every write to it fails.
type writeFailStore struct{ credential.Store }

func (writeFailStore) Get(string) (string, error) { return "", credential.ErrNotFound }
func (writeFailStore) Set(string, string) error   { return errors.New("the keychain is locked") }
func (writeFailStore) Delete(string) error        { return nil }

// A store that cannot be opened for saving is reported as a failure to
// write, not as a failure to read the key that was never read.
func TestLoginReportsAStoreThatCannotBeOpenedForSaving(t *testing.T) {
	useFileStore(t)
	useFakeBrowser(t, nil)
	srv, _ := evrenServer(t, healthy())
	saved := openStore
	t.Cleanup(func() { openStore = saved })

	openStore = func(string, func(string) string) (credential.Store, error) {
		return nil, errors.New("no config dir")
	}
	code, stdout, stderr := execIn(t, context.Background(), nil, testKey+"\n", "login", "--no-browser", "--base-url", srv.URL)
	if code != exitError || !strings.Contains(stderr, "the saved key could not be written: no config dir") {
		t.Errorf("open: code %d stdout %q stderr %q", code, stdout, stderr)
	}
	openStore = func(string, func(string) string) (credential.Store, error) {
		return writeFailStore{}, nil
	}
	code, stdout, stderr = execIn(t, context.Background(), nil, testKey+"\n", "login", "--no-browser", "--base-url", srv.URL)
	if code != exitError || !strings.Contains(stderr, "the saved key could not be written: the keychain is locked") {
		t.Errorf("set: code %d stdout %q stderr %q", code, stdout, stderr)
	}
}
