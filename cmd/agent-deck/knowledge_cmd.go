package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/asheshgoplani/agent-deck/internal/session"
)

func handleKnowledge(profile string, args []string) {
	if len(args) == 0 {
		printKnowledgeHelp()
		os.Exit(1)
	}

	switch args[0] {
	case "list", "ls":
		handleKnowledgeList(args[1:])
	case "show":
		handleKnowledgeShow(args[1:])
	case "search":
		handleKnowledgeSearch(args[1:])
	case "attached":
		handleKnowledgeAttached(profile, args[1:])
	case "attach":
		handleKnowledgeAttach(profile, args[1:])
	case "detach":
		handleKnowledgeDetach(profile, args[1:])
	case "bundle":
		handleKnowledgeBundle(args[1:])
	case "help", "-h", "--help":
		printKnowledgeHelp()
	default:
		fmt.Fprintf(os.Stderr, "Error: unknown knowledge command %q\n", args[0])
		printKnowledgeHelp()
		os.Exit(1)
	}
}

func printKnowledgeHelp() {
	fmt.Println("Usage: agent-deck knowledge <command> [options]")
	fmt.Println()
	fmt.Println("Manage external knowledge-base catalogs and attach docs/bundles to session projects.")
	fmt.Println()
	fmt.Println("Commands:")
	fmt.Println("  list                    List knowledge docs from the configured KB root")
	fmt.Println("  show <doc>              Show one knowledge doc and its content")
	fmt.Println("  search <query>          Search knowledge docs")
	fmt.Println("  bundle list             List knowledge bundles")
	fmt.Println("  bundle show <bundle>    Show one knowledge bundle and its docs")
	fmt.Println("  attached [id]           Show knowledge attached to a session project")
	fmt.Println("  attach <id> <ref>       Attach a knowledge doc or bundle to a session project")
	fmt.Println("  detach <id> <ref>       Detach a knowledge doc or bundle from a session project")
	fmt.Println()
	fmt.Println("Root resolution order:")
	fmt.Println("  1. --root")
	fmt.Println("  2. AGENTDECK_KNOWLEDGE_ROOT")
	fmt.Println("  3. [knowledge].root in ~/.agent-deck/config.toml")
	fmt.Println()
	fmt.Println("Examples:")
	fmt.Println("  agent-deck knowledge list")
	fmt.Println("  agent-deck knowledge search auth")
	fmt.Println("  agent-deck knowledge bundle list")
	fmt.Println("  agent-deck knowledge attach my-project repo.backend")
	fmt.Println("  agent-deck knowledge attach my-project default-onboarding")
}

func handleKnowledgeList(args []string) {
	fs := flag.NewFlagSet("knowledge list", flag.ExitOnError)
	jsonOutput := fs.Bool("json", false, "Output as JSON")
	quiet := fs.Bool("quiet", false, "Minimal output")
	quietShort := fs.Bool("q", false, "Minimal output (short)")
	root := fs.String("root", "", "Override KB root path")
	kind := fs.String("kind", "", "Filter by doc kind")

	fs.Usage = func() {
		fmt.Println("Usage: agent-deck knowledge list [options]")
		fmt.Println()
		fmt.Println("List knowledge docs from the configured KB root.")
		fmt.Println()
		fmt.Println("Options:")
		fs.PrintDefaults()
	}

	if err := fs.Parse(normalizeArgs(fs, args)); err != nil {
		os.Exit(1)
	}
	quietMode := *quiet || *quietShort
	out := NewCLIOutput(*jsonOutput, quietMode)

	catalog, err := session.LoadKnowledgeCatalog(*root)
	if err != nil {
		out.Error(formatKnowledgeRootError(err), ErrCodeInvalidOperation)
		os.Exit(1)
	}
	docs := session.ListKnowledgeDocs(catalog, *kind)

	if *jsonOutput {
		out.Print("", map[string]interface{}{
			"root":       catalog.Root,
			"categories": catalog.Categories,
			"docs":       docs,
		})
		return
	}
	if quietMode {
		for _, doc := range docs {
			fmt.Println(doc.ID)
		}
		return
	}
	if len(docs) == 0 {
		fmt.Println("No knowledge docs found.")
		return
	}

	fmt.Printf("Knowledge root: %s\n\n", FormatPath(catalog.Root))
	fmt.Printf("%-28s %-12s %s\n", "DOC", "KIND", "DESCRIPTION")
	fmt.Println(strings.Repeat("-", 80))
	for _, doc := range docs {
		desc := doc.Description
		if desc == "" {
			desc = "-"
		}
		if len(desc) > 64 {
			desc = desc[:61] + "..."
		}
		fmt.Printf("%-28s %-12s %s\n", doc.ID, doc.Kind, desc)
	}
	fmt.Printf("\nTotal: %d docs\n", len(docs))
}

