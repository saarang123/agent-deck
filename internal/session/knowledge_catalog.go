package session

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
	"gopkg.in/yaml.v3"
)

const (
	projectKnowledgeDirName      = ".agent-deck"
	projectKnowledgeManifestName = "knowledge.toml"
)

const (
	KnowledgeAttachmentDoc    = "doc"
	KnowledgeAttachmentBundle = "bundle"
)

var (
	ErrKnowledgeRootUnset       = errors.New("knowledge root is not configured")
	ErrKnowledgeDocNotFound     = errors.New("knowledge doc not found")
	ErrKnowledgeBundleNotFound  = errors.New("knowledge bundle not found")
	ErrKnowledgeRefAmbiguous    = errors.New("knowledge reference is ambiguous")
	ErrKnowledgeAlreadyAttached = errors.New("knowledge ref already attached")
	ErrKnowledgeNotAttached     = errors.New("knowledge ref is not attached")
	ErrKnowledgeInvalidRoot     = errors.New("knowledge root is invalid")
)

type KnowledgeDoc struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Category    string   `json:"category,omitempty"`
	Kind        string   `json:"kind"`
	FilePath    string   `json:"file_path"`
	Repos       []string `json:"repos,omitempty"`
	Paths       []string `json:"paths,omitempty"`
	Status      string   `json:"status,omitempty"`
	Version     int      `json:"version,omitempty"`
	Description string   `json:"description,omitempty"`
	Related     []string `json:"related,omitempty"`
	Tags        []string `json:"tags,omitempty"`
}

type KnowledgeBundle struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Description string   `json:"description,omitempty"`
	DocIDs      []string `json:"doc_ids,omitempty"`
	Kinds       []string `json:"kinds,omitempty"`
	Tags        []string `json:"tags,omitempty"`
}

type KnowledgeCatalog struct {
	Root       string                     `json:"root"`
	Docs       map[string]KnowledgeDoc    `json:"docs"`
	Bundles    map[string]KnowledgeBundle `json:"bundles"`
	Categories []string                   `json:"categories,omitempty"`
}

type ProjectKnowledgeAttachment struct {
	Kind       string `toml:"kind" json:"kind"`
	ID         string `toml:"id" json:"id"`
	Name       string `toml:"name,omitempty" json:"name,omitempty"`
	AttachedAt string `toml:"attached_at,omitempty" json:"attached_at,omitempty"`
}

type ProjectKnowledgeManifest struct {
	Attachments []ProjectKnowledgeAttachment `toml:"attachment" json:"attachments"`
}

type knowledgeCategoryConfig struct {
	Entries []map[string]any `yaml:"entries"`
}

type knowledgeDocRef struct {
	Kind string
	Doc  *KnowledgeDoc
}

func ResolveKnowledgeRoot(rootOverride string) (string, error) {
	root := strings.TrimSpace(rootOverride)
	if root == "" {
		root = strings.TrimSpace(os.Getenv("AGENTDECK_KNOWLEDGE_ROOT"))
	}
	if root == "" {
		if cfg, err := LoadUserConfig(); err == nil && cfg != nil {
			root = strings.TrimSpace(cfg.Knowledge.Root)
		}
	}
	if root == "" {
		return "", ErrKnowledgeRootUnset
	}
	root = expandSkillPath(root)
	if !filepath.IsAbs(root) {
		abs, err := filepath.Abs(root)
		if err != nil {
			return "", fmt.Errorf("resolve knowledge root: %w", err)
		}
		root = abs
	}
	info, err := os.Stat(root)
	if err != nil {
		if os.IsNotExist(err) {
			return "", fmt.Errorf("%w: %s", ErrKnowledgeInvalidRoot, root)
		}
		return "", err
	}
	if !info.IsDir() {
		return "", fmt.Errorf("%w: %s", ErrKnowledgeInvalidRoot, root)
	}
	return filepath.Clean(root), nil
}

