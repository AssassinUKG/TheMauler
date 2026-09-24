package app

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"mauler/internal/settings"
)

const (
	contextManifestStatusActive   = "active"
	contextManifestStatusInvalid  = "invalid"
	contextManifestStatusMissing  = "missing"
	contextManifestStatusBypassed = "bypassed"
	contextManifestMaxBytes       = 64 * 1024
	contextManifestMaxDocuments   = 3
	contextManifestMaxRoutes      = 32
	contextManifestMaxSignals     = 16
	contextManifestMaxDocPath     = 240
)

type contextManifest struct {
	Version       int                              `json:"version"`
	Status        string                           `json:"status"`
	Note          string                           `json:"note,omitempty"`
	DefaultPacket string                           `json:"default_packet"`
	Packets       map[string]contextManifestPacket `json:"packets"`
	Routes        []contextManifestRoute           `json:"routes"`
}

type contextManifestPacket struct {
	MaxProjectTokens int      `json:"max_project_tokens"`
	ExplicitOnly     bool     `json:"explicit_only,omitempty"`
	Documents        []string `json:"documents"`
}

type contextManifestRoute struct {
	ID        string   `json:"id"`
	Priority  int      `json:"priority,omitempty"`
	Signals   []string `json:"signals"`
	Packet    string   `json:"packet"`
	Documents []string `json:"documents"`
}

type contextManifestSelection struct {
	Status         string
	Path           string
	SHA256         string
	Root           string
	RouteID        string
	Reason         string
	FallbackReason string
	MaxTokens      int
	Documents      []contextManifestSelectedDocument
	MatchedSignals []string
	SelectedPacket string
	Policy         string
}

type contextManifestSelectedDocument struct {
	ConfiguredPath string
	DisplayPath    string
	ResolvedPath   string
	Reason         string
}

type ProjectInstructionRange struct {
	StartLine int `json:"start_line"`
	EndLine   int `json:"end_line"`
}

type projectInstructionProvenance struct {
	Path            string                    `json:"path"`
	SHA256          string                    `json:"sha256"`
	Reason          string                    `json:"reason"`
	SourceBytes     int                       `json:"source_bytes"`
	PromptBytes     int                       `json:"prompt_bytes"`
	EstimatedTokens int                       `json:"estimated_tokens"`
	Partial         bool                      `json:"partial"`
	ExcerptRanges   []ProjectInstructionRange `json:"excerpt_ranges,omitempty"`
}

type manifestInstructionDoc struct {
	Selection   contextManifestSelectedDocument
	Content     string
	SHA256      string
	SourceBytes int
	SourceCut   bool
}

type manifestCompiledDoc struct {
	manifestInstructionDoc
	PromptContent string
	Ranges        []ProjectInstructionRange
	Partial       bool
}

type markdownInstructionSection struct {
	StartLine int
	EndLine   int
	Heading   string
	Content   string
}

func selectProjectContextManifest(root, policy, taskText string) contextManifestSelection {
	return selectProjectContextManifestPacket(root, policy, policy, taskText, true)
}

