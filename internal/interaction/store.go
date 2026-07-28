package interaction

// Store persists durable human interaction records for service recovery and updates.
type Store interface {
	LoadAll() ([]Interaction, error)
	Save(interaction Interaction) error
}