func LoadKnowledgeCatalog(rootOverride string) (*KnowledgeCatalog, error) {
	root, err := ResolveKnowledgeRoot(rootOverride)
	if err != nil {
		return nil, err
	}

	rootConfigPath := filepath.Join(root, "config.yaml")
	data, err := os.ReadFile(rootConfigPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("knowledge root missing config.yaml: %s", root)
		}
		return nil, err
	}

	var raw map[string]any
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("parse knowledge config: %w", err)
	}

	catalog := &KnowledgeCatalog{
		Root:    root,
		Docs:    map[string]KnowledgeDoc{},
		Bundles: map[string]KnowledgeBundle{},
	}

	categoryDefs := parseKnowledgeCategories(raw["categories"])
	for _, cat := range categoryDefs {
		if cat.ID == "" {
			continue
		}
		catalog.Categories = append(catalog.Categories, cat.ID)

		cfgPath := cat.ConfigPath
		if cfgPath == "" {
			cfgPath = filepath.Join(root, cat.ID, "config.yaml")
		} else if !filepath.IsAbs(cfgPath) {
			cfgPath = filepath.Join(root, cfgPath)
		}
		docs, err := loadKnowledgeCategory(root, cat.ID, cfgPath)
		if err != nil {
			return nil, err
		}
		for _, doc := range docs {
			catalog.Docs[doc.ID] = doc
		}
	}
	sort.Strings(catalog.Categories)

	for _, bundle := range parseKnowledgeBundles(raw["bundles"]) {
		if bundle.ID == "" {
			continue
		}
		catalog.Bundles[bundle.ID] = bundle
	}

	return catalog, nil
}

type knowledgeCategoryDef struct {
	ID         string
	ConfigPath string
}

func parseKnowledgeCategories(raw any) []knowledgeCategoryDef {
	switch typed := raw.(type) {
	case map[string]any:
		items := make([]knowledgeCategoryDef, 0, len(typed))
		for id, val := range typed {
			def := knowledgeCategoryDef{ID: strings.TrimSpace(id)}
			if info, ok := val.(map[string]any); ok {
				if path := strings.TrimSpace(toString(info["path"])); path != "" {
					def.ConfigPath = path
				}
			}
			items = append(items, def)
		}
		sort.Slice(items, func(i, j int) bool { return items[i].ID < items[j].ID })
		return items
	case []any:
		items := make([]knowledgeCategoryDef, 0, len(typed))
		for _, val := range typed {
			info, ok := val.(map[string]any)
			if !ok {
				continue
			}
			id := strings.TrimSpace(toString(info["id"]))
			if id == "" {
				continue
			}
			items = append(items, knowledgeCategoryDef{
				ID:         id,
				ConfigPath: strings.TrimSpace(toString(info["path"])),
			})
		}
		sort.Slice(items, func(i, j int) bool { return items[i].ID < items[j].ID })
		return items
	default:
		return nil
	}
}

func loadKnowledgeCategory(root, categoryID, configPath string) ([]KnowledgeDoc, error) {
	data, err := os.ReadFile(configPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read knowledge category %s: %w", categoryID, err)
	}

	var cfg knowledgeCategoryConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse knowledge category %s: %w", categoryID, err)
	}

	docs := make([]KnowledgeDoc, 0, len(cfg.Entries))
	for _, entry := range cfg.Entries {
		docFile := filepath.Join(root, categoryID, strings.TrimSpace(toString(entry["file"])))
		doc, ok := parseKnowledgeDoc(categoryID, docFile, entry)
		if ok {
			docs = append(docs, doc)
		}
	}

	sort.Slice(docs, func(i, j int) bool { return docs[i].ID < docs[j].ID })
	return docs, nil
}

