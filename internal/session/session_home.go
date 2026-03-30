package session

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode"
)

const (
	managedSessionsDirName = "sessions"
	sessionContextDirName  = ".agent-deck"
	sessionContextFileName = "SESSION_CONTEXT.md"
	sessionReposDirName    = "repos"
)

// DefaultManagedSessionHome returns the managed session-home directory for a session.
// Homes are stored under ~/.agent-deck/sessions/<slug>-<id8>/ so they stay out of the
// repo tree while remaining predictable and easy to inspect.
func DefaultManagedSessionHome(title, sessionID string) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve user home: %w", err)
	}

	slug := sanitizeSessionHomeName(title)
	suffix := strings.TrimSpace(sessionID)
	if len(suffix) > 8 {
		suffix = suffix[:8]
	}
	if suffix == "" {
		suffix = "session"
	}

	return filepath.Join(home, ".agent-deck", managedSessionsDirName, slug+"-"+suffix), nil
}

func sanitizeSessionHomeName(title string) string {
	title = strings.TrimSpace(strings.ToLower(title))
	if title == "" {
		return "session"
	}

	var b strings.Builder
	lastDash := false
	for _, r := range title {
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			b.WriteRune(r)
			lastDash = false
		default:
			if !lastDash {
				b.WriteByte('-')
				lastDash = true
			}
		}
	}

	slug := strings.Trim(b.String(), "-")
	if slug == "" {
		slug = "session"
	}
	if len(slug) > 48 {
		slug = strings.Trim(slug[:48], "-")
		if slug == "" {
			slug = "session"
		}
	}
	return slug
}

// EnsureManagedSessionHome assigns a managed session home if one is not already set.
func (inst *Instance) EnsureManagedSessionHome() error {
	if inst == nil || strings.TrimSpace(inst.SessionHome) != "" {
		return nil
	}

	sessionHome, err := DefaultManagedSessionHome(inst.Title, inst.ID)
	if err != nil {
		return err
	}
	inst.SessionHome = sessionHome

	if inst.tmuxSession != nil && (!inst.MultiRepoEnabled || inst.MultiRepoTempDir == "") {
		inst.tmuxSession.WorkDir = inst.SessionHome
	}
	return nil
}

// EffectiveMetadataRoot returns the directory where session-local metadata should live.
// SessionHome wins when configured so attachments and generated files stay per-session.
func (inst *Instance) EffectiveMetadataRoot() string {
	if inst == nil {
		return ""
	}
	if strings.TrimSpace(inst.SessionHome) != "" {
		return filepath.Clean(inst.SessionHome)
	}
	return inst.ProjectPath
}

// ProviderProjectPath returns the directory that provider-native history is scoped to.
// This follows the actual launch cwd, not just the logical repo path.
func (inst *Instance) ProviderProjectPath() string {
	if inst == nil {
		return ""
	}
	return inst.EffectiveWorkingDir()
}

func (inst *Instance) SessionContextDir() string {
	root := inst.EffectiveMetadataRoot()
	if root == "" {
		return ""
	}
	return filepath.Join(root, sessionContextDirName)
}

func (inst *Instance) SessionContextPath() string {
	dir := inst.SessionContextDir()
	if dir == "" {
		return ""
	}
	return filepath.Join(dir, sessionContextFileName)
}

func (inst *Instance) SessionReposPath() string {
	if inst == nil || strings.TrimSpace(inst.SessionHome) == "" {
		return ""
	}
	return filepath.Join(inst.SessionHome, sessionReposDirName)
}

func (inst *Instance) ManagedInstructionsFileName() string {
	if inst == nil {
		return ""
	}
	switch {
	case IsClaudeCompatible(inst.Tool):
		return "CLAUDE.md"
	case inst.Tool == "codex":
		return "AGENTS.md"
	default:
		return ""
	}
}

func (inst *Instance) ManagedInstructionsPath() string {
	if inst == nil || strings.TrimSpace(inst.SessionHome) == "" {
		return ""
	}
	name := inst.ManagedInstructionsFileName()
	if name == "" {
		return ""
	}
	return filepath.Join(inst.SessionHome, name)
}

func (inst *Instance) staleManagedInstructionsPath() string {
	if inst == nil || strings.TrimSpace(inst.SessionHome) == "" {
		return ""
	}
	switch inst.ManagedInstructionsFileName() {
	case "CLAUDE.md":
		return filepath.Join(inst.SessionHome, "AGENTS.md")
	case "AGENTS.md":
		return filepath.Join(inst.SessionHome, "CLAUDE.md")
	default:
		return ""
	}
}

// EnsureSessionHomeLayout creates the basic directory layout for managed homes.
func (inst *Instance) EnsureSessionHomeLayout() error {
	if inst == nil || strings.TrimSpace(inst.SessionHome) == "" {
		return nil
	}

	for _, dir := range []string{
		inst.SessionHome,
		inst.SessionContextDir(),
		inst.SessionReposPath(),
	} {
		if dir == "" {
			continue
		}
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("create session home layout: %w", err)
		}
	}
	return nil
}