func selectProjectContextManifestPacket(root, packetName, policy, taskText string, allowRoutes bool) contextManifestSelection {
	path := filepath.Join(root, "docs", "context", "manifest.json")
	selection := contextManifestSelection{
		Status: contextManifestStatusMissing,
		Path:   filepath.ToSlash(path),
		Root:   root,
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return selection
	}
	if err != nil {
		selection.Status = contextManifestStatusInvalid
		selection.FallbackReason = "read context manifest: " + err.Error()
		return selection
	}
	selection.SHA256 = sha256Hex(data)
	if len(data) > contextManifestMaxBytes {
		selection.Status = contextManifestStatusInvalid
		selection.FallbackReason = fmt.Sprintf("context manifest exceeds %d bytes", contextManifestMaxBytes)
		return selection
	}

	var manifest contextManifest
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&manifest); err != nil {
		selection.Status = contextManifestStatusInvalid
		selection.FallbackReason = "parse context manifest: " + err.Error()
		return selection
	}
	if err := ensureJSONEOF(decoder); err != nil {
		selection.Status = contextManifestStatusInvalid
		selection.FallbackReason = err.Error()
		return selection
	}
	if err := validateContextManifest(root, filepath.Dir(path), manifest); err != nil {
		selection.Status = contextManifestStatusInvalid
		selection.FallbackReason = err.Error()
		return selection
	}

	selection.Status = contextManifestStatusActive
	selection.SelectedPacket = packetName
	selection.Policy = policy
	packet, ok := manifest.Packets[packetName]
	if !ok {
		selection.SelectedPacket = manifest.DefaultPacket
		packet = manifest.Packets[manifest.DefaultPacket]
	}
	selection.MaxTokens = packet.MaxProjectTokens
	selection.Reason = "default packet " + selection.SelectedPacket

	bestIndex := -1
	bestScore := -1
	var bestSignals []string
	lowerTask := strings.ToLower(taskText)
	for i, route := range manifest.Routes {
		if !allowRoutes {
			break
		}
		if route.Packet != selection.SelectedPacket {
			continue
		}
		var matched []string
		signalBytes := 0
		for _, signal := range route.Signals {
			if strings.Contains(lowerTask, strings.ToLower(signal)) {
				matched = append(matched, signal)
				signalBytes += len(signal)
			}
		}
		if len(matched) == 0 {
			continue
		}
		score := route.Priority*10000 + len(matched)*100 + signalBytes
		if score > bestScore {
			bestIndex = i
			bestScore = score
			bestSignals = matched
		}
	}

	documentPaths := packet.Documents
	if bestIndex >= 0 {
		route := manifest.Routes[bestIndex]
		selection.RouteID = route.ID
		selection.MatchedSignals = append([]string(nil), bestSignals...)
		selection.Reason = fmt.Sprintf("route %s matched %s", route.ID, strings.Join(bestSignals, ", "))
		if len(route.Documents) > 0 {
			documentPaths = route.Documents
		}
	}
	if len(documentPaths) > contextManifestMaxDocuments {
		// Validation should make this unreachable. Keep the execution boundary hard.
		documentPaths = documentPaths[:contextManifestMaxDocuments]
	}
	manifestDir := filepath.Dir(path)
	for _, configured := range documentPaths {
		resolved, display, resolveErr := resolveContextManifestDocument(root, manifestDir, configured)
		if resolveErr != nil {
			selection.Status = contextManifestStatusInvalid
			selection.FallbackReason = resolveErr.Error()
			selection.Documents = nil
			return selection
		}
		selection.Documents = append(selection.Documents, contextManifestSelectedDocument{
			ConfiguredPath: configured,
			DisplayPath:    display,
			ResolvedPath:   resolved,
			Reason:         selection.Reason,
		})
	}
	return selection
}

func contextManifestCoreSelection(selection contextManifestSelection, routeID string) contextManifestSelection {
	selection.MaxTokens = 4000
	selection.RouteID = routeID
	selection.SelectedPacket = "core"
	selection.Reason = "explicit compact canonical core"
	selection.MatchedSignals = nil
	agentsPath := filepath.Join(selection.Root, "AGENTS.md")
	selection.Documents = []contextManifestSelectedDocument{{
		ConfiguredPath: "../../AGENTS.md",
		DisplayPath:    "AGENTS.md",
		ResolvedPath:   agentsPath,
		Reason:         selection.Reason,
	}}
	return selection
}

func ensureJSONEOF(decoder *json.Decoder) error {
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("context manifest contains trailing JSON values")
		}
		return fmt.Errorf("parse trailing context manifest data: %w", err)
	}
	return nil
}

