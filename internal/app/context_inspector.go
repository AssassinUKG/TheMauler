package app

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"mauler/internal/llm"
	"mauler/internal/settings"
	"mauler/internal/tools"
)

type ContextInspection struct {
	GeneratedAt          string                       `json:"generated_at"`
	TaskText             string                       `json:"task_text"`
	RequestedClass       string                       `json:"requested_class"`
	EffectiveClass       string                       `json:"effective_class"`
	PinnedNextClass      string                       `json:"pinned_next_class,omitempty"`
	Policy               string                       `json:"policy"`
	RouteID              string                       `json:"route_id,omitempty"`
	ProfileName          string                       `json:"profile_name"`
	ModelID              string                       `json:"model_id"`
	AgentMode            string                       `json:"agent_mode"`
	ToolChoice           string                       `json:"tool_choice"`
	ToolCount            int                          `json:"tool_count"`
	ToolNames            []string                     `json:"tool_names"`
	ToolSchemaSHA256     string                       `json:"tool_schema_sha256,omitempty"`
	PacketSHA256         string                       `json:"packet_sha256,omitempty"`
	ContextWindowTokens  int                          `json:"context_window_tokens"`
	WorkingContextTokens int                          `json:"working_context_tokens"`
	OutputReserveTokens  int                          `json:"output_reserve_tokens"`
	ModelMaxOutputTokens int                          `json:"model_max_output_tokens"`
	PacketLimitTokens    int                          `json:"packet_limit_tokens"`
	PacketLimitBytes     int                          `json:"packet_limit_bytes"`
	ManifestStatus       string                       `json:"manifest_status"`
	ManifestPath         string                       `json:"manifest_path,omitempty"`
	ManifestSHA256       string                       `json:"manifest_sha256,omitempty"`
	FallbackReason       string                       `json:"fallback_reason,omitempty"`
	Budget               ContextInspectionBudget      `json:"budget"`
	Sources              []ContextInspectionSource    `json:"sources"`
	ExcludedSources      []ContextInspectionExclusion `json:"excluded_sources"`
	Warnings             []string                     `json:"warnings"`
	Synopsis             string                       `json:"synopsis"`
}

type ContextInspectionBudget struct {
	CoreSystemTokens       int     `json:"core_system_tokens"`
	ProjectDocumentTokens  int     `json:"project_document_tokens"`
	ToolSchemaTokens       int     `json:"tool_schema_tokens"`
	MemoryProgressTokens   int     `json:"memory_progress_tokens"`
	SkillTokens            int     `json:"skill_tokens"`
	UserProfileTokens      int     `json:"user_profile_tokens"`
	ConversationTokens     int     `json:"conversation_tokens"`
	UserTaskTokens         int     `json:"user_task_tokens"`
	TotalPreflightTokens   int     `json:"total_preflight_tokens"`
	RemainingWorkingTokens int     `json:"remaining_working_tokens"`
	UsagePercent           float64 `json:"usage_percent"`
}

type ContextInspectionSource struct {
	Path            string                    `json:"path"`
	DisplayPath     string                    `json:"display_path"`
	SHA256          string                    `json:"sha256"`
	Reason          string                    `json:"reason"`
	Trust           string                    `json:"trust"`
	SourceBytes     int                       `json:"source_bytes"`
	PromptBytes     int                       `json:"prompt_bytes"`
	EstimatedTokens int                       `json:"estimated_tokens"`
	Partial         bool                      `json:"partial"`
	ExcerptRanges   []ProjectInstructionRange `json:"excerpt_ranges"`
}

type ContextInspectionExclusion struct {
	Path        string `json:"path"`
	DisplayPath string `json:"display_path"`
	SourceBytes int64  `json:"source_bytes"`
	Reason      string `json:"reason"`
	Large       bool   `json:"large"`
}

