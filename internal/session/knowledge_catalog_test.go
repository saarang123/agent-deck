package session

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadKnowledgeCatalog_ParsesDocsAndBundles(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "config.yaml"), `
categories:
  skills:
    description: Task playbooks
  repos:
    description: Repo notes
bundles:
  onboarding:
    description: Default starter context
    docs:
      - skill.build
    tags:
      - repo
`)
	writeFile(t, filepath.Join(root, "skills", "config.yaml"), `
entries:
  - id: skill.build
    file: build.md
    description: Build workflow
    tags: [build]
`)
	writeFile(t, filepath.Join(root, "skills", "build.md"), `-----
id: skill.build
name: Build
kind: skill
description: Build workflow
related:
  - repo.backend
-----

## Purpose
Build the project.
`)
	writeFile(t, filepath.Join(root, "repos", "config.yaml"), `
entries:
  - id: repo.backend
    file: backend.md
    description: Backend API
    repos: [org/backend]
    tags: [repo]
`)
	writeFile(t, filepath.Join(root, "repos", "backend.md"), `-----
id: repo.backend
name: Backend
kind: repo
description: Backend API service
repos:
  - org/backend
tags:
  - repo
-----

## Overview
FastAPI backend.
`)

	catalog, err := LoadKnowledgeCatalog(root)
	if err != nil {
		t.Fatalf("LoadKnowledgeCatalog failed: %v", err)
	}

	if got := len(catalog.Docs); got != 2 {
		t.Fatalf("expected 2 docs, got %d", got)
	}
	if got := len(catalog.Bundles); got != 1 {
		t.Fatalf("expected 1 bundle, got %d", got)
	}

	doc, err := ResolveKnowledgeDoc(catalog, "skill.build")
	if err != nil {
		t.Fatalf("ResolveKnowledgeDoc failed: %v", err)
	}
	if doc.Name != "Build" {
		t.Fatalf("doc name = %q, want Build", doc.Name)
	}
	if doc.Category != "skills" {
		t.Fatalf("doc category = %q, want skills", doc.Category)
	}

	bundle, err := ResolveKnowledgeBundle(catalog, "onboarding")
	if err != nil {
		t.Fatalf("ResolveKnowledgeBundle failed: %v", err)
	}
	docs := ExpandKnowledgeBundle(catalog, *bundle)
	if len(docs) != 2 {
		t.Fatalf("expanded bundle docs = %d, want 2", len(docs))
	}
}

func TestKnowledgeManifest_AttachDetachAndExpand(t *testing.T) {
	root := t.TempDir()
	project := t.TempDir()

	writeFile(t, filepath.Join(root, "config.yaml"), `
categories:
  skills: {}
bundles:
  starter:
    docs:
      - skill.build
`)
	writeFile(t, filepath.Join(root, "skills", "config.yaml"), `
entries:
  - id: skill.build
    file: build.md
    description: Build workflow
`)
	writeFile(t, filepath.Join(root, "skills", "build.md"), `-----
id: skill.build
name: Build
kind: skill
description: Build workflow
-----

Build it.
`)

	catalog, err := LoadKnowledgeCatalog(root)
	if err != nil {
		t.Fatalf("LoadKnowledgeCatalog failed: %v", err)
	}

	if _, err := AttachKnowledgeToProject(project, catalog, "skill.build", ""); err != nil {
		t.Fatalf("AttachKnowledgeToProject doc failed: %v", err)
	}
	if _, err := AttachKnowledgeToProject(project, catalog, "starter", ""); err != nil {
		t.Fatalf("AttachKnowledgeToProject bundle failed: %v", err)
	}

	manifest, err := LoadProjectKnowledgeManifest(project)
	if err != nil {
		t.Fatalf("LoadProjectKnowledgeManifest failed: %v", err)
	}
	if got := len(manifest.Attachments); got != 2 {
		t.Fatalf("attachments = %d, want 2", got)
	}

	expanded := ExpandKnowledgeAttachments(catalog, manifest.Attachments)
	if got := len(expanded); got != 1 {
		t.Fatalf("expanded docs = %d, want 1", got)
	}

	removed, err := DetachKnowledgeFromProject(project, catalog, "starter", "")
	if err != nil {
		t.Fatalf("DetachKnowledgeFromProject failed: %v", err)
	}
	if removed.Kind != KnowledgeAttachmentBundle {
		t.Fatalf("removed kind = %q, want %q", removed.Kind, KnowledgeAttachmentBundle)
	}

	data, err := os.ReadFile(GetProjectKnowledgeManifestPath(project))
	if err != nil {
		t.Fatalf("read manifest failed: %v", err)
	}
	if string(data) == "" {
		t.Fatal("manifest should not be empty")
	}
	if strings.Contains(string(data), root) {
		t.Fatal("manifest should not embed KB root content")
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll(%s): %v", path, err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile(%s): %v", path, err)
	}
}