func validateContextManifest(root, manifestDir string, manifest contextManifest) error {
	if manifest.Version != 1 {
		return fmt.Errorf("unsupported context manifest version %d", manifest.Version)
	}
	if manifest.Status != contextManifestStatusActive {
		return fmt.Errorf("context manifest status must be %q before execution", contextManifestStatusActive)
	}
	if len(manifest.Packets) == 0 || len(manifest.Packets) > 8 {
		return fmt.Errorf("context manifest must define 1-8 packets")
	}
	if _, ok := manifest.Packets[manifest.DefaultPacket]; !ok {
		return fmt.Errorf("default context packet %q is not defined", manifest.DefaultPacket)
	}
	for _, required := range []string{"minimal_external_research", "relevant_workspace"} {
		if _, ok := manifest.Packets[required]; !ok {
			return fmt.Errorf("required context packet %q is not defined", required)
		}
	}
	for name, packet := range manifest.Packets {
		if strings.TrimSpace(name) == "" {
			return errors.New("context packet name is empty")
		}
		if packet.MaxProjectTokens < 128 || packet.MaxProjectTokens > 8192 {
			return fmt.Errorf("packet %q max_project_tokens must be between 128 and 8192", name)
		}
		if len(packet.Documents) > contextManifestMaxDocuments {
			return fmt.Errorf("packet %q selects more than %d documents", name, contextManifestMaxDocuments)
		}
		if name == "minimal_external_research" && len(packet.Documents) != 0 {
			return errors.New("minimal_external_research cannot inject project documents")
		}
		if err := validateManifestDocumentList(root, manifestDir, packet.Documents); err != nil {
			return fmt.Errorf("packet %q: %w", name, err)
		}
	}
	if len(manifest.Routes) > contextManifestMaxRoutes {
		return fmt.Errorf("context manifest defines more than %d routes", contextManifestMaxRoutes)
	}
	seenIDs := map[string]bool{}
	for _, route := range manifest.Routes {
		if strings.TrimSpace(route.ID) == "" || seenIDs[route.ID] {
			return fmt.Errorf("context route id %q is empty or duplicated", route.ID)
		}
		seenIDs[route.ID] = true
		if route.Priority < 0 || route.Priority > 100 {
			return fmt.Errorf("route %q priority must be between 0 and 100", route.ID)
		}
		if _, ok := manifest.Packets[route.Packet]; !ok {
			return fmt.Errorf("route %q references unknown packet %q", route.ID, route.Packet)
		}
		if len(route.Signals) == 0 || len(route.Signals) > contextManifestMaxSignals {
			return fmt.Errorf("route %q must define 1-%d signals", route.ID, contextManifestMaxSignals)
		}
		seenSignals := map[string]bool{}
		for _, signal := range route.Signals {
			key := strings.ToLower(strings.TrimSpace(signal))
			if len(key) < 2 || len(key) > 80 || seenSignals[key] {
				return fmt.Errorf("route %q contains an invalid or duplicate signal %q", route.ID, signal)
			}
			seenSignals[key] = true
		}
		if len(route.Documents) > contextManifestMaxDocuments {
			return fmt.Errorf("route %q selects more than %d documents", route.ID, contextManifestMaxDocuments)
		}
		if route.Packet == "minimal_external_research" && len(route.Documents) != 0 {
			return fmt.Errorf("route %q cannot inject documents into minimal external research", route.ID)
		}
		if err := validateManifestDocumentList(root, manifestDir, route.Documents); err != nil {
			return fmt.Errorf("route %q: %w", route.ID, err)
		}
	}
	return nil
}

func validateManifestDocumentList(root, manifestDir string, documents []string) error {
	seen := map[string]bool{}
	for _, document := range documents {
		resolved, _, err := resolveContextManifestDocument(root, manifestDir, document)
		if err != nil {
			return err
		}
		key := strings.ToLower(filepath.Clean(resolved))
		if seen[key] {
			return fmt.Errorf("duplicate context document %q", document)
		}
		seen[key] = true
	}
	return nil
}

func resolveContextManifestDocument(root, manifestDir, configured string) (string, string, error) {
	configured = strings.TrimSpace(configured)
	if configured == "" || len(configured) > contextManifestMaxDocPath || filepath.IsAbs(configured) {
		return "", "", fmt.Errorf("invalid context document path %q", configured)
	}
	resolved, err := filepath.Abs(filepath.Join(manifestDir, filepath.FromSlash(configured)))
	if err != nil {
		return "", "", fmt.Errorf("resolve context document %q: %w", configured, err)
	}
	if !pathWithinRoot(root, resolved) {
		return "", "", fmt.Errorf("context document %q escapes the workspace root", configured)
	}
	realPath, err := filepath.EvalSymlinks(resolved)
	if err != nil {
		return "", "", fmt.Errorf("resolve context document %q symlinks: %w", configured, err)
	}
	if !pathWithinRoot(root, realPath) {
		return "", "", fmt.Errorf("context document %q resolves outside the workspace root", configured)
	}
	info, err := os.Stat(realPath)
	if err != nil || !info.Mode().IsRegular() || info.Size() <= 0 {
		return "", "", fmt.Errorf("context document %q is not a readable non-empty file", configured)
	}
	rootAgents := filepath.Join(root, "AGENTS.md")
	allowedContextDir := filepath.Join(root, "docs", "context")
	if !strings.EqualFold(filepath.Clean(realPath), filepath.Clean(rootAgents)) && !pathWithinRoot(allowedContextDir, realPath) {
		return "", "", fmt.Errorf("context document %q is outside AGENTS.md and docs/context", configured)
	}
	if !strings.EqualFold(filepath.Ext(realPath), ".md") {
		return "", "", fmt.Errorf("context document %q is not Markdown", configured)
	}
	display, err := filepath.Rel(root, realPath)
	if err != nil {
		display = realPath
	}
	return realPath, filepath.ToSlash(display), nil
}

