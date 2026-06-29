package app

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"mauler/internal/runtimeprofile"
	"mauler/internal/settings"
)

// SpecSource records why MTP/speculative decoding ended up enabled or disabled,
// so the UI badge and Doctor can explain the decision without re-deriving it.
type SpecSource string

const (
	SpecSourceProbe    SpecSource = "probe"    // GGUF header inspection confirmed (or denied) MTP heads
	SpecSourceName     SpecSource = "name"     // fell back to the model name (no probe available)
	SpecSourceRegistry SpecSource = "registry" // family capability record only
	SpecSourceManual   SpecSource = "manual"   // user forced on/off — auto layer leaves it alone
	SpecSourceDisabled SpecSource = "disabled" // no MTP-capable family matched
	SpecSourceGuard    SpecSource = "guard"    // stability guard auto-disabled it this session
)

// SpecPlan is the resolved speculative-decoding decision for the active profile.
// It is what GetSpecPlan returns to the UI and what autoApplySpec writes onto the
// stored profile (SpecType/SpecDraftNMax).
type SpecPlan struct {
	Enabled  bool       `json:"enabled"`
	SpecType string     `json:"spec_type"`      // "draft-mtp" when enabled, "" otherwise
	NMax     int        `json:"n_max"`          // --spec-draft-n-max
	Source   SpecSource `json:"source"`
	Reason   string     `json:"reason"`         // human-readable, shown in badge tooltip + logs
	Locked   bool       `json:"locked"`         // true when the user pinned the choice
	ModelID  string     `json:"model_id"`
}

// defaultSpecNMax is the conservative starting draft window. Unsloth/llama.cpp
// note the optimum is hardware-dependent (try 1–6); Phase 2 calibration tunes it.
const defaultSpecNMax = 2

// specTypeMTP is llama.cpp's accepted --spec-type value for self-MTP heads.
const specTypeMTP = "draft-mtp"

// ResolveSpecPlan is the pure decision function. Inputs:
//   - p           the active (provider-resolved) profile
//   - rp/rpOK     the matched runtime-profile capability record (rpOK=false if none)
//   - info        the GGUF/bridge probe result (info.Confident=false ⇒ fall back)
//   - override    "on" | "off" to honor a user lock; anything else ⇒ auto
//   - guardReason non-empty when the stability guard has tripped this session;
//     it disables MTP unless the user explicitly overrides
//
// It never performs I/O so it is fully unit-testable.
func ResolveSpecPlan(p settings.Profile, rp runtimeprofile.RuntimeProfile, rpOK bool, info runtimeprofile.ModelMTPInfo, override, guardReason string) SpecPlan {
	nMax := p.SpecDraftNMax
	if nMax <= 0 {
		nMax = defaultSpecNMax
	}
	plan := SpecPlan{NMax: nMax, ModelID: strings.TrimSpace(p.ModelID)}

	switch strings.ToLower(strings.TrimSpace(override)) {
	case "on":
		plan.Enabled = true
		plan.SpecType = specTypeMTP
		plan.Source = SpecSourceManual
		plan.Locked = true
		plan.Reason = "Forced on by user — auto-tuning left alone"
		return plan
	case "off":
		plan.Enabled = false
		plan.SpecType = ""
		plan.NMax = 0
		plan.Source = SpecSourceManual
		plan.Locked = true
		plan.Reason = "Forced off by user"
		return plan
	}

	// Stability guard wins over auto-detection (but not over an explicit user
	// override above). It stays off until the user re-enables via SetSpecMode.
	if strings.TrimSpace(guardReason) != "" {
		plan.Enabled = false
		plan.SpecType = ""
		plan.NMax = 0
		plan.Source = SpecSourceGuard
		plan.Reason = guardReason
		return plan
	}

	enable := func(src SpecSource, reason string) SpecPlan {
		plan.Enabled = true
		plan.SpecType = specTypeMTP
		plan.Source = src
		plan.Reason = reason
		return plan
	}
	disable := func(src SpecSource, reason string) SpecPlan {
		plan.Enabled = false
		plan.SpecType = ""
		plan.NMax = 0
		plan.Source = src
		plan.Reason = reason
		return plan
	}

	// A confident probe is authoritative about the artifact itself — it overrides
	// both the registry and the filename, in either direction.
	if info.Confident {
		if info.HasMTPHeads {
			return enable(SpecSourceProbe, "MTP heads confirmed in the model artifact")
		}
		return disable(SpecSourceProbe, "Model artifact has no MTP heads — "+detailOr(info.Detail, "nothing to self-draft"))
	}

	// No confident probe. Use the family capability + the name as a weaker signal.
	if rpOK && !rp.Supports.MTP {
		return disable(SpecSourceRegistry, fmt.Sprintf("%s family has no MTP variant", rp.Family))
	}
	if runtimeprofile.LooksMTPModel(p) {
		return enable(SpecSourceName, "Model name looks MTP-capable (no probe; verify by loading or calibrating)")
	}
	if rpOK {
		return disable(SpecSourceName, fmt.Sprintf("%s supports MTP but this artifact's name has no MTP marker", rp.Name))
	}
	return disable(SpecSourceDisabled, "No MTP-capable model family matched")
}

