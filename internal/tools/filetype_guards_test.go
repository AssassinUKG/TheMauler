package tools

import "testing"

func TestGuardFileContentShellScript(t *testing.T) {
	// HTML-escaped operators in a .sh file are unescaped (incl. compounded escaping).
	in := "#!/bin/bash\ncurl -sk https://x 2&gt;&amp;amp;1 | head\n"
	want := "#!/bin/bash\ncurl -sk https://x 2>&1 | head\n"
	out, note := guardFileContent("/tmp/test_cols.sh", in)
	if out != want {
		t.Fatalf("shell guard = %q, want %q", out, want)
	}
	if note == "" {
		t.Fatal("expected a guard note")
	}
}

func TestGuardFileContentDetectsShebang(t *testing.T) {
	in := "#!/usr/bin/env bash\necho 1 &gt; /tmp/x\n"
	out, _ := guardFileContent("runme", in) // no .sh extension, classified by shebang
	if out != "#!/usr/bin/env bash\necho 1 > /tmp/x\n" {
		t.Fatalf("shebang-classified guard = %q", out)
	}
}

func TestGuardFileContentLeavesNonShellAlone(t *testing.T) {
	// Markdown / HTML legitimately contains entities — never touch them.
	md := "Use &amp; carefully and escape &lt;tag&gt; in docs."
	out, note := guardFileContent("README.md", md)
	if out != md || note != "" {
		t.Fatalf("non-shell content changed: %q note=%q", out, note)
	}
}

func TestGuardFileContentShellNoEntitiesUnchanged(t *testing.T) {
	in := "#!/bin/bash\necho hello\n"
	out, note := guardFileContent("x.sh", in)
	if out != in || note != "" {
		t.Fatalf("clean shell script changed: %q note=%q", out, note)
	}
}