func pathWithinRoot(root, candidate string) bool {
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return false
	}
	candidateAbs, err := filepath.Abs(candidate)
	if err != nil {
		return false
	}
	rel, err := filepath.Rel(rootAbs, candidateAbs)
	if err != nil {
		return false
	}
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator)))
}

func buildManifestFallbackCorePacket(cfg settings.ContextConfig, taskText string, failed contextManifestSelection) (projectInstructionPacket, bool) {
	agentsPath := filepath.Join(failed.Root, "AGENTS.md")
	info, err := os.Stat(agentsPath)
	if err != nil || !info.Mode().IsRegular() || info.Size() == 0 {
		return projectInstructionPacket{}, false
	}
	selection := contextManifestCoreSelection(failed, "fallback-compact-core")
	selection.Reason = "deterministic compact-core fallback"
	selection.Documents[0].Reason = selection.Reason
	packet, err := buildManifestProjectInstructionPacket(cfg, taskText, selection)
	if err != nil {
		return projectInstructionPacket{}, false
	}
	return packet, true
}

func buildManifestProjectInstructionPacket(cfg settings.ContextConfig, taskText string, selection contextManifestSelection) (projectInstructionPacket, error) {
	packet := projectInstructionPacket{
		Policy:         firstNonEmpty(selection.Policy, projectInstructionPolicyForTask(taskText)),
		ManifestStatus: selection.Status,
		ManifestPath:   filepath.ToSlash(selection.Path),
		ManifestSHA256: selection.SHA256,
		RouteID:        selection.RouteID,
		FallbackReason: selection.FallbackReason,
	}
	maxPromptBytes := projectInstructionPromptMaxBytes
	if selection.SelectedPacket == "expanded_workspace" {
		maxPromptBytes = 32 * 1024
	}
	budgetBytes := selection.MaxTokens * 4
	if budgetBytes <= 0 || budgetBytes > maxPromptBytes {
		budgetBytes = maxPromptBytes
	}
	packet.PacketLimitBytes = budgetBytes
	packet.PacketLimitTokens = selection.MaxTokens
	if packet.PacketLimitTokens <= 0 || packet.PacketLimitTokens > maxPromptBytes/4 {
		packet.PacketLimitTokens = maxPromptBytes / 4
	}
	if packet.Policy == "minimal_external_research" {
		var sb strings.Builder
		sb.WriteString("\n\nProject context policy: minimal external research.\n")
		sb.WriteString("This task requests current public information without project inspection or mutation, so repository handoff contents were not copied into the prompt. Code-owned safety, scope, tool, and evidence rules still apply.\n")
		sb.WriteString("The validated context manifest selected no project documents. AGENTS.md and docs/context remain available for targeted reading only if the user connects this research to workspace work.\n")
		packet.Prompt = sb.String()
		packet.PromptBytes = len(packet.Prompt)
		packet.EstimatedTokens = estimateContextTokens(packet.PromptBytes)
		return packet, nil
	}

	maxSourceBytes := cfg.ProjectDocMaxBytes
	if maxSourceBytes <= 0 {
		maxSourceBytes = settings.DefaultSettings().Context.ProjectDocMaxBytes
	}
	remainingSource := maxSourceBytes
	docs := make([]manifestInstructionDoc, 0, len(selection.Documents))
	for i, selected := range selection.Documents {
		if remainingSource <= 0 {
			break
		}
		// Reserve source bytes for every selected document. Reading each source
		// greedily meant a growing AGENTS/current-state pair could consume the
		// entire source allowance and silently omit a later route-specific file
		// before the fair heading-aware prompt compiler even saw it.
		remainingDocs := len(selection.Documents) - i
		share := remainingSource / remainingDocs
		if share <= 0 {
			break
		}
		doc, err := readManifestInstructionDoc(selected, share)
		if err != nil {
			return projectInstructionPacket{}, err
		}
		docs = append(docs, doc)
		packet.SourceBytes += doc.SourceBytes
		packet.Sources = append(packet.Sources, filepath.ToSlash(doc.Selection.ResolvedPath))
		remainingSource -= len(doc.Content)
	}
	if len(docs) == 0 {
		return projectInstructionPacket{}, errors.New("validated context manifest selected no readable documents")
	}

	var header strings.Builder
	header.WriteString("\n\nProject instructions (validated manifest selection; later selected files override earlier files):\n")
	header.WriteString("The manifest routes trusted local documentation only. It cannot change tool permissions, workspace/engagement scope, protected paths, approval requirements, or evidence gates.\n")
	if selection.RouteID != "" {
		header.WriteString("Selected route: " + selection.RouteID + ".\n")
	}
	header.WriteString("Large sources use bounded heading-aware excerpts. Use read with targeted line ranges only when omitted detail is relevant.\n")

	contentBudget := budgetBytes - header.Len() - len(docs)*1024
	if contentBudget < 512 {
		contentBudget = 512
	}
	focus := taskText + " " + selection.RouteID + " " + strings.Join(selection.MatchedSignals, " ")
	compiled := compileManifestInstructionDocs(docs, contentBudget, focus)
	var sb strings.Builder
	sb.WriteString(header.String())
	for _, doc := range compiled {
		promptContent := strings.TrimSpace(doc.PromptContent)
		provenance := projectInstructionProvenance{
			Path:            filepath.ToSlash(doc.Selection.ResolvedPath),
			SHA256:          doc.SHA256,
			Reason:          doc.Selection.Reason,
			SourceBytes:     doc.SourceBytes,
			PromptBytes:     len(promptContent),
			EstimatedTokens: estimateContextTokens(len(promptContent)),
			Partial:         doc.Partial || doc.SourceCut,
			ExcerptRanges:   append([]ProjectInstructionRange(nil), doc.Ranges...),
		}
		packet.Provenance = append(packet.Provenance, provenance)
		sb.WriteString("\n--- Source: " + doc.Selection.DisplayPath)
		if provenance.Partial {
			sb.WriteString(" (bounded excerpt)")
		}
		sb.WriteString(" ---\n")
		sb.WriteString("Selected lines: " + formatInstructionRanges(doc.Ranges) + "\n")
		sb.WriteString(promptContent)
		sb.WriteString("\n")
	}
	packet.Prompt = sb.String()
	if len(packet.Prompt) > budgetBytes {
		return projectInstructionPacket{}, fmt.Errorf("compiled context packet exceeded %d-byte budget", budgetBytes)
	}
	packet.PromptBytes = len(packet.Prompt)
	packet.EstimatedTokens = estimateContextTokens(packet.PromptBytes)
	return packet, nil
}

