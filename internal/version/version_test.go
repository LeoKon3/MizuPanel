package version

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

var semanticVersionPattern = regexp.MustCompile(`^\d+\.\d+\.\d+$`)

func TestPrintCommand(t *testing.T) {
	t.Parallel()

	for _, argument := range []string{"version", "--version", "-v"} {
		argument := argument
		t.Run(argument, func(t *testing.T) {
			t.Parallel()

			var output bytes.Buffer
			handled, err := PrintCommand([]string{argument}, "Agent", &output)
			if err != nil {
				t.Fatalf("PrintCommand returned error: %v", err)
			}
			if !handled {
				t.Fatal("PrintCommand did not handle version argument")
			}
			if want := "MizuPanel Agent v" + Current + "\n"; output.String() != want {
				t.Fatalf("output = %q, want %q", output.String(), want)
			}
		})
	}
}

func TestPrintCommandLeavesNormalStartupArgumentsUntouched(t *testing.T) {
	t.Parallel()

	for _, args := range [][]string{nil, {"--config", "agent.yaml"}, {"version", "extra"}} {
		var output bytes.Buffer
		handled, err := PrintCommand(args, "Agent", &output)
		if err != nil {
			t.Fatalf("PrintCommand(%q) returned error: %v", args, err)
		}
		if handled || output.Len() != 0 {
			t.Fatalf("PrintCommand(%q) handled normal startup arguments", args)
		}
	}
}

func TestReleaseVersionSurfacesStayInSync(t *testing.T) {
	t.Parallel()

	if !semanticVersionPattern.MatchString(Current) {
		t.Fatalf("Current version %q is not a semantic major.minor.patch version", Current)
	}

	repositoryRoot := filepath.Clean(filepath.Join("..", ".."))
	assertTextVersion(t, filepath.Join(repositoryRoot, "VERSION"), Current)
	assertPackageVersions(t, repositoryRoot, Current)
	assertReadmeBadge(t, filepath.Join(repositoryRoot, "README.md"), Current)
	assertReadmeBadge(t, filepath.Join(repositoryRoot, "README.en.md"), Current)
	assertLatestChangelogVersion(t, filepath.Join(repositoryRoot, "CHANGELOG.md"), Current)
	assertComposeDefaultImage(t, filepath.Join(repositoryRoot, "docker-compose.yml"), Current)
	assertComposeDefaultImage(t, filepath.Join(repositoryRoot, "docker-compose.mysql.yml"), Current)
	assertDockerImageIncludesVersion(t, filepath.Join(repositoryRoot, "Dockerfile"))
}

func assertTextVersion(t *testing.T, path, expected string) {
	t.Helper()

	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	if actual := strings.TrimSpace(string(content)); actual != expected {
		t.Fatalf("%s version = %q, want %q", path, actual, expected)
	}
}

func assertPackageVersions(t *testing.T, repositoryRoot, expected string) {
	t.Helper()

	var packageJSON struct {
		Version string `json:"version"`
	}
	readJSON(t, filepath.Join(repositoryRoot, "web", "package.json"), &packageJSON)
	if packageJSON.Version != expected {
		t.Fatalf("web/package.json version = %q, want %q", packageJSON.Version, expected)
	}

	var packageLock struct {
		Version  string `json:"version"`
		Packages map[string]struct {
			Version string `json:"version"`
		} `json:"packages"`
	}
	readJSON(t, filepath.Join(repositoryRoot, "web", "package-lock.json"), &packageLock)
	if packageLock.Version != expected {
		t.Fatalf("web/package-lock.json version = %q, want %q", packageLock.Version, expected)
	}
	rootPackage, ok := packageLock.Packages[""]
	if !ok {
		t.Fatal("web/package-lock.json is missing the root package entry")
	}
	if rootPackage.Version != expected {
		t.Fatalf("web/package-lock.json root package version = %q, want %q", rootPackage.Version, expected)
	}
}

func readJSON(t *testing.T, path string, target any) {
	t.Helper()

	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	if err := json.Unmarshal(content, target); err != nil {
		t.Fatalf("decode %s: %v", path, err)
	}
}

func assertReadmeBadge(t *testing.T, path, expected string) {
	t.Helper()

	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	badge := "version-" + expected + "-"
	if !strings.Contains(string(content), badge) {
		t.Fatalf("%s does not contain version badge %q", path, badge)
	}
}

func assertLatestChangelogVersion(t *testing.T, path, expected string) {
	t.Helper()

	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	headingPattern := regexp.MustCompile(`(?m)^## v(\d+\.\d+\.\d+) - \d{4}-\d{2}-\d{2}$`)
	match := headingPattern.FindStringSubmatch(string(content))
	if len(match) != 2 {
		t.Fatalf("%s has no release heading", path)
	}
	if match[1] != expected {
		t.Fatalf("latest changelog version = %q, want %q", match[1], expected)
	}
}

func assertComposeDefaultImage(t *testing.T, path, expectedVersion string) {
	t.Helper()

	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	imagePattern := regexp.MustCompile(`(?m)^\s*image:\s*\$\{MIZUPANEL_IMAGE:-([^}\s]+)\}\s*$`)
	matches := imagePattern.FindAllStringSubmatch(string(content), -1)
	if len(matches) != 1 {
		t.Fatalf("%s has %d MIZUPANEL_IMAGE defaults, want 1", path, len(matches))
	}
	want := "leokon3/mizupanel:" + expectedVersion
	if matches[0][1] != want {
		t.Fatalf("%s default image = %q, want %q", path, matches[0][1], want)
	}
}

func assertDockerImageIncludesVersion(t *testing.T, path string) {
	t.Helper()

	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	if !regexp.MustCompile(`(?m)^COPY\s+VERSION\s+/app/VERSION\s*$`).Match(content) {
		t.Fatalf("%s does not copy VERSION into the runtime image", path)
	}
}