func handleKnowledgeShow(args []string) {
	fs := flag.NewFlagSet("knowledge show", flag.ExitOnError)
	jsonOutput := fs.Bool("json", false, "Output as JSON")
	root := fs.String("root", "", "Override KB root path")

	fs.Usage = func() {
		fmt.Println("Usage: agent-deck knowledge show <doc> [options]")
		fmt.Println()
		fmt.Println("Show one knowledge doc and its content.")
		fmt.Println()
		fmt.Println("Options:")
		fs.PrintDefaults()
	}

	if err := fs.Parse(normalizeArgs(fs, args)); err != nil {
		os.Exit(1)
	}
	if fs.NArg() < 1 {
		fs.Usage()
		os.Exit(1)
	}
	ref := fs.Arg(0)
	out := NewCLIOutput(*jsonOutput, false)

	catalog, err := session.LoadKnowledgeCatalog(*root)
	if err != nil {
		out.Error(formatKnowledgeRootError(err), ErrCodeInvalidOperation)
		os.Exit(1)
	}
	doc, err := session.ResolveKnowledgeDoc(catalog, ref)
	if err != nil {
		out.Error(err.Error(), ErrCodeNotFound)
		os.Exit(2)
	}
	content, err := session.ReadKnowledgeDocContent(*doc)
	if err != nil {
		out.Error(fmt.Sprintf("failed to read content: %v", err), ErrCodeInvalidOperation)
		os.Exit(1)
	}

	out.Print(
		fmt.Sprintf(
			"Doc:   %s\nKind:  %s\nPath:  %s\nRepos: %s\nTags:  %s\n\n%s\n",
			doc.ID,
			doc.Kind,
			FormatPath(doc.FilePath),
			strings.Join(doc.Repos, ", "),
			strings.Join(doc.Tags, ", "),
			content,
		),
		map[string]interface{}{
			"root":    catalog.Root,
			"doc":     doc,
			"content": content,
		},
	)
}

func handleKnowledgeSearch(args []string) {
	fs := flag.NewFlagSet("knowledge search", flag.ExitOnError)
	jsonOutput := fs.Bool("json", false, "Output as JSON")
	quiet := fs.Bool("quiet", false, "Minimal output")
	quietShort := fs.Bool("q", false, "Minimal output (short)")
	root := fs.String("root", "", "Override KB root path")
	kind := fs.String("kind", "", "Filter by doc kind")

	fs.Usage = func() {
		fmt.Println("Usage: agent-deck knowledge search <query> [options]")
		fmt.Println()
		fmt.Println("Search knowledge docs by id/name/description/tags/repos.")
		fmt.Println()
		fmt.Println("Options:")
		fs.PrintDefaults()
	}

	if err := fs.Parse(normalizeArgs(fs, args)); err != nil {
		os.Exit(1)
	}
	if fs.NArg() < 1 {
		fs.Usage()
		os.Exit(1)
	}
	query := strings.Join(fs.Args(), " ")
	quietMode := *quiet || *quietShort
	out := NewCLIOutput(*jsonOutput, quietMode)

	catalog, err := session.LoadKnowledgeCatalog(*root)
	if err != nil {
		out.Error(formatKnowledgeRootError(err), ErrCodeInvalidOperation)
		os.Exit(1)
	}
	docs := session.SearchKnowledgeDocs(catalog, query, *kind)

	if *jsonOutput {
		out.Print("", map[string]interface{}{
			"root":  catalog.Root,
			"query": query,
			"docs":  docs,
		})
		return
	}
	if quietMode {
		for _, doc := range docs {
			fmt.Println(doc.ID)
		}
		return
	}
	if len(docs) == 0 {
		fmt.Println("No knowledge docs matched.")
		return
	}
	fmt.Printf("Knowledge matches for %q:\n\n", query)
	for _, doc := range docs {
		desc := doc.Description
		if desc == "" {
			desc = "-"
		}
		fmt.Printf("- %s [%s] %s\n", doc.ID, doc.Kind, desc)
	}
}

