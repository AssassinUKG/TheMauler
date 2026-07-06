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
		t.Fatalf("default skill view should not load full external body:\n%s", out)
	}
	if strings.Contains(out, filepath.ToSlash(sourceDir)) {
		t.Fatalf("default skill view should not expose absolute external source path:\n%s", out)
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
		t.Fatalf("focused skill view should not expose absolute external source path:\n%s", out)
	}
}

func TestSkillViewExternalSourcePrefersHTBMethodologyForBoxQueries(t *testing.T) {
	cfgDir := t.TempDir()
	t.Setenv("MAULER_CONFIG_DIR", cfgDir)
	sourceDir := t.TempDir()
	mustWriteToolTest(t, filepath.Join(sourceDir, "maps", "htb_methodology.md"), `# HTB Methodology

## Foothold Enumeration

Enumerate services, web paths, credentials, and shell routes for the current box before using spoilers.
`)
	mustWriteToolTest(t, filepath.Join(sourceDir, "prototyping", "func_encyclopedia", "c_evasion_patterns.md"), `# Prototype Notes

## Foothold Enumeration

Foothold enumeration words appear here too, but this is not the primary HTB methodology map.
`)
	saveToolSkill(t, cfgDir, sourceDir)

	out, err := (&SkillView{}).Run(context.Background(), []byte(`{"name":"master","query":"htb foothold enumeration","max_bytes":12000}`))
	if err != nil {
		t.Fatal(err)
	}
	htbIdx := strings.Index(out, "--- Source: maps/htb_methodology.md ---")
	protoIdx := strings.Index(out, "--- Source: prototyping/func_encyclopedia/c_evasion_patterns.md ---")
	if htbIdx < 0 {
		t.Fatalf("expected HTB methodology source, got:\n%s", out)
	}
	if protoIdx >= 0 && protoIdx < htbIdx {
		t.Fatalf("HTB methodology should rank before prototyping/evasion notes, got:\n%s", out)
	}
}

func TestSkillViewExternalSourceScoresHeadingMatches(t *testing.T) {
	cfgDir := t.TempDir()
	t.Setenv("MAULER_CONFIG_DIR", cfgDir)
	sourceDir := t.TempDir()
	mustWriteToolTest(t, filepath.Join(sourceDir, "maps", "web_application.md"), `# Web Application

## Directory Enumeration

Run focused content discovery and preserve interesting paths as evidence.

## Reporting

Write only final findings.
`)
	saveToolSkill(t, cfgDir, sourceDir)

	out, err := (&SkillView{}).Run(context.Background(), []byte(`{"name":"master","query":"directory enumeration"}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "## Directory Enumeration") || !strings.Contains(out, "focused content discovery") {
		t.Fatalf("expected directory enumeration excerpt, got:\n%s", out)
	}
	if strings.Index(out, "## Reporting") >= 0 && strings.Index(out, "## Reporting") < strings.Index(out, "## Directory Enumeration") {
		t.Fatalf("heading match should rank before unrelated reporting section, got:\n%s", out)
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
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o640); err != nil {
		t.Fatal(err)
	}
}
