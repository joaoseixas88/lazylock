package fake

import (
	"context"
	"errors"
	"testing"

	"github.com/joaoseixas88/lazylock/internal/domain"
)

func TestDemoCatalogWalksTheWholeHierarchy(t *testing.T) {
	ctx := context.Background()
	catalog := DemoCatalog()

	projects, err := catalog.Projects(ctx)
	if err != nil {
		t.Fatalf("Projects() failed: %v", err)
	}
	if len(projects) == 0 {
		t.Fatal("Projects() returned nothing")
	}

	scopes, err := catalog.Scopes(ctx, projects[0].ID)
	if err != nil {
		t.Fatalf("Scopes() failed: %v", err)
	}
	if len(scopes) == 0 {
		t.Fatal("Scopes() returned nothing for the first project")
	}
	if scopes[0].Path != "/" {
		t.Fatalf("first scope path = %q, want the root", scopes[0].Path)
	}
	if scopes[0].EnvSlug == "" {
		t.Fatal("a scope without an environment slug cannot address the API")
	}

	secrets, err := catalog.Secrets(ctx, scopes[0])
	if err != nil {
		t.Fatalf("Secrets() failed: %v", err)
	}
	if len(secrets) == 0 {
		t.Fatal("Secrets() returned nothing for the root scope")
	}
	if secrets[0].Value == "" {
		t.Fatal("demo secret has an empty value")
	}
}

func TestDemoCatalogCoversTheHiddenAndEmptyCases(t *testing.T) {
	ctx := context.Background()
	catalog := DemoCatalog()
	scopes, err := catalog.Scopes(ctx, "payments")
	if err != nil {
		t.Fatal(err)
	}

	var nested, hidden, empty bool
	for _, scope := range scopes {
		if scope.Path != "/" {
			nested = true
		}
		secrets, err := catalog.Secrets(ctx, scope)
		if err != nil {
			t.Fatal(err)
		}
		for _, secret := range secrets {
			hidden = hidden || secret.Hidden
			empty = empty || (!secret.Hidden && secret.Value == "")
		}
	}
	if !nested || !hidden || !empty {
		t.Fatalf("fixture must exercise every case: nested=%v hidden=%v empty=%v", nested, hidden, empty)
	}
}

func TestInjectedErrorSurfaces(t *testing.T) {
	want := errors.New("nope")
	catalog := DemoCatalog()
	catalog.ProjectsErr = want

	if _, err := catalog.Projects(context.Background()); !errors.Is(err, want) {
		t.Fatalf("Projects() error = %v, want %v", err, want)
	}
}

func TestCancelledContextIsHonoured(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := DemoCatalog().Projects(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("Projects() error = %v, want context.Canceled", err)
	}
}

func keysOf(secrets []domain.Secret) map[string]domain.Secret {
	byKey := map[string]domain.Secret{}
	for _, s := range secrets {
		byKey[s.Key] = s
	}
	return byKey
}

func TestImportsAreRelationsBetweenFolders(t *testing.T) {
	ctx := context.Background()
	catalog := DemoCatalog()
	root, services := scope("payments", "dev", "Development", "/"), scope("payments", "dev", "Development", "/services")

	secrets, _ := catalog.Secrets(ctx, services)
	if db := keysOf(secrets)["DATABASE_URL"]; db.ImportedFrom != root {
		t.Fatalf("DATABASE_URL in /services = %+v, want it imported from /", db)
	}
	if _, err := catalog.Delete(ctx, root, []string{"DATABASE_URL"}); err != nil {
		t.Fatal(err)
	}
	if secrets, _ = catalog.Secrets(ctx, services); keysOf(secrets)["DATABASE_URL"].Key != "" {
		t.Fatal("deleting the source must remove the secret from every folder importing it")
	}
}

func TestDeleteIsAllOrNothing(t *testing.T) {
	ctx := context.Background()
	catalog := DemoCatalog()
	root := scope("payments", "dev", "Development", "/")
	if _, err := catalog.Delete(ctx, root, []string{"STRIPE_SECRET_KEY", "MISSING"}); !errors.Is(err, domain.ErrRejected) {
		t.Fatalf("err = %v, want a rejection", err)
	}
	if secrets, _ := catalog.Secrets(ctx, root); keysOf(secrets)["STRIPE_SECRET_KEY"].Key == "" {
		t.Fatal("a rejected delete must change nothing")
	}
}

func TestApprovalAppliesNothing(t *testing.T) {
	ctx := context.Background()
	catalog := DemoCatalog()
	catalog.Approval = true
	root := scope("payments", "dev", "Development", "/")
	outcome, err := catalog.Delete(ctx, root, []string{"STRIPE_SECRET_KEY"})
	if err != nil || !outcome.Pending {
		t.Fatalf("Delete = %+v, %v", outcome, err)
	}
	if secrets, _ := catalog.Secrets(ctx, root); keysOf(secrets)["STRIPE_SECRET_KEY"].Key == "" {
		t.Fatal("a change request must not apply the change")
	}
}