func handleKnowledgeBundle(args []string) {
	if len(args) == 0 {
		printKnowledgeBundleHelp()
		os.Exit(1)
	}
	switch args[0] {
	case "list", "ls":
		handleKnowledgeBundleList(args[1:])
	case "show":
		handleKnowledgeBundleShow(args[1:])
	case "help", "-h", "--help":
		printKnowledgeBundleHelp()
	default:
		fmt.Fprintf(os.Stderr, "Error: unknown knowledge bundle command %q\n", args[0])
		printKnowledgeBundleHelp()
		os.Exit(1)
	}
}

func printKnowledgeBundleHelp() {
	fmt.Println("Usage: agent-deck knowledge bundle <command> [options]")
	fmt.Println()
	fmt.Println("Commands:")
	fmt.Println("  list            List defined bundles from the KB root")
	fmt.Println("  show <bundle>   Show a bundle and its expanded doc set")
}

func handleKnowledgeBundleList(args []string) {
	fs := flag.NewFlagSet("knowledge bundle list", flag.ExitOnError)
	jsonOutput := fs.Bool("json", false, "Output as JSON")
	quiet := fs.Bool("quiet", false, "Minimal output")
	quietShort := fs.Bool("q", false, "Minimal output (short)")
	root := fs.String("root", "", "Override KB root path")

	if err := fs.Parse(normalizeArgs(fs, args)); err != nil {
		os.Exit(1)
	}
	quietMode := *quiet || *quietShort
	out := NewCLIOutput(*jsonOutput, quietMode)

	catalog, err := session.LoadKnowledgeCatalog(*root)
	if err != nil {
		out.Error(formatKnowledgeRootError(err), ErrCodeInvalidOperation)
		os.Exit(1)
	}
	bundles := session.ListKnowledgeBundles(catalog)

	if *jsonOutput {
		out.Print("", map[string]interface{}{"root": catalog.Root, "bundles": bundles})
		return
	}
	if quietMode {
		for _, bundle := range bundles {
			fmt.Println(bundle.ID)
		}
		return
	}
	if len(bundles) == 0 {
		fmt.Println("No knowledge bundles found.")
		return
	}
	fmt.Printf("Knowledge bundles (%s):\n\n", FormatPath(catalog.Root))
	for _, bundle := range bundles {
		fmt.Printf("- %s: %s\n", bundle.ID, defaultText(bundle.Description, bundle.Name))
	}
}

func handleKnowledgeBundleShow(args []string) {
	fs := flag.NewFlagSet("knowledge bundle show", flag.ExitOnError)
	jsonOutput := fs.Bool("json", false, "Output as JSON")
	root := fs.String("root", "", "Override KB root path")

	if err := fs.Parse(normalizeArgs(fs, args)); err != nil {
		os.Exit(1)
	}
	if fs.NArg() < 1 {
		fs.Usage()
		os.Exit(1)
	}
	out := NewCLIOutput(*jsonOutput, false)

	catalog, err := session.LoadKnowledgeCatalog(*root)
	if err != nil {
		out.Error(formatKnowledgeRootError(err), ErrCodeInvalidOperation)
		os.Exit(1)
	}
	bundle, err := session.ResolveKnowledgeBundle(catalog, fs.Arg(0))
	if err != nil {
		out.Error(err.Error(), ErrCodeNotFound)
		os.Exit(2)
	}
	docs := session.ExpandKnowledgeBundle(catalog, *bundle)

	if *jsonOutput {
		out.Print("", map[string]interface{}{
			"root":   catalog.Root,
			"bundle": bundle,
			"docs":   docs,
		})
		return
	}
	fmt.Printf("Bundle: %s\n", bundle.ID)
	if bundle.Description != "" {
		fmt.Printf("About:  %s\n", bundle.Description)
	}
	fmt.Println()
	for _, doc := range docs {
		fmt.Printf("- %s [%s]\n", doc.ID, doc.Kind)
	}
}

