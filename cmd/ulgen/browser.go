package main

import (
	osexec "os/exec"
	"runtime"
)

// openBrowser opens a URL in the user's browser. It is a variable so tests
// can replace it. The command is started and not waited for: some openers
// block until the browser exits.
var openBrowser = func(rawURL string) error {
	var cmd *osexec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = osexec.Command("open", rawURL)
	case "windows":
		cmd = osexec.Command("rundll32", "url.dll,FileProtocolHandler", rawURL)
	default:
		cmd = osexec.Command("xdg-open", rawURL)
	}
	detach(cmd)
	if err := cmd.Start(); err != nil {
		return err
	}
	go cmd.Wait()
	return nil
}