func readManifestInstructionDoc(selection contextManifestSelectedDocument, maxBytes int) (manifestInstructionDoc, error) {
	file, err := os.Open(selection.ResolvedPath)
	if err != nil {
		return manifestInstructionDoc{}, err
	}
	hasher := sha256.New()
	sourceBytes, err := io.Copy(hasher, file)
	closeErr := file.Close()
	if err != nil {
		return manifestInstructionDoc{}, err
	}
	if closeErr != nil {
		return manifestInstructionDoc{}, closeErr
	}

	readFile, err := os.Open(selection.ResolvedPath)
	if err != nil {
		return manifestInstructionDoc{}, err
	}
	data, err := io.ReadAll(io.LimitReader(readFile, int64(maxBytes)+1))
	closeErr = readFile.Close()
	if err != nil {
		return manifestInstructionDoc{}, err
	}
	if closeErr != nil {
		return manifestInstructionDoc{}, closeErr
	}
	cut := len(data) > maxBytes
	if cut {
		data = data[:maxBytes]
		for len(data) > 0 && !utf8.Valid(data) {
			data = data[:len(data)-1]
		}
	}
	if len(strings.TrimSpace(string(data))) == 0 {
		return manifestInstructionDoc{}, errors.New("selected context document is empty")
	}
	return manifestInstructionDoc{
		Selection:   selection,
		Content:     string(data),
		SHA256:      hex.EncodeToString(hasher.Sum(nil)),
		SourceBytes: int(sourceBytes),
		SourceCut:   cut,
	}, nil
}