func handleKnowledgeAttached(profile string, args []string) {
	fs := flag.NewFlagSet("knowledge attached", flag.ExitOnError)
	jsonOutput := fs.Bool("json", false, "Output as JSON")
	quiet := fs.Bool("quiet", false, "Minimal output")
	quietShort := fs.Bool("q", false, "Minimal output (short)")
	root := fs.String("root", "", "Override KB root path")

	fs.Usage = func() {
		fmt.Println("Usage: agent-deck knowledge attached [session-id] [options]")
		fmt.Println()
		fmt.Println("Show knowledge refs attached to a session project.")
		fmt.Println("If no session ID is provided, uses the current session if inside tmux.")
		fmt.Println()
		fmt.Println("Options:")
		fs.PrintDefaults()
	}

	if err := fs.Parse(normalizeArgs(fs, args)); err != nil {
		os.Exit(1)
	}
	quietMode := *quiet || *quietShort
	out := NewCLIOutput(*jsonOutput, quietMode)

	storage, err := session.NewStorageWithProfile(profile)
	if err != nil {
		out.Error(fmt.Sprintf("failed to initialize storage: %v", err), ErrCodeNotFound)
		os.Exit(1)
	}
	instances, _, err := storage.LoadWithGroups()
	if err != nil {
		out.Error(fmt.Sprintf("failed to load sessions: %v", err), ErrCodeNotFound)
		os.Exit(1)
	}
	inst, errMsg, errCode := ResolveSessionOrCurrent(fs.Arg(0), instances)
	if inst == nil {
		out.Error(errMsg, errCode)
		os.Exit(2)
	}

	attachments, err := session.GetAttachedProjectKnowledge(inst.ProjectPath)
	if err != nil {
		out.Error(fmt.Sprintf("failed to load attached knowledge: %v", err), ErrCodeInvalidOperation)
		os.Exit(1)
	}

	var catalog *session.KnowledgeCatalog
	if loaded, err := session.LoadKnowledgeCatalog(*root); err == nil {
		catalog = loaded
	}
	effectiveDocs := session.ExpandKnowledgeAttachments(catalog, attachments)

	if *jsonOutput {
		out.Print("", map[string]interface{}{
			"session":        inst.Title,
			"session_id":     inst.ID,
			"project_path":   inst.ProjectPath,
			"manifest_path":  session.GetProjectKnowledgeManifestPath(inst.ProjectPath),
			"attachments":    attachments,
			"effective_docs": effectiveDocs,
		})
		return
	}
	if quietMode {
		for _, attachment := range attachments {
			fmt.Printf("%s:%s\n", attachment.Kind, attachment.ID)
		}
		return
	}

	fmt.Printf("Session: %s\n", inst.Title)
	fmt.Printf("Project: %s\n", FormatPath(inst.ProjectPath))
	fmt.Printf("Manifest: %s\n\n", FormatPath(session.GetProjectKnowledgeManifestPath(inst.ProjectPath)))

	if len(attachments) == 0 {
		fmt.Println("No knowledge attached to this project.")
		return
	}

	fmt.Println("ATTACHED:")
	for _, attachment := range attachments {
		fmt.Printf("- %s: %s\n", attachment.Kind, attachment.ID)
	}
	if len(effectiveDocs) > 0 {
		fmt.Println()
		fmt.Println("EFFECTIVE DOCS:")
		for _, doc := range effectiveDocs {
			fmt.Printf("- %s [%s]\n", doc.ID, doc.Kind)
		}
	}
}

