package tools

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSkillViewExternalSourceReturnsOutlineByDefault(t *testing.T) {
	cfgDir := t.TempDir()
	t.Setenv("MAULER_CONFIG_DIR", cfgDir)
	sourceDir := t.TempDir()
	mustWriteToolTest(t, filepath.Join(sourceDir, "master_skill.md"), `# Overview

Use this workflow carefully.

## Enumeration

Long enumeration instructions that should not be loaded by default.

## Exploitation

Long exploitation instructions that should not be loaded by default.
`)
	saveToolSkill(t, cfgDir, sourceDir)

	out, err := (&SkillView{}).Run(context.Background(), []byte(`{"name":"master"}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "Large external skill source") || !strings.Contains(out, "## Enumeration") {
		t.Fatalf("expected outline, got:\n%s", out)
	}
	if strings.Contains(out, "Long enumeration instructions") {
		t.Fatalf("default skill_view should not load full external body:\n%s", out)
	}
	if strings.Contains(out, filepath.ToSlash(sourceDir)) {
		t.Fatalf("default skill_view should not expose absolute external source path:\n%s", out)
	}
	if !strings.Contains(out, "--- Source: master_skill.md ---") {
		t.Fatalf("expected relative source label, got:\n%s", out)
	}
}

func TestSkillViewExternalSourceQueryReturnsFocusedSections(t *testing.T) {
	cfgDir := t.TempDir()
	t.Setenv("MAULER_CONFIG_DIR", cfgDir)
	sourceDir := t.TempDir()
	mustWriteToolTest(t, filepath.Join(sourceDir, "master_skill.md"), `# Overview

General workflow.

## Enumeration

Use discovery commands and collect service facts.

## Reporting

Write a concise final report.
`)
	saveToolSkill(t, cfgDir, sourceDir)

	out, err := (&SkillView{}).Run(context.Background(), []byte(`{"name":"master","query":"reporting"}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "## Reporting") || !strings.Contains(out, "concise final report") {
		t.Fatalf("expected focused reporting excerpt, got:\n%s", out)
	}
	if strings.Contains(out, "Use discovery commands") {
		t.Fatalf("query should avoid unrelated sections, got:\n%s", out)
	}
	if strings.Contains(out, filepath.ToSlash(sourceDir)) {
		t.Fatalf("focused skill_view should not expose absolute external source path:\n%s", out)
	}
}

func saveToolSkill(t *testing.T, cfgDir, sourcePath string) {
	t.Helper()
	skillsDir := filepath.Join(cfgDir, "skills")
	if err := os.MkdirAll(skillsDir, 0o750); err != nil {
		t.Fatal(err)
	}
	mustWriteToolTest(t, filepath.Join(skillsDir, "master.md"), `---
name: master
description: Use when the user explicitly asks to apply the selected master workflow/instruction source.
version: 1.0.0
tags: [master, workflow, instructions]
source_path: `+filepath.ToSlash(sourcePath)+`
---

External master skill source.
`)
}

func mustWriteToolTest(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o640); err != nil {
		t.Fatal(err)
	}
}
