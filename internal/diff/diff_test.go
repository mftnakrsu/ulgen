package diff

import (
	"fmt"
	"strings"
	"testing"
)

func TestUnifiedSimpleChange(t *testing.T) {
	old := "a\nb\nc\nd\ne\nf\ng\n"
	new := "a\nb\nc\nD\ne\nf\ng\n"
	got := Unified("a/x.txt", "b/x.txt", old, new)
	want := "--- a/x.txt\n+++ b/x.txt\n@@ -1,7 +1,7 @@\n a\n b\n c\n-d\n+D\n e\n f\n g\n"
	if got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestUnifiedEqualTextsAreEmpty(t *testing.T) {
	if got := Unified("a", "b", "same\ntext\n", "same\ntext\n"); got != "" {
		t.Errorf("got %q, want empty", got)
	}
	if got := Unified("a", "b", "crlf\r\nfile\r\n", "crlf\nfile\n"); got != "" {
		t.Errorf("CRLF vs LF should be equal, got %q", got)
	}
}

func TestUnifiedNewFile(t *testing.T) {
	got := Unified("/dev/null", "b/new.txt", "", "hello\nworld\n")
	want := "--- /dev/null\n+++ b/new.txt\n@@ -0,0 +1,2 @@\n+hello\n+world\n"
	if got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestUnifiedDeleteEverything(t *testing.T) {
	got := Unified("a/x", "b/x", "one\ntwo\n", "")
	want := "--- a/x\n+++ b/x\n@@ -1,2 +0,0 @@\n-one\n-two\n"
	if got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestUnifiedTwoHunks(t *testing.T) {
	var a, b []string
	for i := 1; i <= 30; i++ {
		a = append(a, fmt.Sprintf("line %d", i))
		b = append(b, fmt.Sprintf("line %d", i))
	}
	b[2] = "changed 3"
	b[27] = "changed 28"
	got := Unified("a", "b", strings.Join(a, "\n")+"\n", strings.Join(b, "\n")+"\n")
	if strings.Count(got, "@@ ") != 2 {
		t.Errorf("want two hunks, got:\n%s", got)
	}
	if !strings.Contains(got, "@@ -1,6 +1,6 @@") || !strings.Contains(got, "@@ -25,6 +25,6 @@") {
		t.Errorf("hunk headers:\n%s", got)
	}
	if !strings.Contains(got, "-line 3\n+changed 3\n") || !strings.Contains(got, "-line 28\n+changed 28\n") {
		t.Errorf("hunk bodies:\n%s", got)
	}
}

func TestUnifiedInsertionInMiddle(t *testing.T) {
	got := Unified("a", "b", "x\ny\n", "x\nnew\ny\n")
	want := "--- a\n+++ b\n@@ -1,2 +1,3 @@\n x\n+new\n y\n"
	if got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestStat(t *testing.T) {
	added, removed := Stat("a\nb\nc\n", "a\nB\nc\nd\n")
	if added != 2 || removed != 1 {
		t.Errorf("Stat = +%d -%d, want +2 -1", added, removed)
	}
}

func TestLargeDifferentFilesFallBackToBlock(t *testing.T) {
	var a, b []string
	for i := 0; i < 3000; i++ {
		a = append(a, fmt.Sprintf("old %d", i))
		b = append(b, fmt.Sprintf("new %d", i))
	}
	got := Unified("a", "b", strings.Join(a, "\n"), strings.Join(b, "\n"))
	if !strings.Contains(got, "@@ -1,3000 +1,3000 @@") || strings.Count(got, "\n-old ") != 3000 || strings.Count(got, "\n+new ") != 3000 {
		t.Errorf("block fallback header or counts wrong:\n%s", got[:200])
	}
}

func TestNoTrailingNewlineIsTolerated(t *testing.T) {
	got := Unified("a", "b", "x\ny", "x\nz")
	if !strings.Contains(got, "-y\n+z\n") {
		t.Errorf("got:\n%s", got)
	}
}
