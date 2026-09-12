package domain

type Connection struct {
	ID       string
	Name     string
	Provider string
}

type Project struct {
	ID           string
	ConnectionID string
	Name         string
}

type Environment struct {
	ID        string
	ProjectID string
	Name      string
}

type Folder struct {
	ID            string
	EnvironmentID string
	ParentID      string
	Name          string
}

type Secret struct {
	ID       string
	FolderID string
	Key      string
	Value    string
}
