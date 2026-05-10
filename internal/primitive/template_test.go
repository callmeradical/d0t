package primitive_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/callmeradical/d0t/internal/plan"
	"github.com/callmeradical/d0t/internal/primitive"
	"github.com/callmeradical/d0t/internal/secrets"
	"github.com/callmeradical/d0t/internal/vars"
)

// ---------------------------------------------------------------------------
// Template.Plan
// ---------------------------------------------------------------------------

func TestTemplate_Plan_TargetAbsent(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src", ".gitconfig.tmpl")
	mustWrite(t, src, "[user]\n  email = {{ .Vars.email }}\n")
	target := filepath.Join(dir, "dst", ".gitconfig")

	tmpl := primitive.NewTemplate(src, target, "base/home/.gitconfig.tmpl",
		vars.Map{"email": "user@example.com"}, builtins(t))

	ch, err := tmpl.Plan(newCtx(t, false, false, false))
	if err != nil {
		t.Fatal(err)
	}
	if ch.Op != plan.OpCreate {
		t.Errorf("Op = %v, want %v", ch.Op, plan.OpCreate)
	}
}

func TestTemplate_Plan_NoOp(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src", ".gitconfig.tmpl")
	mustWrite(t, src, "[user]\n  email = {{ .Vars.email }}\n")
	target := filepath.Join(dir, "dst", ".gitconfig")

	// Write the expected rendered output to the target.
	mustWrite(t, target, "[user]\n  email = user@example.com\n")

	tmpl := primitive.NewTemplate(src, target, "base/home/.gitconfig.tmpl",
		vars.Map{"email": "user@example.com"}, builtins(t))

	ch, err := tmpl.Plan(newCtx(t, false, false, false))
	if err != nil {
		t.Fatal(err)
	}
	if ch.Op != plan.OpNoOp {
		t.Errorf("Op = %v, want %v", ch.Op, plan.OpNoOp)
	}
}

func TestTemplate_Plan_UpdateWhenVarsChange(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src", ".gitconfig.tmpl")
	mustWrite(t, src, "[user]\n  email = {{ .Vars.email }}\n")
	target := filepath.Join(dir, "dst", ".gitconfig")

	// Target has old email.
	mustWrite(t, target, "[user]\n  email = old@example.com\n")

	tmpl := primitive.NewTemplate(src, target, "base/home/.gitconfig.tmpl",
		vars.Map{"email": "new@example.com"}, builtins(t))

	ch, err := tmpl.Plan(newCtx(t, false, false, false))
	if err != nil {
		t.Fatal(err)
	}
	if ch.Op != plan.OpUpdate {
		t.Errorf("Op = %v, want %v", ch.Op, plan.OpUpdate)
	}
}

func TestTemplate_Plan_InvalidTemplateErrors(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src", ".gitconfig.tmpl")
	mustWrite(t, src, "{{ .Vars.unclosed")
	target := filepath.Join(dir, "dst", ".gitconfig")

	tmpl := primitive.NewTemplate(src, target, "base/home/.gitconfig.tmpl",
		vars.Map{}, builtins(t))

	_, err := tmpl.Plan(newCtx(t, false, false, false))
	if err == nil {
		t.Error("expected error for invalid template syntax, got nil")
	}
}

// ---------------------------------------------------------------------------
// Template.Apply
// ---------------------------------------------------------------------------

func TestTemplate_Apply_RendersVars(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src", ".gitconfig.tmpl")
	mustWrite(t, src, "[user]\n  email = {{ .Vars.email }}\n  name = {{ .Vars.name }}\n")
	target := filepath.Join(dir, "dst", ".gitconfig")

	tmpl := primitive.NewTemplate(src, target, "base/home/.gitconfig.tmpl",
		vars.Map{"email": "user@example.com", "name": "Test User"}, builtins(t))

	if err := tmpl.Apply(newCtx(t, false, false, false)); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	want := "[user]\n  email = user@example.com\n  name = Test User\n"
	if string(got) != want {
		t.Errorf("rendered content:\ngot:  %q\nwant: %q", got, want)
	}
}

