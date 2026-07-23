// Package engagement owns TheMauler's native, project-scoped engagement grid.
//
// The package deliberately contains no HTTP server or Python bridge. Workflow
// and checklist packs are JSON inputs; validation and runtime state transitions
// remain Go-native so the desktop UI and agent tool can share one service.
package engagement
