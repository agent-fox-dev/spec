package project

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// projectDir is a repository holding files, by name and content.
func projectDir(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// allTests is a tasks artifact whose test_commands.all_tests is cmd.
func allTests(cmd string) map[string]any {
	return map[string]any{"test_commands": map[string]any{"all_tests": cmd}}
}

// Each manifest is detected with its ecosystem's conventional commands, and a
// lockfile picks the tool that runs them.
func TestDetectProfile_KnowsEachEcosystem(t *testing.T) {
	for _, c := range []struct {
		name     string
		files    map[string]string
		language string
		manifest string
		allTests string
		linter   string
	}{
		{"go", map[string]string{"go.mod": "module x\n"}, "go", "go.mod", "go test ./... -count=1", "go vet ./..."},
		{"rust", map[string]string{"Cargo.toml": "[package]\n"}, "rust", "Cargo.toml", "cargo test", "cargo clippy -- -D warnings"},
		{"pyproject", map[string]string{"pyproject.toml": "[project]\n"}, "python", "pyproject.toml", "pytest -q", "ruff check ."},
		{"setup.py", map[string]string{"setup.py": ""}, "python", "setup.py", "pytest -q", "ruff check ."},
		{"requirements.txt", map[string]string{"requirements.txt": "requests\n"}, "python", "requirements.txt", "pytest -q", "ruff check ."},
		{"uv", map[string]string{"pyproject.toml": "", "uv.lock": ""}, "python", "pyproject.toml", "uv run pytest -q", "uv run ruff check ."},
		{"poetry", map[string]string{"pyproject.toml": "", "poetry.lock": ""}, "python", "pyproject.toml", "poetry run pytest -q", "poetry run ruff check ."},
		{"pdm", map[string]string{"pyproject.toml": "", "pdm.lock": ""}, "python", "pyproject.toml", "pdm run pytest -q", "pdm run ruff check ."},
		{"node", map[string]string{"package.json": `{"scripts":{"test":"jest","lint":"eslint ."}}`}, "node", "package.json", "npm test", "npm run lint"},
		{"maven", map[string]string{"pom.xml": "<project/>"}, "jvm", "pom.xml", "mvn -q test", "mvn -q verify"},
		{"gradle", map[string]string{"build.gradle": ""}, "jvm", "build.gradle", "gradle test", "gradle check"},
		{"gradle wrapper", map[string]string{"build.gradle.kts": "", "gradlew": ""}, "jvm", "build.gradle.kts", "./gradlew test", "./gradlew check"},
		{"dotnet project", map[string]string{"App.csproj": "<Project/>"}, "dotnet", "App.csproj", "dotnet test", "dotnet format --verify-no-changes"},
		{"dotnet solution", map[string]string{"App.sln": ""}, "dotnet", "App.sln", "dotnet test", "dotnet format --verify-no-changes"},
		{"ruby", map[string]string{"Gemfile": ""}, "ruby", "Gemfile", "bundle exec rspec", "bundle exec rubocop"},
		{"elixir", map[string]string{"mix.exs": ""}, "elixir", "mix.exs", "mix test", "mix format --check-formatted"},
		{"php", map[string]string{"composer.json": "{}"}, "php", "composer.json", "vendor/bin/phpunit", ""},
		{"dart", map[string]string{"pubspec.yaml": "name: x\n"}, "dart", "pubspec.yaml", "dart test", "dart analyze"},
		{"flutter", map[string]string{"pubspec.yaml": "name: x\nflutter:\n  sdk: flutter\n"}, "dart", "pubspec.yaml", "flutter test", "flutter analyze"},
		{"swift", map[string]string{"Package.swift": ""}, "swift", "Package.swift", "swift test", ""},
		{"cmake", map[string]string{"CMakeLists.txt": ""}, "cpp", "CMakeLists.txt", "ctest --test-dir build", ""},
	} {
		t.Run(c.name, func(t *testing.T) {
			got := DetectProfile(projectDir(t, c.files))
			if got.Language != c.language || got.Manifest != c.manifest || got.AllTests != c.allTests || got.Linter != c.linter {
				t.Errorf("got %s from %s / %q / %q, want %s from %s / %q / %q",
					got.Language, got.Manifest, got.AllTests, got.Linter, c.language, c.manifest, c.allTests, c.linter)
			}
			if !got.Known() {
				t.Error("Known() = false")
			}
			if got.StubMarker == "" {
				t.Error("no stub marker")
			}
		})
	}
}

func TestDetectProfile_ReturnsNothingWhenItCannotTell(t *testing.T) {
	if got := DetectProfile(t.TempDir()); got.Known() {
		t.Errorf("got %+v for an empty directory", got)
	}
}

// A node project's commands are its own scripts, run by the tool its lockfile
// names; a script it does not have is not a command.
func TestDetectProfile_NodeCommandsAreItsScripts(t *testing.T) {
	scripts := `{"scripts": {"test": "vitest run", "lint": "eslint ."}}`
	for _, c := range []struct {
		name, pkg, lock, allTests, linter string
	}{
		{"npm", scripts, "", "npm test", "npm run lint"},
		{"pnpm", scripts, "pnpm-lock.yaml", "pnpm test", "pnpm run lint"},
		{"yarn", scripts, "yarn.lock", "yarn test", "yarn lint"},
		{"bun", scripts, "bun.lockb", "bun run test", "bun run lint"},
		{"the npm init stub", `{"scripts": {"test": "echo \"Error: no test specified\" && exit 1"}}`, "", "", ""},
		{"an unreadable package.json", `{not json`, "", "", ""},
	} {
		t.Run(c.name, func(t *testing.T) {
			files := map[string]string{"package.json": c.pkg}
			if c.lock != "" {
				files[c.lock] = ""
			}
			got := DetectProfile(projectDir(t, files))
			if got.Language != "node" || got.AllTests != c.allTests || got.Linter != c.linter {
				t.Errorf("got %s / %q / %q, want node / %q / %q", got.Language, got.AllTests, got.Linter, c.allTests, c.linter)
			}
		})
	}
}

// The Makefile answers "how do I check this"; the manifest answers "what is
// this written in". Conflating them would make a Go repository with a
// Makefile look like a project with no language.
func TestDetectProfile_MakefileOverridesTheCommandsButNotTheLanguage(t *testing.T) {
	for _, c := range []struct {
		name, makefile, allTests, linter string
	}{
		{"test and lint", "test:\n\tgo test ./...\nlint:\n\tgo vet ./...\n", "make test", "make lint"},
		{"check only", "check:\n\tgo test ./...\n", "make check", "go vet ./..."},
		{"test and check", "test:\n\tgo test ./...\ncheck: test\n", "make test", "go vet ./..."},
		{"no known target", "build:\n\tgo build ./...\n", "go test ./... -count=1", "go vet ./..."},
	} {
		t.Run(c.name, func(t *testing.T) {
			got := DetectProfile(projectDir(t, map[string]string{"go.mod": "module x\n", "Makefile": c.makefile}))
			if got.Language != "go" {
				t.Errorf("Language = %q", got.Language)
			}
			if got.AllTests != c.allTests || got.Linter != c.linter {
				t.Errorf("commands = %q / %q, want %q / %q", got.AllTests, got.Linter, c.allTests, c.linter)
			}
		})
	}
}

// The first manifest decides the language; the others present are recorded
// as other ecosystems of the same repository.
func TestDetectProfile_RecordsTheOtherEcosystemsPresent(t *testing.T) {
	p := DetectProfile(projectDir(t, map[string]string{"go.mod": "", "build.gradle": "", "App.csproj": ""}))
	if p.Language != "go" {
		t.Errorf("Language = %q", p.Language)
	}
	if !slices.Contains(p.Also, "jvm") || !slices.Contains(p.Also, "dotnet") || slices.Contains(p.Also, "go") {
		t.Errorf("Also = %v", p.Also)
	}
}

// The skill asks a human to read tasks.json afterwards and check that
// test_commands names the project's real runner. Here it is a check the model
// repairs before the file is ever written, so the message names both what was
// submitted and what the project uses.
func TestAuditTasks_RefusesAnotherEcosystemsRunner(t *testing.T) {
	profile := DetectProfile(projectDir(t, map[string]string{"go.mod": "module x\n"}))

	err := profile.AuditTasks(map[string]any{
		"test_commands": map[string]any{
			"all_tests": "pytest -q",
			"linter":    "ruff check .",
		},
	})
	if err == nil {
		t.Fatal("a Python command in a Go project was accepted")
	}
	for _, want := range []string{"go", "go.mod", "pytest", "ruff", "go test ./... -count=1", `panic("not implemented")`} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the message is missing %q:\n%v", want, err)
		}
	}
}