func TestTemplate_Apply_BuiltinVars(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src", "info.tmpl")
	mustWrite(t, src, "host={{ .Host }} os={{ .OS }}")
	target := filepath.Join(dir, "dst", "info")

	b := builtins(t)
	b.Host = "myhost"
	b.OS = "darwin"

	tmpl := primitive.NewTemplate(src, target, "base/home/info.tmpl", vars.Map{}, b)
	if err := tmpl.Apply(newCtx(t, false, false, false)); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "host=myhost os=darwin" {
		t.Errorf("got %q", got)
	}
}

func TestTemplate_Apply_DryRunDoesNotWrite(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src", "file.tmpl")
	mustWrite(t, src, "hello")
	target := filepath.Join(dir, "dst", "file")

	tmpl := primitive.NewTemplate(src, target, "base/home/file.tmpl", vars.Map{}, builtins(t))
	if err := tmpl.Apply(newCtx(t, true, false, false)); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(target); !os.IsNotExist(err) {
		t.Error("dry-run should not write the file")
	}
}

func TestTemplate_Apply_Idempotent(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src", "file.tmpl")
	mustWrite(t, src, "hello {{ .Vars.who }}")
	target := filepath.Join(dir, "dst", "file")

	tmpl := primitive.NewTemplate(src, target, "base/home/file.tmpl",
		vars.Map{"who": "world"}, builtins(t))

	for i := 0; i < 3; i++ {
		if err := tmpl.Apply(newCtx(t, false, false, false)); err != nil {
			t.Fatalf("run %d: %v", i+1, err)
		}
	}
	got, _ := os.ReadFile(target)
	if string(got) != "hello world" {
		t.Errorf("got %q, want %q", got, "hello world")
	}
}

func TestTemplate_Apply_FuncDefault(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src", "file.tmpl")
	mustWrite(t, src, `{{ default .Vars.missing "fallback" }}`)
	target := filepath.Join(dir, "dst", "file")

	tmpl := primitive.NewTemplate(src, target, "base/home/file.tmpl", vars.Map{}, builtins(t))
	if err := tmpl.Apply(newCtx(t, false, false, false)); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(target)
	if string(got) != "fallback" {
		t.Errorf("got %q, want %q", got, "fallback")
	}
}

func TestTemplate_Apply_Secret(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src", "config.tmpl")
	mustWrite(t, src, `token={{ secret "env://TEST_API_TOKEN" }}`)
	target := filepath.Join(dir, "dst", "config")

	t.Setenv("TEST_API_TOKEN", "super-secret-value")

	b := builtins(t)
	// Use a full router (via NewFromConfig) so env:// prefix is stripped correctly.
	var err error
	b.Secrets, err = secrets.NewFromConfig("env", nil)
	if err != nil {
		t.Fatal(err)
	}

	tmpl := primitive.NewTemplate(src, target, "base/home/config.tmpl", vars.Map{}, b)
	if err := tmpl.Apply(newCtx(t, false, false, false)); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(target)
	if string(got) != "token=super-secret-value" {
		t.Errorf("got %q, want %q", got, "token=super-secret-value")
	}
}

func TestTemplate_Apply_SecretMissingBackendErrors(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src", "config.tmpl")
	mustWrite(t, src, `token={{ secret "op://Personal/App/token" }}`)
	target := filepath.Join(dir, "dst", "config")

	b := builtins(t)
	b.Secrets = nil // no backend configured

	tmpl := primitive.NewTemplate(src, target, "base/home/config.tmpl", vars.Map{}, b)
	if err := tmpl.Apply(newCtx(t, false, false, false)); err == nil {
		t.Error("expected error when no secrets backend configured, got nil")
	}
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

func builtins(t *testing.T) primitive.BuiltinVars {
	t.Helper()
	return primitive.BuiltinVars{
		Host: "testhost",
		OS:   "linux",
		Arch: "amd64",
		User: "testuser",
		Home: t.TempDir(),
	}
}