func detailOr(detail, fallback string) string {
	if strings.TrimSpace(detail) != "" {
		return detail
	}
	return fallback
}

// GetSpecPlan returns the last resolved speculative-decoding plan for the UI.
func (a *App) GetSpecPlan() SpecPlan {
	a.specMu.Lock()
	defer a.specMu.Unlock()
	return a.specPlan
}

// SetSpecMode pins the speculative-decoding decision. mode is "auto" (let the
// detector decide), "on", or "off". It re-resolves and persists immediately.
func (a *App) SetSpecMode(mode string) (SpecPlan, error) {
	mode = strings.ToLower(strings.TrimSpace(mode))
	if mode != "auto" && mode != "on" && mode != "off" {
		return a.GetSpecPlan(), fmt.Errorf("invalid spec mode %q (want auto|on|off)", mode)
	}
	a.mu.Lock()
	name := a.cfg.ActiveProfile
	p, ok := a.profiles.Profiles[name]
	a.mu.Unlock()
	if !ok {
		return a.GetSpecPlan(), fmt.Errorf("no active profile")
	}
	a.specMu.Lock()
	if a.specOverrides == nil {
		a.specOverrides = map[string]string{}
	}
	if mode == "auto" {
		delete(a.specOverrides, name)
	} else {
		a.specOverrides[name] = mode
	}
	// An explicit user choice clears any stability-guard trip: the user is taking
	// control, so the guard should not keep overriding them.
	delete(a.specGuard, name)
	a.specTruncStreak = 0
	a.specMu.Unlock()
	return a.autoApplySpec(name, applyProvider(p, a.profiles)), nil
}

// autoApplySpec resolves the spec plan for the named profile, writes the result
// onto the stored profile (SpecType/SpecDraftNMax), persists, records it, and
// emits "mauler:spec_plan" for the UI. It is best-effort: probe failures degrade
// to weaker signals rather than erroring. Must be called WITHOUT a.mu held.
func (a *App) autoApplySpec(name string, active settings.Profile) SpecPlan {
	rp, rpOK := runtimeprofile.Match(active)
	info := a.probeModelMTP(active)

	a.specMu.Lock()
	override := a.specOverrides[name]
	guard := a.specGuard[name]
	a.specMu.Unlock()

	plan := ResolveSpecPlan(active, rp, rpOK, info, override, guard)

	// Apply a cached n-max calibration (Phase 2) so every load uses the fastest
	// draft window measured for this (model, ctx) on this machine.
	if plan.Enabled {
		if cal, ok := a.getSpecCalibration(active); ok && cal.BestN > 0 {
			plan.NMax = cal.BestN
			if cal.Speedup > 0 {
				plan.Reason += fmt.Sprintf(" · tuned n=%d (%.2f× vs off)", cal.BestN, cal.Speedup)
			}
		}
	}

	// Persist the decision onto the stored profile so buildChatRequest / the load
	// body pick it up. Only write when something actually changed.
	a.mu.Lock()
	if stored, ok := a.profiles.Profiles[name]; ok {
		if stored.SpecType != plan.SpecType || stored.SpecDraftNMax != specNMaxFor(plan) {
			stored.SpecType = plan.SpecType
			stored.SpecDraftNMax = specNMaxFor(plan)
			a.profiles.Profiles[name] = stored
			if a.cfg.ActiveProfile == name {
				// Force a reload so the new spec flags reach the bridge.
				a.loadedModelKey = ""
			}
			_ = settings.SaveProfiles(a.profiles)
		}
	}
	a.mu.Unlock()

	a.specMu.Lock()
	a.specPlan = plan
	a.specMu.Unlock()

	a.emit("mauler:spec_plan", plan)
	return plan
}