func parseKnowledgeDoc(categoryID, filePath string, entry map[string]any) (KnowledgeDoc, bool) {
	metadata := copyMap(entry)
	if st, err := os.Stat(filePath); err == nil && !st.IsDir() {
		if fileMeta, ok := parseKnowledgeMetadata(filePath); ok {
			for k, v := range fileMeta {
				metadata[k] = v
			}
		}
	}

	docID := strings.TrimSpace(toString(metadata["id"]))
	if docID == "" {
		return KnowledgeDoc{}, false
	}

	name := strings.TrimSpace(toString(metadata["name"]))
	if name == "" {
		name = docID
	}

	doc := KnowledgeDoc{
		ID:          docID,
		Name:        name,
		Category:    strings.TrimSpace(categoryID),
		Kind:        strings.TrimSpace(toString(metadata["kind"])),
		FilePath:    filepath.Clean(filePath),
		Repos:       uniqSortedStrings(toStringSlice(metadata["repos"])),
		Paths:       uniqSortedStrings(toStringSlice(metadata["paths"])),
		Status:      defaultString(strings.TrimSpace(toString(metadata["status"])), "active"),
		Version:     maxInt(toInt(metadata["version"]), 1),
		Description: strings.TrimSpace(toString(metadata["description"])),
		Related:     uniqSortedStrings(toStringSlice(metadata["related"])),
		Tags:        uniqSortedStrings(toStringSlice(metadata["tags"])),
	}
	if doc.Kind == "" {
		doc.Kind = inferKnowledgeKind(doc.ID)
	}
	return doc, true
}

func parseKnowledgeMetadata(filePath string) (map[string]any, bool) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, false
	}
	text := string(data)
	parts := strings.SplitN(text, "-----", 4)
	if len(parts) < 3 {
		return nil, false
	}
	var raw map[string]any
	if err := yaml.Unmarshal([]byte(parts[1]), &raw); err != nil {
		return nil, false
	}
	return raw, true
}

func ReadKnowledgeDocContent(doc KnowledgeDoc) (string, error) {
	data, err := os.ReadFile(doc.FilePath)
	if err != nil {
		return "", err
	}
	text := string(data)
	parts := strings.SplitN(text, "-----", 4)
	if len(parts) >= 3 {
		return strings.TrimSpace(parts[2]), nil
	}
	return strings.TrimSpace(text), nil
}

func SearchKnowledgeDocs(catalog *KnowledgeCatalog, query string, kind string) []KnowledgeDoc {
	query = strings.TrimSpace(strings.ToLower(query))
	if catalog == nil || query == "" {
		return nil
	}
	words := strings.Fields(query)
	type scoredDoc struct {
		score float64
		doc   KnowledgeDoc
	}
	scored := make([]scoredDoc, 0)
	for _, doc := range catalog.Docs {
		if kind != "" && !strings.EqualFold(doc.Kind, kind) {
			continue
		}
		if !strings.EqualFold(doc.Status, "active") {
			continue
		}
		var score float64
		haystack := strings.ToLower(strings.Join([]string{
			doc.ID,
			doc.Name,
			doc.Description,
			strings.Join(doc.Repos, " "),
			strings.Join(doc.Tags, " "),
			strings.Join(doc.Paths, " "),
		}, " "))
		for _, word := range words {
			switch {
			case strings.EqualFold(doc.ID, word):
				score += 6
			case strings.Contains(strings.ToLower(doc.Name), word):
				score += 3
			case strings.Contains(haystack, word):
				score += 1
			}
		}
		if score > 0 {
			scored = append(scored, scoredDoc{score: score, doc: doc})
		}
	}
	sort.Slice(scored, func(i, j int) bool {
		if scored[i].score == scored[j].score {
			return scored[i].doc.ID < scored[j].doc.ID
		}
		return scored[i].score > scored[j].score
	})
	out := make([]KnowledgeDoc, 0, len(scored))
	for _, item := range scored {
		out = append(out, item.doc)
	}
	return out
}

func ListKnowledgeDocs(catalog *KnowledgeCatalog, kind string) []KnowledgeDoc {
	if catalog == nil {
		return nil
	}
	docs := make([]KnowledgeDoc, 0, len(catalog.Docs))
	for _, doc := range catalog.Docs {
		if kind != "" && !strings.EqualFold(doc.Kind, kind) {
			continue
		}
		docs = append(docs, doc)
	}
	sort.Slice(docs, func(i, j int) bool { return docs[i].ID < docs[j].ID })
	return docs
}

