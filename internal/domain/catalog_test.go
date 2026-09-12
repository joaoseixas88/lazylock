package domain

import "testing"

func TestDemoCatalogExposesNestedSecretContext(t *testing.T) {
	catalog := DemoCatalog()

	connections := catalog.Connections()
	if len(connections) < 2 {
		t.Fatalf("Connections() returned %d connections, want at least 2", len(connections))
	}

	projects := catalog.Projects(connections[0].ID)
	if len(projects) == 0 {
		t.Fatal("Projects() returned no projects for the first connection")
	}

	environments := catalog.Environments(projects[0].ID)
	if len(environments) == 0 {
		t.Fatal("Environments() returned no environments for the first project")
	}

	folders := catalog.Folders(environments[0].ID, "")
	if len(folders) == 0 {
		t.Fatal("Folders() returned no root folders for the first environment")
	}

	secrets := catalog.Secrets(folders[0].ID)
	if len(secrets) == 0 {
		t.Fatal("Secrets() returned no secrets for the first folder")
	}
	if secrets[0].Value == "" {
		t.Fatal("demo secret has an empty value")
	}
}