// specNMaxFor returns the n-max to persist: the plan's value when enabled, else 0.
func specNMaxFor(plan SpecPlan) int {
	if !plan.Enabled {
		return 0
	}
	if plan.NMax <= 0 {
		return defaultSpecNMax
	}
	return plan.NMax
}

// probeModelMTP asks the bridge for the active model's local GGUF path, then
// inspects that file's header for MTP heads. Any failure returns a non-confident
// result so ResolveSpecPlan falls back to name/registry signals.
func (a *App) probeModelMTP(active settings.Profile) runtimeprofile.ModelMTPInfo {
	if !strings.EqualFold(strings.TrimSpace(active.Backend), "llamacpp") {
		// Spec flags only mean anything for the managed llama.cpp bridge.
		return runtimeprofile.ModelMTPInfo{Detail: "active backend is not llama.cpp; skipping probe"}
	}
	path := a.bridgeModelPath(active)
	if path == "" {
		return runtimeprofile.ModelMTPInfo{Detail: "bridge did not report a local model path"}
	}
	return runtimeprofile.ProbeGGUFForMTP(path)
}

// bridgeModelPath fetches GET {baseURL}/models/{id} and returns the local GGUF
// path the bridge reports, or "" on any error.
func (a *App) bridgeModelPath(active settings.Profile) string {
	baseURL := strings.TrimRight(strings.TrimSpace(active.BaseURL), "/")
	if baseURL == "" {
		baseURL = "http://localhost:8080/v1"
	}
	modelID := strings.TrimSpace(active.ModelID)
	if modelID == "" {
		return ""
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2500*time.Millisecond)
	defer cancel()
	url := baseURL + "/models/" + escapePathSegment(modelID)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return ""
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return ""
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return ""
	}
	var detail struct {
		Path string `json:"path"`
	}
	if err := json.Unmarshal(body, &detail); err != nil {
		return ""
	}
	return strings.TrimSpace(detail.Path)
}

// specGuardThreshold is how many MTP-suspect truncations in a row trip the
// stability guard. Two is conservative: a single early-stop can be benign, but a
// repeat while MTP is on is the documented speculative-rejection-at-</think>
// signature and warrants auto-falling back to a stable decode.
const specGuardThreshold = 2

// noteSpecTurn records the outcome of one model turn that ran with MTP enabled.
// suspect=true means the turn truncated with thinking on but no usable answer —
// the speculative-EOS-spike pattern. A clean turn resets the streak. When the
// streak reaches the threshold the stability guard trips and MTP auto-disables.
// Safe to call without a.mu held.
func (a *App) noteSpecTurn(profileName string, suspect bool) {
	profileName = strings.TrimSpace(profileName)
	if profileName == "" {
		return
	}
	a.specMu.Lock()
	if !suspect {
		a.specTruncStreak = 0
		a.specMu.Unlock()
		return
	}
	a.specTruncStreak++
	streak := a.specTruncStreak
	alreadyGuarded := a.specGuard[profileName] != ""
	a.specMu.Unlock()

	if streak < specGuardThreshold || alreadyGuarded {
		return
	}
	a.tripSpecGuard(profileName, fmt.Sprintf(
		"Auto-disabled after %d truncated thinking turns — likely speculative-decoding instability. Re-enable from the MTP chip to retry.",
		streak))
}

// tripSpecGuard records a stability-guard trip for the profile and re-resolves
// the plan so MTP turns off, persists, and notifies the UI. The change applies at
// the next model load (we deliberately avoid a disruptive mid-run reload).
func (a *App) tripSpecGuard(profileName, reason string) {
	a.specMu.Lock()
	if a.specGuard == nil {
		a.specGuard = map[string]string{}
	}
	a.specGuard[profileName] = reason
	a.specTruncStreak = 0
	a.specMu.Unlock()

	a.mu.Lock()
	p, ok := a.profiles.Profiles[profileName]
	a.mu.Unlock()
	if !ok {
		return
	}
	a.autoApplySpec(profileName, applyProvider(p, a.profiles))
}

// escapePathSegment encodes a model id for use as a single URL path segment
// without dragging in net/url for the common (already-safe) case.
func escapePathSegment(s string) string {
	if !strings.ContainsAny(s, " #?%/") {
		return s
	}
	var b strings.Builder
	for _, r := range []byte(s) {
		switch r {
		case ' ':
			b.WriteString("%20")
		case '#':
			b.WriteString("%23")
		case '?':
			b.WriteString("%3F")
		case '%':
			b.WriteString("%25")
		case '/':
			b.WriteString("%2F")
		default:
			b.WriteByte(r)
		}
	}
	return b.String()
}