func ListKnowledgeBundles(catalog *KnowledgeCatalog) []KnowledgeBundle {
	if catalog == nil {
		return nil
	}
	bundles := make([]KnowledgeBundle, 0, len(catalog.Bundles))
	for _, bundle := range catalog.Bundles {
		bundles = append(bundles, bundle)
	}
	sort.Slice(bundles, func(i, j int) bool { return bundles[i].ID < bundles[j].ID })
	return bundles
}

func ResolveKnowledgeDoc(catalog *KnowledgeCatalog, ref string) (*KnowledgeDoc, error) {
	if catalog == nil {
		return nil, ErrKnowledgeDocNotFound
	}
	ref = normalizeSkillToken(ref)
	if ref == "" {
		return nil, ErrKnowledgeDocNotFound
	}
	matches := make([]KnowledgeDoc, 0)
	for _, doc := range catalog.Docs {
		if normalizeSkillToken(doc.ID) == ref || normalizeSkillToken(doc.Name) == ref {
			matches = append(matches, doc)
		}
	}
	if len(matches) == 0 {
		return nil, fmt.Errorf("%w: %s", ErrKnowledgeDocNotFound, ref)
	}
	if len(matches) > 1 {
		return nil, fmt.Errorf("%w: %s", ErrKnowledgeRefAmbiguous, ref)
	}
	doc := matches[0]
	return &doc, nil
}

func ResolveKnowledgeBundle(catalog *KnowledgeCatalog, ref string) (*KnowledgeBundle, error) {
	if catalog == nil {
		return nil, ErrKnowledgeBundleNotFound
	}
	ref = normalizeSkillToken(ref)
	if ref == "" {
		return nil, ErrKnowledgeBundleNotFound
	}
	matches := make([]KnowledgeBundle, 0)
	for _, bundle := range catalog.Bundles {
		if normalizeSkillToken(bundle.ID) == ref || normalizeSkillToken(bundle.Name) == ref {
			matches = append(matches, bundle)
		}
	}
	if len(matches) == 0 {
		return nil, fmt.Errorf("%w: %s", ErrKnowledgeBundleNotFound, ref)
	}
	if len(matches) > 1 {
		return nil, fmt.Errorf("%w: %s", ErrKnowledgeRefAmbiguous, ref)
	}
	bundle := matches[0]
	return &bundle, nil
}

func ResolveKnowledgeReference(catalog *KnowledgeCatalog, ref, forcedKind string) (string, string, string, error) {
	switch strings.TrimSpace(strings.ToLower(forcedKind)) {
	case KnowledgeAttachmentDoc:
		doc, err := ResolveKnowledgeDoc(catalog, ref)
		if err != nil {
			return "", "", "", err
		}
		return KnowledgeAttachmentDoc, doc.ID, doc.Name, nil
	case KnowledgeAttachmentBundle:
		bundle, err := ResolveKnowledgeBundle(catalog, ref)
		if err != nil {
			return "", "", "", err
		}
		return KnowledgeAttachmentBundle, bundle.ID, bundle.Name, nil
	}

	doc, docErr := ResolveKnowledgeDoc(catalog, ref)
	bundle, bundleErr := ResolveKnowledgeBundle(catalog, ref)
	switch {
	case docErr == nil && bundleErr == nil:
		return "", "", "", fmt.Errorf("%w: %s matches both doc and bundle", ErrKnowledgeRefAmbiguous, ref)
	case docErr == nil:
		return KnowledgeAttachmentDoc, doc.ID, doc.Name, nil
	case bundleErr == nil:
		return KnowledgeAttachmentBundle, bundle.ID, bundle.Name, nil
	default:
		if errors.Is(docErr, ErrKnowledgeDocNotFound) && errors.Is(bundleErr, ErrKnowledgeBundleNotFound) {
			return "", "", "", fmt.Errorf("%w: %s", ErrKnowledgeDocNotFound, ref)
		}
		if docErr != nil && !errors.Is(docErr, ErrKnowledgeDocNotFound) {
			return "", "", "", docErr
		}
		return "", "", "", bundleErr
	}
}

