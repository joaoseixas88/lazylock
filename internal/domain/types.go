package domain

// Project is one secret-manager project on the configured connection.
type Project struct {
	ID   string
	Name string
}

// Scope is one row of the "Paths / Environments" pane and, at the same time,
// the complete address of a secret list. Every field is a string so that Scope
// stays comparable: the TUI compares scopes with == to decide whether a reply
// still answers the question the user is asking.
type Scope struct {
	ProjectID string
	EnvSlug   string // provider-native environment key ("dev"); never displayed
	EnvName   string // human label ("Development"); display only
	Path      string // "/", "/services", "/services/api"
}

// Secret is one entry at a Scope. Hidden marks the ones a session may list but
// not read, which is a different thing from a secret whose value is empty.
type Secret struct {
	ID      string
	Key     string
	Value   string
	Hidden  bool
	Comment string
}
