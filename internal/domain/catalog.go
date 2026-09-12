package domain

type Catalog struct {
	connections  []Connection
	projects     []Project
	environments []Environment
	folders      []Folder
	secrets      []Secret
}

func DemoCatalog() Catalog {
	return Catalog{
		connections: []Connection{
			{ID: "infisical-work", Name: "Infisical Work", Provider: "Infisical"},
			{ID: "vault-personal", Name: "Personal Vault", Provider: "Demo"},
		},
		projects: []Project{
			{ID: "payments", ConnectionID: "infisical-work", Name: "Payments API"},
			{ID: "website", ConnectionID: "infisical-work", Name: "Marketing Website"},
			{ID: "homelab", ConnectionID: "vault-personal", Name: "Homelab"},
		},
		environments: []Environment{
			{ID: "payments-dev", ProjectID: "payments", Name: "Development"},
			{ID: "payments-prod", ProjectID: "payments", Name: "Production"},
			{ID: "website-prod", ProjectID: "website", Name: "Production"},
		},
		folders: []Folder{
			{ID: "payments-dev-root", EnvironmentID: "payments-dev", Name: "/"},
			{ID: "payments-dev-services", EnvironmentID: "payments-dev", ParentID: "payments-dev-root", Name: "services"},
			{ID: "payments-prod-root", EnvironmentID: "payments-prod", Name: "/"},
		},
		secrets: []Secret{
			{ID: "stripe-key", FolderID: "payments-dev-root", Key: "STRIPE_SECRET_KEY", Value: "sk_test_demo_123"},
			{ID: "db-url", FolderID: "payments-dev-root", Key: "DATABASE_URL", Value: "postgres://demo:demo@localhost/payments"},
			{ID: "redis-url", FolderID: "payments-dev-services", Key: "REDIS_URL", Value: "redis://localhost:6379/0"},
			{ID: "stripe-live-key", FolderID: "payments-prod-root", Key: "STRIPE_SECRET_KEY", Value: "sk_live_demo_456"},
		},
	}
}

func (c Catalog) Connections() []Connection { return append([]Connection(nil), c.connections...) }

func (c Catalog) Projects(connectionID string) []Project {
	var result []Project
	for _, item := range c.projects {
		if item.ConnectionID == connectionID {
			result = append(result, item)
		}
	}
	return result
}

func (c Catalog) Environments(projectID string) []Environment {
	var result []Environment
	for _, item := range c.environments {
		if item.ProjectID == projectID {
			result = append(result, item)
		}
	}
	return result
}

func (c Catalog) Folders(environmentID, parentID string) []Folder {
	var result []Folder
	for _, item := range c.folders {
		if item.EnvironmentID == environmentID && item.ParentID == parentID {
			result = append(result, item)
		}
	}
	return result
}

func (c Catalog) Secrets(folderID string) []Secret {
	var result []Secret
	for _, item := range c.secrets {
		if item.FolderID == folderID {
			result = append(result, item)
		}
	}
	return result
}