func ExpandKnowledgeBundle(catalog *KnowledgeCatalog, bundle KnowledgeBundle) []KnowledgeDoc {
	if catalog == nil {
		return nil
	}
	seen := make(map[string]bool)
	out := make([]KnowledgeDoc, 0)
	for _, id := range bundle.DocIDs {
		if doc, ok := catalog.Docs[id]; ok {
			seen[doc.ID] = true
			out = append(out, doc)
		}
	}
	for _, doc := range catalog.Docs {
		if seen[doc.ID] || !strings.EqualFold(doc.Status, "active") {
			continue
		}
		if len(bundle.Kinds) > 0 && !containsFold(bundle.Kinds, doc.Kind) {
			continue
		}
		if len(bundle.Tags) > 0 && !sharesFold(bundle.Tags, doc.Tags) {
			continue
		}
		out = append(out, doc)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

func GetProjectKnowledgeManifestPath(projectPath string) string {
	return filepath.Join(projectPath, projectKnowledgeDirName, projectKnowledgeManifestName)
}

func LoadProjectKnowledgeManifest(projectPath string) (*ProjectKnowledgeManifest, error) {
	manifestPath := GetProjectKnowledgeManifestPath(projectPath)
	if _, err := os.Stat(manifestPath); os.IsNotExist(err) {
		return &ProjectKnowledgeManifest{Attachments: []ProjectKnowledgeAttachment{}}, nil
	}

	var manifest ProjectKnowledgeManifest
	if _, err := toml.DecodeFile(manifestPath, &manifest); err != nil {
		return nil, fmt.Errorf("failed to parse knowledge manifest: %w", err)
	}
	if manifest.Attachments == nil {
		manifest.Attachments = []ProjectKnowledgeAttachment{}
	}
	for i := range manifest.Attachments {
		manifest.Attachments[i] = normalizeKnowledgeAttachment(manifest.Attachments[i])
	}
	sortKnowledgeAttachments(manifest.Attachments)
	return &manifest, nil
}

func SaveProjectKnowledgeManifest(projectPath string, manifest *ProjectKnowledgeManifest) error {
	if manifest == nil {
		manifest = &ProjectKnowledgeManifest{}
	}
	if manifest.Attachments == nil {
		manifest.Attachments = []ProjectKnowledgeAttachment{}
	}
	for i := range manifest.Attachments {
		manifest.Attachments[i] = normalizeKnowledgeAttachment(manifest.Attachments[i])
	}
	sortKnowledgeAttachments(manifest.Attachments)

	path := GetProjectKnowledgeManifestPath(projectPath)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("failed to create knowledge manifest dir: %w", err)
	}

	var buf bytes.Buffer
	if err := toml.NewEncoder(&buf).Encode(manifest); err != nil {
		return fmt.Errorf("failed to encode knowledge manifest: %w", err)
	}

	tmpPath := path + ".tmp"
	if err := os.WriteFile(tmpPath, buf.Bytes(), 0o600); err != nil {
		return fmt.Errorf("failed to write knowledge manifest: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("failed to save knowledge manifest: %w", err)
	}
	return nil
}

func GetAttachedProjectKnowledge(projectPath string) ([]ProjectKnowledgeAttachment, error) {
	manifest, err := LoadProjectKnowledgeManifest(projectPath)
	if err != nil {
		return nil, err
	}
	out := make([]ProjectKnowledgeAttachment, len(manifest.Attachments))
	copy(out, manifest.Attachments)
	sortKnowledgeAttachments(out)
	return out, nil
}

func AttachKnowledgeToProject(projectPath string, catalog *KnowledgeCatalog, ref, forcedKind string) (*ProjectKnowledgeAttachment, error) {
	kind, id, name, err := ResolveKnowledgeReference(catalog, ref, forcedKind)
	if err != nil {
		return nil, err
	}
	manifest, err := LoadProjectKnowledgeManifest(projectPath)
	if err != nil {
		return nil, err
	}
	for _, existing := range manifest.Attachments {
		if strings.EqualFold(existing.Kind, kind) && strings.EqualFold(existing.ID, id) {
			return nil, fmt.Errorf("%w: %s", ErrKnowledgeAlreadyAttached, id)
		}
	}
	attachment := normalizeKnowledgeAttachment(ProjectKnowledgeAttachment{
		Kind:       kind,
		ID:         id,
		Name:       name,
		AttachedAt: time.Now().UTC().Format(time.RFC3339),
	})
	manifest.Attachments = append(manifest.Attachments, attachment)
	if err := SaveProjectKnowledgeManifest(projectPath, manifest); err != nil {
		return nil, err
	}
	return &attachment, nil
}

func DetachKnowledgeFromProject(projectPath string, catalog *KnowledgeCatalog, ref, forcedKind string) (*ProjectKnowledgeAttachment, error) {
	kind := strings.TrimSpace(strings.ToLower(forcedKind))
	id := strings.TrimSpace(ref)
	if kind == "" && catalog != nil {
		if resolvedKind, resolvedID, _, err := ResolveKnowledgeReference(catalog, ref, ""); err == nil {
			kind = resolvedKind
			id = resolvedID
		}
	}

	manifest, err := LoadProjectKnowledgeManifest(projectPath)
	if err != nil {
		return nil, err
	}

	matchIdx := -1
	for i, attachment := range manifest.Attachments {
		sameKind := kind == "" || strings.EqualFold(attachment.Kind, kind)
		if !sameKind {
			continue
		}
		if strings.EqualFold(attachment.ID, id) || strings.EqualFold(attachment.Name, ref) {
			if matchIdx != -1 {
				return nil, fmt.Errorf("%w: %s", ErrKnowledgeRefAmbiguous, ref)
			}
			matchIdx = i
		}
	}
	if matchIdx == -1 {
		return nil, fmt.Errorf("%w: %s", ErrKnowledgeNotAttached, ref)
	}

	removed := manifest.Attachments[matchIdx]
	manifest.Attachments = append(manifest.Attachments[:matchIdx], manifest.Attachments[matchIdx+1:]...)
	if err := SaveProjectKnowledgeManifest(projectPath, manifest); err != nil {
		return nil, err
	}
	return &removed, nil
}

func ExpandKnowledgeAttachments(catalog *KnowledgeCatalog, attachments []ProjectKnowledgeAttachment) []KnowledgeDoc {
	if catalog == nil {
		return nil
	}
	seen := make(map[string]bool)
	out := make([]KnowledgeDoc, 0)
	for _, attachment := range attachments {
		switch strings.TrimSpace(strings.ToLower(attachment.Kind)) {
		case KnowledgeAttachmentDoc:
			if doc, ok := catalog.Docs[attachment.ID]; ok && !seen[doc.ID] {
				seen[doc.ID] = true
				out = append(out, doc)
			}
		case KnowledgeAttachmentBundle:
			if bundle, ok := catalog.Bundles[attachment.ID]; ok {
				for _, doc := range ExpandKnowledgeBundle(catalog, bundle) {
					if !seen[doc.ID] {
						seen[doc.ID] = true
						out = append(out, doc)
					}
				}
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

func parseKnowledgeBundles(raw any) []KnowledgeBundle {
	switch typed := raw.(type) {
	case map[string]any:
		bundles := make([]KnowledgeBundle, 0, len(typed))
		for id, value := range typed {
			info, ok := value.(map[string]any)
			if !ok {
				continue
			}
			bundles = append(bundles, normalizeKnowledgeBundle(id, info))
		}
		sort.Slice(bundles, func(i, j int) bool { return bundles[i].ID < bundles[j].ID })
		return bundles
	case []any:
		bundles := make([]KnowledgeBundle, 0, len(typed))
		for _, value := range typed {
			info, ok := value.(map[string]any)
			if !ok {
				continue
			}
			id := strings.TrimSpace(toString(info["id"]))
			if id == "" {
				continue
			}
			bundles = append(bundles, normalizeKnowledgeBundle(id, info))
		}
		sort.Slice(bundles, func(i, j int) bool { return bundles[i].ID < bundles[j].ID })
		return bundles
	default:
		return nil
	}
}

func normalizeKnowledgeBundle(id string, info map[string]any) KnowledgeBundle {
	name := strings.TrimSpace(toString(info["name"]))
	if name == "" {
		name = strings.TrimSpace(id)
	}
	return KnowledgeBundle{
		ID:          strings.TrimSpace(id),
		Name:        name,
		Description: strings.TrimSpace(toString(info["description"])),
		DocIDs:      uniqSortedStrings(toStringSlice(info["docs"])),
		Kinds:       uniqSortedStrings(toStringSlice(info["kinds"])),
		Tags:        uniqSortedStrings(toStringSlice(info["tags"])),
	}
}

func normalizeKnowledgeAttachment(a ProjectKnowledgeAttachment) ProjectKnowledgeAttachment {
	a.Kind = strings.TrimSpace(strings.ToLower(a.Kind))
	if a.Kind == "" {
		a.Kind = KnowledgeAttachmentDoc
	}
	a.ID = strings.TrimSpace(a.ID)
	a.Name = strings.TrimSpace(a.Name)
	if a.AttachedAt == "" {
		a.AttachedAt = time.Now().UTC().Format(time.RFC3339)
	}
	return a
}

func sortKnowledgeAttachments(items []ProjectKnowledgeAttachment) {
	sort.Slice(items, func(i, j int) bool {
		if items[i].Kind == items[j].Kind {
			return items[i].ID < items[j].ID
		}
		return items[i].Kind < items[j].Kind
	})
}

func toString(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case fmt.Stringer:
		return t.String()
	case int:
		return fmt.Sprintf("%d", t)
	case int64:
		return fmt.Sprintf("%d", t)
	case float64:
		if t == float64(int(t)) {
			return fmt.Sprintf("%d", int(t))
		}
		return fmt.Sprintf("%v", t)
	default:
		return ""
	}
}

func toStringSlice(v any) []string {
	switch t := v.(type) {
	case []string:
		out := make([]string, 0, len(t))
		for _, item := range t {
			item = strings.TrimSpace(item)
			if item != "" {
				out = append(out, item)
			}
		}
		return out
	case []any:
		out := make([]string, 0, len(t))
		for _, item := range t {
			s := strings.TrimSpace(toString(item))
			if s != "" {
				out = append(out, s)
			}
		}
		return out
	default:
		return nil
	}
}

func toInt(v any) int {
	switch t := v.(type) {
	case int:
		return t
	case int64:
		return int(t)
	case float64:
		return int(t)
	default:
		return 0
	}
}

func maxInt(v, fallback int) int {
	if v <= 0 {
		return fallback
	}
	return v
}

func uniqSortedStrings(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	set := make(map[string]bool, len(values))
	out := make([]string, 0, len(values))
	for _, v := range values {
		v = strings.TrimSpace(v)
		if v == "" || set[v] {
			continue
		}
		set[v] = true
		out = append(out, v)
	}
	sort.Strings(out)
	return out
}

func inferKnowledgeKind(id string) string {
	if prefix := strings.SplitN(id, ".", 2)[0]; prefix != "" {
		return prefix
	}
	return ""
}

func copyMap(in map[string]any) map[string]any {
	out := make(map[string]any, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func defaultString(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

func containsFold(items []string, needle string) bool {
	for _, item := range items {
		if strings.EqualFold(item, needle) {
			return true
		}
	}
	return false
}

func sharesFold(left, right []string) bool {
	for _, l := range left {
		for _, r := range right {
			if strings.EqualFold(l, r) {
				return true
			}
		}
	}
	return false
}
