package core

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/NamTranQuoc/shlink-go/internal/db"
	"github.com/NamTranQuoc/shlink-go/internal/phpx"
	"github.com/NamTranQuoc/shlink-go/internal/valinor"
)

func newSQLiteStore(t *testing.T) *Store {
	t.Helper()
	d, err := db.Open(db.Params{Driver: db.SQLite, SQLitePath: filepath.Join(t.TempDir(), "test.sqlite")}, true)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.Close() })
	if err := d.CreateSchema(context.Background()); err != nil {
		t.Fatal(err)
	}
	return NewStore(d)
}

func strp(s string) *string { return &s }

func TestDomainServiceListAndRedirects(t *testing.T) {
	ctx := context.Background()
	store := newSQLiteStore(t)
	ds := &DomainService{Store: store, DefaultDomain: "s.test"}

	if _, err := ds.GetOrCreate(ctx, "unused.com", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := ds.ConfigureNotFoundRedirects(ctx, "b.com", NotFoundRedirects{BaseURLRedirect: strp("https://b")}, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := ds.ConfigureNotFoundRedirects(ctx, "a.com", NotFoundRedirects{Regular404Redirect: strp("https://a")}, nil); err != nil {
		t.Fatal(err)
	}
	items, err := ds.ListDomains(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	got := string(phpx.Encode(items, phpx.DiactorosJSONFlags))
	want := `[{"domain":"s.test","isDefault":true,"redirects":{"baseUrlRedirect":null,"regular404Redirect":null,"invalidShortUrlRedirect":null}},` +
		`{"domain":"a.com","isDefault":false,"redirects":{"baseUrlRedirect":null,"regular404Redirect":"https://a","invalidShortUrlRedirect":null}},` +
		`{"domain":"b.com","isDefault":false,"redirects":{"baseUrlRedirect":"https://b","regular404Redirect":null,"invalidShortUrlRedirect":null}}]`
	if got != want {
		t.Fatalf("got %s\nwant %s", got, want)
	}

	a, _ := ds.FindByAuthority(ctx, "a.com", nil)
	domainKey := &APIKey{ID: 99, Roles: []APIKeyRole{{Role: RoleDomainSpecific, Meta: map[string]any{"domain_id": "1"}}}}
	if _, err := ds.ConfigureNotFoundRedirects(ctx, "a.com", NotFoundRedirects{}, domainKey); !IsKind(err, KindDomainNotFound) {
		t.Fatalf("expected domain not found, got %v (a.com id %d)", err, a.ID)
	}
	if _, err := ds.GetDomain(ctx, "12345"); !IsKind(err, KindDomainNotFound) {
		t.Fatalf("expected domain not found, got %v", err)
	}
}

func TestMapDomainRedirectsRequest(t *testing.T) {
	if _, err := MapDomainRedirectsRequest(valinor.Input{"domain": "not valid"}); !IsKind(err, KindValidation) {
		t.Fatalf("expected validation error, got %v", err)
	}
	r, err := MapDomainRedirectsRequest(valinor.Input{"domain": "foo.com", "regular404Redirect": nil, "baseUrlRedirect": " <b>x</b> "})
	if err != nil {
		t.Fatal(err)
	}
	got := r.ToNotFoundRedirects(&Domain{InvalidShortURLRedirect: strp("inv"), Regular404Redirect: strp("old")})
	if s := string(phpx.Encode(got, 0)); s != `{"baseUrlRedirect":"x","regular404Redirect":null,"invalidShortUrlRedirect":"inv"}` {
		t.Fatalf("got %s", s)
	}
}

func TestAPIKeyService(t *testing.T) {
	ctx := context.Background()
	store := newSQLiteStore(t)
	svc := &APIKeyService{Store: store}

	initial, err := svc.CreateInitial(ctx, "initial_key")
	if err != nil || initial == nil || initial.Name != "initial_key" {
		t.Fatalf("initial: %+v %v", initial, err)
	}
	if again, err := svc.CreateInitial(ctx, "other"); err != nil || again != nil {
		t.Fatalf("second initial: %+v %v", again, err)
	}

	meta := NewAPIKeyMeta(nil, nil, nil, []RoleDefinition{RoleForAuthoredShortURLs(), RoleForDomain(&Domain{ID: 3, Authority: "x.com"})})
	if !strings.HasSuffix(meta.Name, "-****-****-****-************") || meta.Name[:8] != meta.Key[:8] || len(meta.Key) != 36 {
		t.Fatalf("meta: %+v", meta)
	}
	k, err := svc.Create(ctx, meta)
	if err != nil {
		t.Fatal(err)
	}
	res, err := svc.Check(ctx, meta.Key)
	if err != nil || !res.IsValid(store.Now()) || len(res.APIKey.Roles) != 2 || res.APIKey.Roles[1].DomainAuthority() != "x.com" {
		t.Fatalf("check: %+v %v", res.APIKey, err)
	}
	var rawMeta string
	_ = store.DB.QueryRow(ctx, store.DB.DB, "SELECT meta FROM api_key_roles WHERE role_name = 'DOMAIN_SPECIFIC'").Scan(&rawMeta)
	if rawMeta != `{"domain_id":"3","authority":"x.com"}` {
		t.Fatalf("meta json: %s", rawMeta)
	}

	if _, err := svc.Create(ctx, NewAPIKeyMeta(nil, strp("initial_key"), nil, nil)); err == nil ||
		err.Error() != `Another API key with name "initial_key" already exists` {
		t.Fatalf("expected conflict, got %v", err)
	}
	if _, err := svc.RenameAPIKey(ctx, Renaming{OldName: k.Name, NewName: "initial_key"}); err == nil ||
		err.Error() != `An API key with name "initial_key" already exists` {
		t.Fatalf("expected rename conflict, got %v", err)
	}
	if _, err := svc.RenameAPIKey(ctx, Renaming{OldName: k.Name, NewName: "renamed"}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.DisableByName(ctx, "renamed"); err != nil {
		t.Fatal(err)
	}
	enabled, err := svc.ListKeys(ctx, true)
	if err != nil || len(enabled) != 1 || enabled[0].Name != "initial_key" {
		t.Fatalf("enabled: %v %v", enabled, err)
	}
	if err := svc.DeleteByName(ctx, "missing"); err == nil || err.Error() != `API key with name "missing" not found` {
		t.Fatalf("expected not found, got %v", err)
	}
	if err := svc.DeleteByName(ctx, "renamed"); err != nil {
		t.Fatal(err)
	}
	all, _ := svc.ListKeys(ctx, false)
	if len(all) != 1 {
		t.Fatalf("all: %v", all)
	}
}
