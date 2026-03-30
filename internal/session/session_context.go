package session

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// RefreshManagedSessionFiles regenerates the lightweight session-local files that
// teach the agent where it is, which repos are relevant, and which KB docs are attached.
func RefreshManagedSessionFiles(inst *Instance, catalog *KnowledgeCatalog) error {
	if inst == nil || strings.TrimSpace(inst.SessionHome) == "" {
		return nil
	}
	if err := inst.EnsureSessionHomeLayout(); err != nil {
		return err
	}

	if catalog == nil {
		if loaded, err := LoadKnowledgeCatalog(""); err == nil {
			catalog = loaded
		}
	}

	attachments, err := GetAttachedProjectKnowledge(inst.EffectiveMetadataRoot())
	if err != nil {
		return fmt.Errorf("load attached knowledge: %w", err)
	}
	docs := ExpandKnowledgeAttachments(catalog, attachments)

	if err := writeSessionManagedFile(inst.SessionContextPath(), renderSessionContextMarkdown(inst, attachments, docs), 0o644); err != nil {
		return err
	}

	instructionsPath := inst.ManagedInstructionsPath()
	if instructionsPath != "" {
		if err := writeSessionManagedFile(instructionsPath, renderManagedInstructionsMarkdown(inst, attachments, docs), 0o644); err != nil {
			return err
		}
		if stale := inst.staleManagedInstructionsPath(); stale != "" {
			_ = os.Remove(stale)
		}
	}

	return nil
}

func renderSessionContextMarkdown(inst *Instance, attachments []ProjectKnowledgeAttachment, docs []KnowledgeDoc) string {
	var b strings.Builder

	b.WriteString("# Agent Deck Session Context\n\n")
	b.WriteString("This directory is the managed home for an Agent Deck session.\n")
	b.WriteString("It is the default launch cwd for the runtime, even when the primary repo lives elsewhere.\n\n")

	b.WriteString("## Session\n\n")
	b.WriteString(fmt.Sprintf("- Title: `%s`\n", inst.Title))
	b.WriteString(fmt.Sprintf("- Session ID: `%s`\n", inst.ID))
	b.WriteString(fmt.Sprintf("- Session home: `%s`\n", inst.SessionHome))
	b.WriteString(fmt.Sprintf("- Tool: `%s`\n", inst.Tool))
	b.WriteString(fmt.Sprintf("- Primary project path: `%s`\n", inst.ProjectPath))
	if inst.WorktreePath != "" {
		b.WriteString(fmt.Sprintf("- Active worktree: `%s`\n", inst.WorktreePath))
	}
	if len(inst.AdditionalPaths) > 0 {
		b.WriteString("\n## Additional Project Paths\n\n")
		for _, path := range inst.AdditionalPaths {
			b.WriteString(fmt.Sprintf("- `%s`\n", path))
		}
	}

	reposPath := inst.SessionReposPath()
	if reposPath != "" {
		b.WriteString("\n## Working Conventions\n\n")
		b.WriteString(fmt.Sprintf("- Start from this session home by default: `%s`\n", inst.SessionHome))
		b.WriteString(fmt.Sprintf("- Put new clones or scratch repos under: `%s`\n", reposPath))
		b.WriteString("- For edits in an existing repo, `cd` into the relevant project path before making changes.\n")
	}

	b.WriteString("\n## Attached Knowledge\n\n")
	if len(attachments) == 0 {
		b.WriteString("No knowledge docs are attached to this session.\n")
		return b.String()
	}

	docByID := make(map[string]KnowledgeDoc, len(docs))
	for _, doc := range docs {
		docByID[doc.ID] = doc
	}

	for _, attachment := range attachments {
		label := attachment.ID
		if attachment.Name != "" {
			label = attachment.Name
		}
		b.WriteString(fmt.Sprintf("- `%s:%s`", attachment.Kind, attachment.ID))
		if label != "" && label != attachment.ID {
			b.WriteString(fmt.Sprintf(" - %s", label))
		}
		if doc, ok := docByID[attachment.ID]; ok {
			b.WriteString(fmt.Sprintf("\n  - Path: `%s`", doc.FilePath))
			if doc.Description != "" {
				b.WriteString(fmt.Sprintf("\n  - Summary: %s", doc.Description))
			}
			if doc.Category != "" {
				b.WriteString(fmt.Sprintf("\n  - Category: `%s`", doc.Category))
			}
		}
		b.WriteString("\n")
	}

	return b.String()
}

func renderManagedInstructionsMarkdown(inst *Instance, attachments []ProjectKnowledgeAttachment, docs []KnowledgeDoc) string {
	var b strings.Builder

	b.WriteString("# Agent Deck Managed Session\n\n")
	b.WriteString("This session launches from a managed Agent Deck home rather than a repo root.\n")
	b.WriteString(fmt.Sprintf("Read `./%s/%s` before taking action.\n\n", sessionContextDirName, sessionContextFileName))

	b.WriteString("## Startup Rules\n\n")
	b.WriteString(fmt.Sprintf("1. Treat `%s` as the session home and default cwd.\n", inst.SessionHome))
	b.WriteString(fmt.Sprintf("2. Use `%s` as the primary existing project path.\n", inst.ProjectPath))
	if reposPath := inst.SessionReposPath(); reposPath != "" {
		b.WriteString(fmt.Sprintf("3. Put new clones or scratch repos under `%s` unless told otherwise.\n", reposPath))
	} else {
		b.WriteString("3. For new clones, create them under this session home unless told otherwise.\n")
	}
	b.WriteString("4. Attached knowledge docs are references, not mandatory reading. Open only the relevant files.\n")

	if len(docs) > 0 {
		b.WriteString("\n## Attached Knowledge Paths\n\n")
		for _, doc := range docs {
			b.WriteString(fmt.Sprintf("- `%s` -> `%s`\n", doc.ID, doc.FilePath))
		}
	}

	if len(attachments) > 0 && len(docs) == 0 {
		b.WriteString("\n## Attached Knowledge\n\n")
		for _, attachment := range attachments {
			b.WriteString(fmt.Sprintf("- `%s:%s`\n", attachment.Kind, attachment.ID))
		}
	}

	return b.String()
}

func writeSessionManagedFile(path, content string, mode os.FileMode) error {
	if path == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create managed session file dir: %w", err)
	}

	var buf bytes.Buffer
	buf.WriteString(content)
	if !strings.HasSuffix(content, "\n") {
		buf.WriteByte('\n')
	}

	tmpPath := path + ".tmp"
	if err := os.WriteFile(tmpPath, buf.Bytes(), mode); err != nil {
		return fmt.Errorf("write managed session file: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("save managed session file: %w", err)
	}
	return nil
}