func compileManifestInstructionDocs(docs []manifestInstructionDoc, maxBytes int, focus string) []manifestCompiledDoc {
	remaining := maxBytes
	out := make([]manifestCompiledDoc, 0, len(docs))
	for i, doc := range docs {
		if remaining <= 0 {
			break
		}
		share := remaining / (len(docs) - i)
		if share <= 0 {
			break
		}
		compiled := manifestCompiledDoc{manifestInstructionDoc: doc}
		if len(doc.Content) <= share {
			compiled.PromptContent = doc.Content
			compiled.Ranges = []ProjectInstructionRange{{StartLine: 1, EndLine: sourceLineCount(doc.Content)}}
			compiled.Partial = doc.SourceCut
		} else {
			compiled.PromptContent, compiled.Ranges = selectHeadingAwareInstructionExcerpt(doc.Content, share, focus)
			compiled.Partial = true
		}
		if strings.TrimSpace(compiled.PromptContent) == "" {
			continue
		}
		out = append(out, compiled)
		remaining -= len(compiled.PromptContent)
	}
	return out
}

func selectHeadingAwareInstructionExcerpt(content string, maxBytes int, focus string) (string, []ProjectInstructionRange) {
	if maxBytes <= 0 || content == "" {
		return "", nil
	}
	if len(content) <= maxBytes {
		return content, []ProjectInstructionRange{{StartLine: 1, EndLine: sourceLineCount(content)}}
	}
	sections := splitMarkdownInstructionSections(content)
	if len(sections) == 0 {
		text := validUTF8Prefix(content, maxBytes)
		return text, []ProjectInstructionRange{{StartLine: 1, EndLine: sourceLineCount(text)}}
	}
	terms := instructionFocusTerms(focus)
	type rankedSection struct {
		Index int
		Score int
	}
	ranked := make([]rankedSection, 0, len(sections))
	for i, section := range sections {
		ranked = append(ranked, rankedSection{Index: i, Score: scoreInstructionSection(section, terms)})
	}
	sort.SliceStable(ranked, func(i, j int) bool {
		if ranked[i].Score != ranked[j].Score {
			return ranked[i].Score > ranked[j].Score
		}
		return sections[ranked[i].Index].StartLine < sections[ranked[j].Index].StartLine
	})

	marker := "\n\n[... non-selected sections omitted; use targeted read by line range if needed ...]\n\n"
	remaining := maxBytes
	selected := map[int]bool{}
	type chosenSection struct {
		Index   int
		Content string
		Range   ProjectInstructionRange
	}
	var chosen []chosenSection
	add := func(index, allowance int) {
		if selected[index] || allowance <= 0 || remaining <= 0 {
			return
		}
		if len(chosen) > 0 {
			allowance -= len(marker)
			remaining -= len(marker)
		}
		if allowance <= 0 || remaining <= 0 {
			return
		}
		if allowance > remaining {
			allowance = remaining
		}
		section := sections[index]
		text := validUTF8Prefix(section.Content, allowance)
		if strings.TrimSpace(text) == "" {
			return
		}
		endLine := section.StartLine + sourceLineCount(text) - 1
		if endLine > section.EndLine {
			endLine = section.EndLine
		}
		chosen = append(chosen, chosenSection{
			Index:   index,
			Content: text,
			Range:   ProjectInstructionRange{StartLine: section.StartLine, EndLine: endLine},
		})
		selected[index] = true
		remaining -= len(text)
	}

	// Preserve the document identity/preamble without allowing it to consume the task-relevant budget.
	add(0, minInt(640, maxBytes/5))
	for _, candidate := range ranked {
		if len(chosen) >= 4 || remaining <= len(marker)+64 {
			break
		}
		allowance := minInt(len(sections[candidate.Index].Content), remaining)
		add(candidate.Index, allowance)
	}
	if len(chosen) == 0 {
		add(0, maxBytes)
	}
	sort.Slice(chosen, func(i, j int) bool {
		return chosen[i].Range.StartLine < chosen[j].Range.StartLine
	})
	var sb strings.Builder
	ranges := make([]ProjectInstructionRange, 0, len(chosen))
	for i, section := range chosen {
		if i > 0 {
			sb.WriteString(marker)
		}
		sb.WriteString(section.Content)
		ranges = append(ranges, section.Range)
	}
	text := sb.String()
	if len(text) > maxBytes {
		text = validUTF8Prefix(text, maxBytes)
	}
	return text, ranges
}