func (a *App) PreviewContext(taskText, requestedClass string) (ContextInspection, error) {
	if a == nil {
		return ContextInspection{}, fmt.Errorf("app is not ready")
	}
	a.mu.Lock()
	if a.cfg == nil || a.profiles == nil || a.history == nil {
		a.mu.Unlock()
		return ContextInspection{}, fmt.Errorf("context inspector is not ready")
	}
	cfg := *a.cfg
	cloneToolsConfigRefs(&cfg.Tools)
	profiles := *a.profiles
	autonomous := a.autonomous
	autoAgents := a.autoAgents
	pinned := a.nextContextPacketClass
	historyMessages := a.history.Messages()
	historyTokens := a.history.TokenCount()
	a.mu.Unlock()

	requestedClass = normalizeContextPacketClass(requestedClass)
	taskText = strings.TrimSpace(taskText)
	profile := activeProfile(&cfg, &profiles)
	profileName := cfg.ActiveProfile
	mode := selectAgentMode(taskText, cfg)
	if !autoAgents {
		mode = manualAgentMode()
	}
	beforeModel := profile.ModelID
	applyAgentPreset(&cfg, &profiles, mode, &profile, &autonomous)
	if profile.ModelID != beforeModel && strings.TrimSpace(profile.Name) != "" {
		profileName = profile.Name
	}

	memorySelection := previewRelevantMemory(cfg, taskText)
	if a.db != nil {
		if results, err := a.SearchSessionRecall(taskText, 2); err == nil {
			memorySelection.AddSessionRecall(results)
		}
	}
	if a.ledger != nil {
		if events, err := a.ListLedgerEvents(300); err == nil {
			memorySelection.AddEvidencePointers(events, taskText)
		}
	}
	memories := memorySelection.Entries
	skills := relevantSkillsForSettings(cfg.Skills, cfg, taskText)
	packet := buildProjectInstructionPacketForClass(cfg.Context, taskText, requestedClass)

	skillsPrompt := buildRelevantSkillsPrompt(skills)
	memoryPrompt := buildSelectedMemoryPrompt(memories)
	runFactsPrompt := buildRunFactsPromptFromLedger()
	progressPrompt := buildProgressArtifactPrompt()
	resumePrompt := buildProjectResumePrompt(cfg)
	userProfilePrompt := buildUserProfilePrompt()
	memoryProgressPrompt := memoryPrompt + runFactsPrompt + progressPrompt + resumePrompt
	fullSystemPrompt := buildSystemPromptForTaskWithProjectInstructions(cfg, mode, memories, skills, taskText, packet.Prompt)

	toolChoice := "none"
	var toolDefs []llm.ToolDef
	if a.registry != nil {
		toolDefs, toolChoice = toolDefsAndChoiceForTurn(a.registry, cfg.Tools, taskText, 0, 0)
		toolDefs, toolChoice, _ = applyActiveBrowserTurnRouting(a.registry, cfg.Tools, taskText, toolDefs, toolChoice, tools.GetBrowserWorkflowStatus(a.browserWorkflowOwnerID()))
	}
	toolSchemaBytes := 0
	if data, err := json.Marshal(toolDefs); err == nil {
		toolSchemaBytes = len(data)
	}
	preflightMessages := messagesWithPrimarySystemPrompt(historyMessages, fullSystemPrompt)
	preflightMessages = append(preflightMessages, llm.NewTextMessage(llm.RoleUser, taskText))
	totalPreflight := estimateChatPromptTokens(preflightMessages, toolDefs)

	dynamicBytes := len(packet.Prompt) + len(skillsPrompt) + len(memoryProgressPrompt) + len(userProfilePrompt)
	coreSystemBytes := len(fullSystemPrompt) - dynamicBytes
	if coreSystemBytes < 0 {
		coreSystemBytes = 0
	}
	contextWindow := profile.CtxTokens
	workingContext := effectiveWorkingContextBudget(mode.ContextBudget, contextWindow)
	if workingContext <= 0 {
		workingContext = contextWindow
	}
	outputReserve := contextWindow - workingContext
	if outputReserve < 0 {
		outputReserve = 0
	}
	remaining := workingContext - totalPreflight
	if remaining < 0 {
		remaining = 0
	}
	usage := 0.0
	if workingContext > 0 {
		usage = float64(totalPreflight) * 100 / float64(workingContext)
	}
	modelMaxOutput := profile.ActiveParams(shouldUseCodingParams(taskText, mode)).MaxTokens
	root, _ := os.Getwd()

	inspection := ContextInspection{
		GeneratedAt:          time.Now().Format(time.RFC3339),
		TaskText:             taskText,
		RequestedClass:       requestedClass,
		EffectiveClass:       packet.EffectiveClass,
		PinnedNextClass:      strings.TrimSpace(pinned),
		Policy:               packet.Policy,
		RouteID:              packet.RouteID,
		ProfileName:          profileName,
		ModelID:              profile.ModelID,
		AgentMode:            mode.Name,
		ToolChoice:           toolChoice,
		ToolCount:            len(toolDefs),
		ToolNames:            toolNamesFromDefs(toolDefs),
		ToolSchemaSHA256:     hashToolDefs(toolDefs),
		PacketSHA256:         sha256Hex([]byte(packet.Prompt)),
		ContextWindowTokens:  contextWindow,
		WorkingContextTokens: workingContext,
		OutputReserveTokens:  outputReserve,
		ModelMaxOutputTokens: modelMaxOutput,
		PacketLimitTokens:    packet.PacketLimitTokens,
		PacketLimitBytes:     packet.PacketLimitBytes,
		ManifestStatus:       packet.ManifestStatus,
		ManifestPath:         packet.ManifestPath,
		ManifestSHA256:       packet.ManifestSHA256,
		FallbackReason:       packet.FallbackReason,
		Budget: ContextInspectionBudget{
			CoreSystemTokens:       estimateContextTokens(coreSystemBytes),
			ProjectDocumentTokens:  packet.EstimatedTokens,
			ToolSchemaTokens:       (toolSchemaBytes + 2) / 3,
			MemoryProgressTokens:   estimateContextTokens(len(memoryProgressPrompt)),
			SkillTokens:            estimateContextTokens(len(skillsPrompt)),
			UserProfileTokens:      estimateContextTokens(len(userProfilePrompt)),
			ConversationTokens:     historyTokens,
			UserTaskTokens:         estimateContextTokens(len(taskText)),
			TotalPreflightTokens:   totalPreflight,
			RemainingWorkingTokens: remaining,
			UsagePercent:           usage,
		},
	}
	for _, source := range packet.Provenance {
		display := source.Path
		if relative, err := filepath.Rel(root, filepath.FromSlash(source.Path)); err == nil && !strings.HasPrefix(relative, "..") {
			display = filepath.ToSlash(relative)
		}
		inspection.Sources = append(inspection.Sources, ContextInspectionSource{
			Path:            source.Path,
			DisplayPath:     display,
			SHA256:          source.SHA256,
			Reason:          source.Reason,
			Trust:           "trusted project instruction",
			SourceBytes:     source.SourceBytes,
			PromptBytes:     source.PromptBytes,
			EstimatedTokens: source.EstimatedTokens,
			Partial:         source.Partial,
			ExcerptRanges:   append([]ProjectInstructionRange(nil), source.ExcerptRanges...),
		})
	}
	inspection.ExcludedSources = contextInspectionExclusions(root, packet)
	inspection.Warnings = contextInspectionWarnings(inspection)
	inspection.Synopsis = contextInspectionSynopsis(inspection)
	return inspection, nil
}