func handleKnowledgeAttach(profile string, args []string) {
	fs := flag.NewFlagSet("knowledge attach", flag.ExitOnError)
	jsonOutput := fs.Bool("json", false, "Output as JSON")
	root := fs.String("root", "", "Override KB root path")
	kind := fs.String("kind", "", "Force ref kind: doc or bundle")

	fs.Usage = func() {
		fmt.Println("Usage: agent-deck knowledge attach <session-id> <doc-or-bundle> [options]")
		fmt.Println()
		fmt.Println("Attach a knowledge doc or bundle to a session project.")
		fmt.Println()
		fmt.Println("Options:")
		fs.PrintDefaults()
	}

	if err := fs.Parse(normalizeArgs(fs, args)); err != nil {
		os.Exit(1)
	}
	if fs.NArg() < 2 {
		fs.Usage()
		os.Exit(1)
	}
	out := NewCLIOutput(*jsonOutput, false)

	catalog, err := session.LoadKnowledgeCatalog(*root)
	if err != nil {
		out.Error(formatKnowledgeRootError(err), ErrCodeInvalidOperation)
		os.Exit(1)
	}
	inst, errMsg, errCode := resolveKnowledgeSession(profile, fs.Arg(0))
	if inst == nil {
		out.Error(errMsg, errCode)
		os.Exit(2)
	}

	attachment, err := session.AttachKnowledgeToProject(inst.ProjectPath, catalog, fs.Arg(1), *kind)
	if err != nil {
		out.Error(err.Error(), ErrCodeInvalidOperation)
		os.Exit(1)
	}

	out.Success(
		fmt.Sprintf("Attached %s '%s' to '%s'", attachment.Kind, attachment.ID, inst.Title),
		map[string]interface{}{
			"success":      true,
			"session_id":   inst.ID,
			"session":      inst.Title,
			"project_path": inst.ProjectPath,
			"attachment":   attachment,
		},
	)
}

func handleKnowledgeDetach(profile string, args []string) {
	fs := flag.NewFlagSet("knowledge detach", flag.ExitOnError)
	jsonOutput := fs.Bool("json", false, "Output as JSON")
	root := fs.String("root", "", "Override KB root path")
	kind := fs.String("kind", "", "Force ref kind: doc or bundle")

	fs.Usage = func() {
		fmt.Println("Usage: agent-deck knowledge detach <session-id> <doc-or-bundle> [options]")
		fmt.Println()
		fmt.Println("Detach a knowledge doc or bundle from a session project.")
		fmt.Println()
		fmt.Println("Options:")
		fs.PrintDefaults()
	}

	if err := fs.Parse(normalizeArgs(fs, args)); err != nil {
		os.Exit(1)
	}
	if fs.NArg() < 2 {
		fs.Usage()
		os.Exit(1)
	}
	out := NewCLIOutput(*jsonOutput, false)
	inst, errMsg, errCode := resolveKnowledgeSession(profile, fs.Arg(0))
	if inst == nil {
		out.Error(errMsg, errCode)
		os.Exit(2)
	}

	var catalog *session.KnowledgeCatalog
	if loaded, err := session.LoadKnowledgeCatalog(*root); err == nil {
		catalog = loaded
	}
	attachment, err := session.DetachKnowledgeFromProject(inst.ProjectPath, catalog, fs.Arg(1), *kind)
	if err != nil {
		out.Error(err.Error(), ErrCodeInvalidOperation)
		os.Exit(1)
	}

	out.Success(
		fmt.Sprintf("Detached %s '%s' from '%s'", attachment.Kind, attachment.ID, inst.Title),
		map[string]interface{}{
			"success":      true,
			"session_id":   inst.ID,
			"session":      inst.Title,
			"project_path": inst.ProjectPath,
			"attachment":   attachment,
		},
	)
}

func resolveKnowledgeSession(profile, identifier string) (*session.Instance, string, string) {
	storage, err := session.NewStorageWithProfile(profile)
	if err != nil {
		return nil, fmt.Sprintf("failed to initialize storage: %v", err), ErrCodeNotFound
	}
	instances, _, err := storage.LoadWithGroups()
	if err != nil {
		return nil, fmt.Sprintf("failed to load sessions: %v", err), ErrCodeNotFound
	}
	return ResolveSessionOrCurrent(identifier, instances)
}

func formatKnowledgeRootError(err error) string {
	if errors.Is(err, session.ErrKnowledgeRootUnset) {
		return "knowledge root is not configured; set --root, AGENTDECK_KNOWLEDGE_ROOT, or [knowledge].root in config.toml"
	}
	return err.Error()
}

func defaultText(value, fallback string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return fallback
	}
	return value
}

func sortKnowledgeAttachments(items []session.ProjectKnowledgeAttachment) {
	sort.Slice(items, func(i, j int) bool {
		if items[i].Kind == items[j].Kind {
			return items[i].ID < items[j].ID
		}
		return items[i].Kind < items[j].Kind
	})
}