func splitMarkdownInstructionSections(content string) []markdownInstructionSection {
	lines := strings.SplitAfter(content, "\n")
	if len(lines) == 0 {
		return nil
	}
	start := 1
	var heading string
	var sb strings.Builder
	var sections []markdownInstructionSection
	flush := func(end int) {
		if sb.Len() == 0 {
			return
		}
		sections = append(sections, markdownInstructionSection{
			StartLine: start,
			EndLine:   end,
			Heading:   heading,
			Content:   sb.String(),
		})
		sb.Reset()
	}
	for i, line := range lines {
		lineNo := i + 1
		trimmed := strings.TrimSpace(line)
		isHeading := strings.HasPrefix(trimmed, "#")
		if isHeading && sb.Len() > 0 {
			flush(lineNo - 1)
			start = lineNo
			heading = trimmed
		} else if isHeading {
			heading = trimmed
		}
		sb.WriteString(line)
	}
	flush(len(lines))
	return sections
}

func instructionFocusTerms(focus string) []string {
	stop := map[string]bool{
		"and": true, "the": true, "this": true, "that": true, "with": true, "from": true,
		"into": true, "please": true, "next": true, "update": true, "implement": true,
		"repository": true, "workspace": true, "mauler": true,
	}
	seen := map[string]bool{}
	var terms []string
	for _, field := range strings.FieldsFunc(strings.ToLower(focus), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '_' && r != '-'
	}) {
		field = strings.Trim(field, "_-")
		if len(field) < 3 || stop[field] || seen[field] {
			continue
		}
		seen[field] = true
		terms = append(terms, field)
	}
	return terms
}

func scoreInstructionSection(section markdownInstructionSection, terms []string) int {
	heading := strings.ToLower(section.Heading)
	body := strings.ToLower(section.Content)
	score := 0
	for _, term := range terms {
		if strings.Contains(heading, term) {
			score += 30
		}
		if count := strings.Count(body, term); count > 0 {
			if count > 4 {
				count = 4
			}
			score += count * 3
		}
	}
	return score
}

func validUTF8Prefix(content string, maxBytes int) string {
	if maxBytes <= 0 {
		return ""
	}
	if len(content) <= maxBytes {
		return content
	}
	end := maxBytes
	for end > 0 && !utf8.ValidString(content[:end]) {
		end--
	}
	return content[:end]
}

func sourceLineCount(content string) int {
	if content == "" {
		return 0
	}
	count := strings.Count(content, "\n") + 1
	if strings.HasSuffix(content, "\n") {
		count--
	}
	if count < 1 {
		return 1
	}
	return count
}

func formatInstructionRanges(ranges []ProjectInstructionRange) string {
	parts := make([]string, 0, len(ranges))
	for _, excerpt := range ranges {
		if excerpt.StartLine == excerpt.EndLine {
			parts = append(parts, fmt.Sprintf("%d", excerpt.StartLine))
		} else {
			parts = append(parts, fmt.Sprintf("%d-%d", excerpt.StartLine, excerpt.EndLine))
		}
	}
	return strings.Join(parts, ", ")
}

func (packet projectInstructionPacket) ledgerDetail() string {
	detail, err := json.MarshalIndent(packet, "", "  ")
	if err != nil {
		return fmt.Sprintf("policy=%s\nsource_bytes=%d\nprompt_bytes=%d", packet.Policy, packet.SourceBytes, packet.PromptBytes)
	}
	return string(detail)
}

func estimateContextTokens(bytes int) int {
	if bytes <= 0 {
		return 0
	}
	return (bytes + 3) / 4
}

func sha256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