// The runner families know the common tools of each ecosystem, so any of
// them is refused in a project of another.
func TestAuditTasks_KnowsTheCommonRunners(t *testing.T) {
	py := DetectProfile(projectDir(t, map[string]string{"pyproject.toml": ""}))
	for _, cmd := range []string{"go test ./...", "mocha", "ava", "bun test", "deno test", "npx playwright test", "mix test",
		"dotnet test", "./mvnw test", "swift test", "flutter test", "vendor/bin/phpunit", "/usr/local/bin/cargo test"} {
		t.Run(cmd, func(t *testing.T) {
			if err := py.AuditTasks(allTests(cmd)); err == nil {
				t.Errorf("%q accepted in a Python project", cmd)
			}
		})
	}
}

// The check is narrow on purpose. `go test` in a Python project is
// unambiguously wrong; a command nobody here has heard of might be
// ./scripts/ci.sh, which is legitimate.
func TestAuditTasks_AcceptsWhatItCannotProveWrong(t *testing.T) {
	goProject := DetectProfile(projectDir(t, map[string]string{"go.mod": "module x\n"}))
	py := DetectProfile(projectDir(t, map[string]string{"pyproject.toml": ""}))
	withFrontend := DetectProfile(projectDir(t, map[string]string{"go.mod": "module x\n", "package.json": "{}"}))
	for _, c := range []struct {
		name    string
		profile Profile
		content map[string]any
	}{
		{"the project's own commands", goProject, map[string]any{"test_commands": map[string]any{
			"all_tests": "go test ./... -count=1", "linter": "go vet ./...", "spec_tests": "go test ./widget/... -count=1"}}},
		{"unknown programs", goProject, map[string]any{"test_commands": map[string]any{
			"all_tests": "./scripts/ci.sh --all", "linter": "make lint"}}},
		{"blank commands", goProject, map[string]any{"test_commands": map[string]any{"all_tests": "  ", "linter": ""}}},
		{"no test_commands", goProject, map[string]any{}},
		{"another ecosystem present in the repository", withFrontend, allTests("npm test")},
		{"pdm", py, allTests("pdm run pytest")},
		{"pipenv", py, allTests("pipenv run pytest")},
		{"nox", py, allTests("nox")},
		{"hatch", py, allTests("hatch test")},
		{"no detected language", Profile{}, allTests("pytest -q")},
	} {
		t.Run(c.name, func(t *testing.T) {
			if err := c.profile.AuditTasks(c.content); err != nil {
				t.Errorf("refused: %v", err)
			}
		})
	}
}

// The prompt block is the positive half of the audit: saying it up front is
// cheaper than refusing a submission afterwards.
func TestLanguageBlock_NamesTheCommands(t *testing.T) {
	profile := DetectProfile(projectDir(t, map[string]string{"go.mod": "module x\n"}))
	block := profile.LanguageBlock()
	for _, want := range []string{"go", "go.mod", "go test ./... -count=1", "go vet ./...", "task.touches"} {
		if !strings.Contains(block, want) {
			t.Errorf("the block is missing %q:\n%s", want, block)
		}
	}
}

func TestLanguageBlock_SaysWhenTheProjectDefinesNoCommand(t *testing.T) {
	block := DetectProfile(projectDir(t, map[string]string{"composer.json": "{}"})).LanguageBlock()
	if !strings.Contains(block, "`test_commands.linter`: the project defines none") {
		t.Errorf("the block does not say there is no linter:\n%s", block)
	}
}

// With no detected language there is nothing to say, and guessing would be
// worse than saying nothing.
func TestLanguageBlock_IsEmptyWithoutAProfile(t *testing.T) {
	if block := (Profile{}).LanguageBlock(); block != "" {
		t.Errorf("an unknown project contributed a prompt block:\n%s", block)
	}
}
