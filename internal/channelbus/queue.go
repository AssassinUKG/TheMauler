package channelbus

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"sync"
	"time"
)

type Queue struct {
	mu    sync.Mutex
	items []WorkItem
	db    *sql.DB
}

func NewQueue() *Queue {
	return &Queue{}
}

func NewPersistentQueue(db *sql.DB) *Queue {
	return &Queue{db: db}
}

func (q *Queue) Enqueue(env Envelope, route Route) WorkItem {
	if q == nil {
		return WorkItem{}
	}
	env = NormalizeEnvelope(env)
	now := time.Now()
	createdAt := now.Format(time.RFC3339Nano)
	item := WorkItem{
		ID:        "work-" + now.Format("20060102-150405.000000000"),
		Envelope:  env,
		Route:     route,
		Status:    "queued",
		CreatedAt: createdAt,
	}
	if q.db != nil {
		if err := saveWorkItemDB(q.db, item); err == nil {
			return item
		}
	}
	q.mu.Lock()
	q.items = append(q.items, item)
	q.mu.Unlock()
	return item
}

func (q *Queue) List() []WorkItem {
	if q == nil {
		return nil
	}
	if q.db != nil {
		items, err := listWorkItemsDB(q.db)
		if err == nil {
			return items
		}
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	out := make([]WorkItem, len(q.items))
	copy(out, q.items)
	return out
}

func (q *Queue) ListActive() []WorkItem {
	items := q.List()
	out := make([]WorkItem, 0, len(items))
	for _, item := range items {
		if item.Status == "queued" || item.Status == "dispatching" {
			out = append(out, item)
		}
	}
	return out
}

func (q *Queue) PopNext() (WorkItem, bool) {
	if q == nil {
		return WorkItem{}, false
	}
	if q.db != nil {
		return popNextWorkItemDB(q.db)
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	for i, item := range q.items {
		if item.Status != "queued" {
			continue
		}
		item.Status = "dispatching"
		q.items[i] = item
		return item, true
	}
	return WorkItem{}, false
}

func (q *Queue) Mark(id, status string) {
	if q == nil {
		return
	}
	if q.db != nil {
		_ = markWorkItemDB(q.db, id, status)
		return
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	for i := range q.items {
		if q.items[i].ID == id {
			q.items[i].Status = status
			return
		}
	}
}

func (q *Queue) CancelActive() int {
	if q == nil {
		return 0
	}
	if q.db != nil {
		return cancelActiveWorkItemsDB(q.db)
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	cancelled := 0
	for i := range q.items {
		if q.items[i].Status == "queued" || q.items[i].Status == "dispatching" {
			q.items[i].Status = "cancelled"
			cancelled++
		}
	}
	return cancelled
}

func saveWorkItemDB(db *sql.DB, item WorkItem) error {
	envJSON, err := json.Marshal(item.Envelope)
	if err != nil {
		return err
	}
	routeJSON, err := json.Marshal(item.Route)
	if err != nil {
		return err
	}
	_, err = db.Exec(`
insert into channel_work_queue (id, envelope_json, route_json, status, created_at)
values (?, ?, ?, ?, ?)
on conflict(id) do update set
  envelope_json=excluded.envelope_json,
  route_json=excluded.route_json,
  status=excluded.status,
  created_at=excluded.created_at
`, item.ID, string(envJSON), string(routeJSON), item.Status, item.CreatedAt)
	return err
}

func listWorkItemsDB(db *sql.DB) ([]WorkItem, error) {
	rows, err := db.Query(`select id, envelope_json, route_json, status, created_at from channel_work_queue order by created_at asc`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []WorkItem
	for rows.Next() {
		item, err := scanWorkItem(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

type workItemScanner interface {
	Scan(dest ...any) error
}

func scanWorkItem(scanner workItemScanner) (WorkItem, error) {
	var item WorkItem
	var envJSON, routeJSON string
	if err := scanner.Scan(&item.ID, &envJSON, &routeJSON, &item.Status, &item.CreatedAt); err != nil {
		return item, err
	}
	if err := json.Unmarshal([]byte(envJSON), &item.Envelope); err != nil {
		return item, fmt.Errorf("decode envelope for %s: %w", item.ID, err)
	}
	if err := json.Unmarshal([]byte(routeJSON), &item.Route); err != nil {
		return item, fmt.Errorf("decode route for %s: %w", item.ID, err)
	}
	return item, nil
}

func popNextWorkItemDB(db *sql.DB) (WorkItem, bool) {
	tx, err := db.Begin()
	if err != nil {
		return WorkItem{}, false
	}
	var item WorkItem
	var envJSON, routeJSON string
	err = tx.QueryRow(`
select id, envelope_json, route_json, status, created_at
from channel_work_queue
where status = 'queued'
order by created_at asc
limit 1
`).Scan(&item.ID, &envJSON, &routeJSON, &item.Status, &item.CreatedAt)
	if err == sql.ErrNoRows {
		_ = tx.Rollback()
		return WorkItem{}, false
	}
	if err != nil {
		_ = tx.Rollback()
		return WorkItem{}, false
	}
	if err := json.Unmarshal([]byte(envJSON), &item.Envelope); err != nil {
		_ = tx.Rollback()
		return WorkItem{}, false
	}
	if err := json.Unmarshal([]byte(routeJSON), &item.Route); err != nil {
		_ = tx.Rollback()
		return WorkItem{}, false
	}
	if _, err := tx.Exec(`update channel_work_queue set status = 'dispatching' where id = ?`, item.ID); err != nil {
		_ = tx.Rollback()
		return WorkItem{}, false
	}
	if err := tx.Commit(); err != nil {
		return WorkItem{}, false
	}
	item.Status = "dispatching"
	return item, true
}

func markWorkItemDB(db *sql.DB, id, status string) error {
	_, err := db.Exec(`update channel_work_queue set status = ? where id = ?`, status, id)
	return err
}

func cancelActiveWorkItemsDB(db *sql.DB) int {
	res, err := db.Exec(`update channel_work_queue set status = 'cancelled' where status in ('queued', 'dispatching')`)
	if err != nil {
		return 0
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0
	}
	return int(n)
}
