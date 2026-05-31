package agent

import (
	"fmt"
	"os"
)

const maxRollbackDepth = 20

// OpKind identifies what a rollback operation reverses.
type OpKind string

const (
	OpWrite OpKind = "write" // file was created or overwritten
	OpEdit  OpKind = "edit"  // file was edited in-place
)

// Op is a single reversible file mutation.
type Op struct {
	Kind       OpKind
	Path       string
	Before     []byte      // nil if the file didn't exist before
	PermBefore os.FileMode // original permissions; 0 if file was new
}

// Rollback is a stack of reversible operations.
type Rollback struct {
	stack []Op
}

// Push records a file state before it is mutated.
// Call this before writing or editing a file.
func (r *Rollback) Push(kind OpKind, path string) error {
	info, statErr := os.Stat(path)
	if os.IsNotExist(statErr) {
		// File didn't exist — undo means deleting it
		r.stack = append(r.stack, Op{Kind: kind, Path: path, Before: nil, PermBefore: 0})
		return nil
	}
	if statErr != nil {
		return fmt.Errorf("rollback stat %s: %w", path, statErr)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("rollback snapshot %s: %w", path, err)
	}
	r.stack = append(r.stack, Op{Kind: kind, Path: path, Before: data, PermBefore: info.Mode()})
	// Cap depth
	if len(r.stack) > maxRollbackDepth {
		r.stack = r.stack[len(r.stack)-maxRollbackDepth:]
	}
	return nil
}

// Pop undoes the most recent operation and returns a description.
// Returns ("", false) if the stack is empty.
func (r *Rollback) Pop() (string, bool) {
	if len(r.stack) == 0 {
		return "", false
	}
	op := r.stack[len(r.stack)-1]
	r.stack = r.stack[:len(r.stack)-1]

	if op.Before == nil {
		// File was created — delete it
		if err := os.Remove(op.Path); err != nil && !os.IsNotExist(err) {
			return fmt.Sprintf("undo failed: remove %s: %v", op.Path, err), true
		}
		return fmt.Sprintf("undone: deleted %s (was newly created)", op.Path), true
	}

	perm := op.PermBefore
	if perm == 0 {
		perm = 0o644
	}
	if err := os.WriteFile(op.Path, op.Before, perm); err != nil {
		return fmt.Sprintf("undo failed: restore %s: %v", op.Path, err), true
	}
	return fmt.Sprintf("undone: restored %s to previous state", op.Path), true
}

// Len returns the number of operations on the stack.
func (r *Rollback) Len() int { return len(r.stack) }

// Clear empties the rollback stack.
func (r *Rollback) Clear() { r.stack = nil }
