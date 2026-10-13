package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"runtime"
	"strconv"
	"strings"
)

// The tests that start a real process use the test binary itself as the
// tool: TestMain hands over to runHelper when helperVar is set. No installed
// tool is ever started, and the processes involved are only ones a test
// started itself.
const (
	helperVar        = "ULGEN_TEST_HELPER"  // the mode, see runHelper
	helperArgsVar    = "ULGEN_TEST_ARGS"    // mode "exec": the arguments to hand over, as a JSON list
	helperKeyVar     = "ULGEN_TEST_KEY"     // mode "exec": the (invented) key to hand over
	helperOutVar     = "ULGEN_TEST_OUT"     // mode "report": the file to write to instead of standard output
	helperExitVar    = "ULGEN_TEST_EXIT"    // mode "report": the exit code
	helperSelfVar    = "ULGEN_TEST_SELF"    // the path of the test binary, for a batch file to call
	helperEnvVar     = "ULGEN_TEST_ENV"     // mode "report": also record the whole environment
	helperReqBodyVar = "ULGEN_TEST_REQBODY" // mode "proxy-request": the request body to send
)

// helperReport is what the stand-in tool saw of its start.
type helperReport struct {
	Pid  int
	Arg0 string
	Args []string // without the program's own name
	Dir  string
	Keys []string // every entry of the environment that sets the key variable
	Env  []string // the whole environment, only when helperEnvVar is set
	Open []int    // the open file descriptors above standard error (Unix)
}

// runHelper does what the mode says and returns the exit code:
//
//	report         write a helperReport, then exit with the code in helperExitVar
//	version        print a version line, as a tool does for --version
//	exec           start the test binary again in the mode "report" through
//	               platformExec, the way "ulgen run" starts a tool
//	proxy-request  as a tool that speaks the Anthropic API, send the request
//	               body in helperReqBodyVar to ANTHROPIC_BASE_URL with the
//	               token in ANTHROPIC_AUTH_TOKEN, and print the answer
//
// Other modes belong to one system (platformHelper).
func runHelper(mode string) int {
	switch mode {
	case "report":
		return helperWriteReport()
	case "version":
		fmt.Println("stand-in 1.2.3")
		return 0
	case "exec":
		return helperExec()
	case "proxy-request":
		return helperProxyRequest()
	}
	return platformHelper(mode)
}

// helperProxyRequest is the mode "proxy-request": it stands in for Claude
// Code that has been started by "ulgen run claude". It reads its address from
// ANTHROPIC_BASE_URL and its token from ANTHROPIC_AUTH_TOKEN, sends the
// Messages request in helperReqBodyVar with the token, and writes the answer
// to standard output. Its exit code is 0 on a 2xx answer and 1 otherwise.
func helperProxyRequest() int {
	base := os.Getenv("ANTHROPIC_BASE_URL")
	token := os.Getenv("ANTHROPIC_AUTH_TOKEN")
	body := os.Getenv(helperReqBodyVar)
	req, err := http.NewRequest(http.MethodPost, base+"/v1/messages", strings.NewReader(body))
	if err != nil {
		fmt.Fprintln(os.Stderr, "helper:", err)
		return 98
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		fmt.Fprintln(os.Stderr, "helper:", err)
		return 98
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	// The answer goes to the file in helperOutVar when a test set one, so it
	// can be read without mixing with the test's own standard output; else it
	// goes to standard output.
	if out := os.Getenv(helperOutVar); out != "" {
		if err := os.WriteFile(out, b, 0o600); err != nil {
			fmt.Fprintln(os.Stderr, "helper:", err)
			return 98
		}
	} else {
		os.Stdout.Write(b)
	}
	if resp.StatusCode/100 != 2 {
		return 1
	}
	return 0
}

// helperReqForClaude is the body a request in the tests sends: a one-message
// Messages request that asks for the default model.
const helperReqForClaude = `{"model":"deepseek-v4.1-flash","max_tokens":100,"messages":[{"role":"user","content":"hi"}]}`

// helperWriteReport is the mode "report".
func helperWriteReport() int {
	rep := helperReport{Pid: os.Getpid(), Arg0: os.Args[0], Args: os.Args[1:], Open: openDescriptors()}
	rep.Dir, _ = os.Getwd()
	if os.Getenv(helperEnvVar) != "" {
		rep.Env = os.Environ()
	}
	for _, kv := range os.Environ() {
		if name, _, _ := strings.Cut(kv, "="); strings.EqualFold(name, "EVREN_API_KEY") {
			rep.Keys = append(rep.Keys, kv)
		}
	}
	b, err := json.Marshal(rep)
	if err != nil {
		fmt.Fprintln(os.Stderr, "helper:", err)
		return 98
	}
	if out := os.Getenv(helperOutVar); out != "" {
		if err := os.WriteFile(out, b, 0o600); err != nil {
			fmt.Fprintln(os.Stderr, "helper:", err)
			return 98
		}
	} else {
		os.Stdout.Write(b)
	}
	code, _ := strconv.Atoi(os.Getenv(helperExitVar))
	return code
}

// helperExec is the mode "exec": what "ulgen run" does once it has the tool
// and the key. On Unix platformExec does not come back.
func helperExec() int {
	self, err := os.Executable()
	if err != nil {
		fmt.Fprintln(os.Stderr, "helper:", err)
		return 98
	}
	var args []string
	if err := json.Unmarshal([]byte(os.Getenv(helperArgsVar)), &args); err != nil {
		fmt.Fprintln(os.Stderr, "helper:", err)
		return 98
	}
	env := withKey(helperEnv(os.Environ(), "report"), os.Getenv(helperKeyVar), runtime.GOOS == "windows")
	code, err := platformExec(self, append([]string{self}, args...), env)
	if err != nil {
		fmt.Fprintln(os.Stderr, "helper: platformExec:", err)
		return 98
	}
	return code
}

// helperEnv returns env with the helper mode set to mode.
func helperEnv(env []string, mode string) []string {
	out := make([]string, 0, len(env)+1)
	for _, kv := range env {
		if !strings.HasPrefix(kv, helperVar+"=") {
			out = append(out, kv)
		}
	}
	return append(out, helperVar+"="+mode)
}
