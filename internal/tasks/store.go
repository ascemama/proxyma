package tasks

// Store persists durable task records for manager recovery and lifecycle updates.
type Store interface {
	LoadAll() ([]Task, error)
	Save(task Task) error
}
