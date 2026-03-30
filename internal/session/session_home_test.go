package session

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEnsureManagedSessionHome(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)

	inst := NewInstanceWithTool("Fix Auth Flow", "/tmp/project", "claude")
	if err := inst.EnsureManagedSessionHome(); err != nil {
		t.Fatalf("EnsureManagedSessionHome() error = %v", err)
	}

	if inst.SessionHome == "" {
		t.Fatal("SessionHome should be assigned")
	}
	if !strings.HasPrefix(inst.SessionHome, filepath.Join(tmpHome, ".agent-deck", "sessions")) {
		t.Fatalf("SessionHome = %q, want prefix %q", inst.SessionHome, filepath.Join(tmpHome, ".agent-deck", "sessions"))
	}
	if inst.GetTmuxSession() == nil {
		t.Fatal("tmux session should exist")
	}
	if got := inst.GetTmuxSession().WorkDir; got != inst.SessionHome {
		t.Fatalf("tmux workdir = %q, want %q", got, inst.SessionHome)
	}
}

func TestRefreshManagedSessionFiles(t *testing.T) {
	tmpDir := t.TempDir()
	projectPath := filepath.Join(tmpDir, "project")
	sessionHome := filepath.Join(tmpDir, "session-home")
	docPath := filepath.Join(tmpDir, "knowledge", "auth.md")

	if err := os.MkdirAll(projectPath, 0o755); err != nil {
		t.Fatalf("mkdir project: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(docPath), 0o755); err != nil {
		t.Fatalf("mkdir knowledge: %v", err)
	}
	if err := os.WriteFile(docPath, []byte("# Auth\n"), 0o644); err != nil {
		t.Fatalf("write knowledge doc: %v", err)
	}

	inst := NewInstanceWithTool("Auth Session", projectPath, "claude")
	inst.SessionHome = sessionHome
	if inst.GetTmuxSession() != nil {
		inst.GetTmuxSession().WorkDir = sessionHome
	}

	catalog := &KnowledgeCatalog{
		Docs: map[string]KnowledgeDoc{
			"kb.auth": {
				ID:          "kb.auth",
				Name:        "Auth Overview",
				Category:    "backend",
				Kind:        "guide",
				FilePath:    docPath,
				Description: "Primary auth notes",
			},
		},
		Bundles: map[string]KnowledgeBundle{},
	}
	if _, err := AttachKnowledgeToProject(inst.EffectiveMetadataRoot(), catalog, "kb.auth", KnowledgeAttachmentDoc); err != nil {
		t.Fatalf("AttachKnowledgeToProject() error = %v", err)
	}

	if err := RefreshManagedSessionFiles(inst, catalog); err != nil {
		t.Fatalf("RefreshManagedSessionFiles() error = %v", err)
	}

	contextPath := inst.SessionContextPath()
	contextData, err := os.ReadFile(contextPath)
	if err != nil {
		t.Fatalf("read session context: %v", err)
	}
	contextText := string(contextData)
	if !strings.Contains(contextText, projectPath) {
		t.Fatalf("session context should include project path %q, got:\n%s", projectPath, contextText)
	}
	if !strings.Contains(contextText, docPath) {
		t.Fatalf("session context should include KB doc path %q, got:\n%s", docPath, contextText)
	}

	instructionsPath := inst.ManagedInstructionsPath()
	instructionsData, err := os.ReadFile(instructionsPath)
	if err != nil {
		t.Fatalf("read instructions: %v", err)
	}
	instructionsText := string(instructionsData)
	if !strings.Contains(instructionsText, "./.agent-deck/SESSION_CONTEXT.md") {
		t.Fatalf("instructions should reference session context file, got:\n%s", instructionsText)
	}
	if !strings.Contains(instructionsText, docPath) {
		t.Fatalf("instructions should include KB doc path %q, got:\n%s", docPath, instructionsText)
	}

	if _, err := os.Stat(filepath.Join(sessionHome, "repos")); err != nil {
		t.Fatalf("expected repos dir in session home: %v", err)
	}
}

func TestBuildClaudeExtraFlagsAddsProjectPathForSessionHome(t *testing.T) {
	inst := NewInstanceWithTool("Claude Home", "/tmp/project", "claude")
	inst.SessionHome = "/tmp/agent-deck/sessions/claude-home-12345678"
	if inst.GetTmuxSession() != nil {
		inst.GetTmuxSession().WorkDir = inst.SessionHome
	}

	flags := inst.buildClaudeExtraFlags(&ClaudeOptions{})
	if !strings.Contains(flags, "--add-dir /tmp/project") {
		t.Fatalf("expected project path add-dir in flags, got %q", flags)
	}
}