func (a *App) SetNextContextPacketClass(requestedClass string) (string, error) {
	if a == nil {
		return "", fmt.Errorf("app is not ready")
	}
	raw := strings.ToLower(strings.TrimSpace(requestedClass))
	if raw == "" || raw == "auto" {
		a.mu.Lock()
		a.nextContextPacketClass = ""
		a.mu.Unlock()
		if a.ctx != nil {
			a.emit("mauler:context_packet_pin", "")
		}
		return "", nil
	}
	if raw != "core" && raw != "relevant" && raw != "expanded" {
		return "", fmt.Errorf("context packet class must be core, relevant, expanded, or auto")
	}
	a.mu.Lock()
	a.nextContextPacketClass = raw
	a.mu.Unlock()
	if a.ctx != nil {
		a.emit("mauler:context_packet_pin", raw)
	}
	return raw, nil
}

func (a *App) GetNextContextPacketClass() string {
	if a == nil {
		return ""
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.nextContextPacketClass
}

func (a *App) consumeNextContextPacketClass(origin string) string {
	if a == nil || !strings.EqualFold(strings.TrimSpace(origin), "desktop") {
		return ""
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	packetClass := normalizeContextPacketClass(a.nextContextPacketClass)
	a.nextContextPacketClass = ""
	if packetClass == "auto" {
		return ""
	}
	return packetClass
}

func previewRelevantMemory(cfg settings.Settings, prompt string) memorySelection {
	if !cfg.Memory.Enabled || !cfg.Memory.AutoInject {
		return memorySelection{}
	}
	entries, err := loadMemory()
	if err != nil || len(entries) == 0 {
		return memorySelection{}
	}
	filtered, withheld, conflicts := filterMemoryInjectionConflicts(entries, cfg, prompt)
	limit := cfg.Memory.MaxInject
	if limit <= 0 {
		limit = 8
	}
	plan := planMemoryRetrieval(filtered, prompt, limit)
	return memorySelection{
		Entries:   planEntries(filtered, plan),
		Withheld:  withheld,
		Conflicts: conflicts,
		Plan:      plan,
	}
}

func messagesWithPrimarySystemPrompt(messages []llm.Message, prompt string) []llm.Message {
	updated := make([]llm.Message, len(messages))
	copy(updated, messages)
	replacement := llm.NewTextMessage(llm.RoleSystem, prompt)
	for i, message := range updated {
		if message.Role == llm.RoleSystem && strings.HasPrefix(strings.TrimSpace(messageText(message)), "You are TheMauler") {
			updated[i] = replacement
			return updated
		}
	}
	return append([]llm.Message{replacement}, updated...)
}

func contextInspectionExclusions(root string, packet projectInstructionPacket) []ContextInspectionExclusion {
	selected := map[string]bool{}
	for _, source := range packet.Sources {
		selected[strings.ToLower(filepath.Clean(filepath.FromSlash(source)))] = true
	}
	add := func(out *[]ContextInspectionExclusion, path, reason string) {
		info, err := os.Stat(path)
		if err != nil || !info.Mode().IsRegular() {
			return
		}
		clean := strings.ToLower(filepath.Clean(path))
		if selected[clean] {
			return
		}
		display, err := filepath.Rel(root, path)
		if err != nil {
			display = path
		}
		*out = append(*out, ContextInspectionExclusion{
			Path:        filepath.ToSlash(path),
			DisplayPath: filepath.ToSlash(display),
			SourceBytes: info.Size(),
			Reason:      reason,
			Large:       info.Size() >= 8*1024,
		})
	}
	var out []ContextInspectionExclusion
	contextDir := filepath.Join(root, "docs", "context")
	if entries, err := os.ReadDir(contextDir); err == nil {
		for _, entry := range entries {
			if entry.IsDir() || !strings.EqualFold(filepath.Ext(entry.Name()), ".md") {
				continue
			}
			reason := "not selected by the current manifest route"
			if packet.Policy == "minimal_external_research" {
				reason = "excluded by minimal external-research policy"
			}
			add(&out, filepath.Join(contextDir, entry.Name()), reason)
		}
	}
	add(&out, filepath.Join(root, "MAULER.md"), "compatibility shim; canonical context comes from AGENTS.md and the manifest")
	archiveDir := filepath.Join(root, "docs", "archive")
	if entries, err := os.ReadDir(archiveDir); err == nil {
		for _, entry := range entries {
			name := strings.ToLower(entry.Name())
			if entry.IsDir() || !strings.HasSuffix(name, ".md") || (!strings.Contains(name, "agents-handoff") && !strings.Contains(name, "mauler-legacy")) {
				continue
			}
			add(&out, filepath.Join(archiveDir, entry.Name()), "historical archive; never automatically injected")
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Large != out[j].Large {
			return out[i].Large
		}
		return strings.ToLower(out[i].DisplayPath) < strings.ToLower(out[j].DisplayPath)
	})
	return out
}

func contextInspectionWarnings(inspection ContextInspection) []string {
	var warnings []string
	if inspection.ManifestStatus == contextManifestStatusInvalid {
		warnings = append(warnings, "The manifest is invalid; Mauler will use the deterministic compact-core fallback.")
	}
	if inspection.Budget.UsagePercent >= 85 {
		warnings = append(warnings, "Estimated preflight context is above 85% of the working budget; compaction or a smaller packet may be required.")
	} else if inspection.Budget.UsagePercent >= 70 {
		warnings = append(warnings, "Estimated preflight context is above 70% of the working budget.")
	}
	for _, source := range inspection.Sources {
		if source.Partial {
			warnings = append(warnings, "One or more sources use bounded excerpts; the complete files remain available through targeted reads.")
			break
		}
	}
	if inspection.RequestedClass == "expanded" {
		warnings = append(warnings, "Expanded applies only when explicitly pinned for the next desktop task; it never becomes the persistent default.")
	}
	return warnings
}

func contextInspectionSynopsis(inspection ContextInspection) string {
	route := firstNonEmpty(inspection.RouteID, "default packet")
	return fmt.Sprintf(
		"%s context selected by %s: %d trusted source(s), about %d project tokens and %d total preflight tokens within a %d-token working budget. %d working token(s) remain; the response reserve is separate.",
		strings.Title(inspection.EffectiveClass), route, len(inspection.Sources), inspection.Budget.ProjectDocumentTokens,
		inspection.Budget.TotalPreflightTokens, inspection.WorkingContextTokens, inspection.Budget.RemainingWorkingTokens,
	)
}
