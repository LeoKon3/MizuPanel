package docker

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/mizupanel/mizupanel/internal/protocol"
)

func newManagedComposeTestHandler(root string, runner composeCommandRunner) *ComposeHandler {
	return &ComposeHandler{
		supported:     true,
		runner:        runner,
		cachePath:     filepath.Join(filepath.Dir(root), "compose-projects.json"),
		managedRoot:   root,
		active:        make(map[string]bool),
		confirmations: make(map[string]composeDeploymentConfirmation),
	}
}

func testManagedComposeYAML(image string) string {
	return "services:\n  app:\n    image: " + image + "\n"
}

func previewManagedCompose(t *testing.T, handler *ComposeHandler, request protocol.DockerComposeDeploymentRequest) protocol.DockerComposeDeploymentResponse {
	t.Helper()
	request.Action = "preview"
	request.DisplayName = "Demo App"
	if request.ComposeYAML == "" {
		request.ComposeYAML = testManagedComposeYAML("alpine:3")
	}
	response := handler.HandleDockerComposeDeployment(context.Background(), request)
	if !response.Success {
		t.Fatalf("preview response = %#v", response)
	}
	return response
}

func applyManagedCompose(t *testing.T, handler *ComposeHandler, preview protocol.DockerComposeDeploymentResponse, request protocol.DockerComposeDeploymentRequest) protocol.DockerComposeDeploymentResponse {
	t.Helper()
	request.Action = "apply"
	request.ProjectID = preview.Project.ManagedProjectID
	request.DisplayName = preview.Project.DisplayName
	request.ConfirmationToken = preview.ConfirmationToken
	response := handler.HandleDockerComposeDeployment(context.Background(), request)
	if !response.Success {
		t.Fatalf("apply response = %#v", response)
	}
	return response
}

func TestManagedComposePreviewRejectsTraversalAndSymlinkRoot(t *testing.T) {
	root := filepath.Join(t.TempDir(), "compose")
	calls := 0
	handler := newManagedComposeTestHandler(root, func(context.Context, ...string) (string, string, error) {
		calls++
		return "", "", nil
	})
	response := handler.HandleDockerComposeDeployment(context.Background(), protocol.DockerComposeDeploymentRequest{
		Action:      "preview",
		ProjectID:   "../not-a-project",
		DisplayName: "Demo App",
		ComposeYAML: testManagedComposeYAML("alpine:3"),
	})
	if response.Success || response.Error == "" || calls != 0 {
		t.Fatalf("traversal response = %#v, calls = %d", response, calls)
	}

	target := t.TempDir()
	link := filepath.Join(t.TempDir(), "compose-link")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	handler.managedRoot = link
	response = handler.HandleDockerComposeDeployment(context.Background(), protocol.DockerComposeDeploymentRequest{
		Action:      "preview",
		DisplayName: "Demo App",
		ComposeYAML: testManagedComposeYAML("alpine:3"),
	})
	if response.Success || response.Error == "" || calls != 0 {
		t.Fatalf("symlink root response = %#v, calls = %d", response, calls)
	}

	projectID := "a0f7d6c5-b4e3-4f21-8a9b-0c1d2e3f4a5b"
	handler.managedRoot = root
	if err := os.MkdirAll(root, managedComposeRootMode); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(root, projectID)); err != nil {
		t.Fatal(err)
	}
	response = handler.HandleDockerComposeDeployment(context.Background(), protocol.DockerComposeDeploymentRequest{
		Action:      "preview",
		ProjectID:   projectID,
		DisplayName: "Demo App",
		ComposeYAML: testManagedComposeYAML("alpine:3"),
	})
	if response.Success || response.Error == "" || calls != 0 {
		t.Fatalf("symlink project response = %#v, calls = %d", response, calls)
	}
}

func TestManagedComposeDeploymentUnsupportedWhenRootIsNotDirectory(t *testing.T) {
	root := filepath.Join(t.TempDir(), "compose")
	if err := os.WriteFile(root, []byte("not a directory"), 0600); err != nil {
		t.Fatal(err)
	}
	calls := 0
	handler := newManagedComposeTestHandler(root, func(context.Context, ...string) (string, string, error) {
		calls++
		return "", "", nil
	})

	if handler.SupportsDeployment() {
		t.Fatal("deployment capability advertised for an invalid managed root")
	}
	response := handler.HandleDockerComposeDeployment(context.Background(), protocol.DockerComposeDeploymentRequest{
		Action:      "preview",
		DisplayName: "Demo App",
		ComposeYAML: testManagedComposeYAML("alpine:3"),
	})
	if response.Supported || response.Success || response.Error == "" || calls != 0 {
		t.Fatalf("response = %#v, runner calls = %d", response, calls)
	}
}

func TestManagedComposeApplyUsesPrivatePathsAndPermissions(t *testing.T) {
	root := filepath.Join(t.TempDir(), "compose")
	var calls [][]string
	handler := newManagedComposeTestHandler(root, func(_ context.Context, args ...string) (string, string, error) {
		calls = append(calls, append([]string(nil), args...))
		return "ok", "", nil
	})
	request := protocol.DockerComposeDeploymentRequest{ComposeYAML: testManagedComposeYAML("alpine:3"), EnvFile: "PASSWORD=supersecret\n"}
	preview := previewManagedCompose(t, handler, request)
	if !validManagedComposeProjectID(preview.Project.ManagedProjectID) || preview.Project.Name != managedComposeProjectName(preview.Project.ManagedProjectID) {
		t.Fatalf("preview project = %#v", preview.Project)
	}
	result := applyManagedCompose(t, handler, preview, request)
	if result.Project.ConfigFiles != nil {
		t.Fatalf("deployment response exposed paths: %#v", result.Project)
	}

	projectDir := filepath.Join(root, preview.Project.ManagedProjectID)
	for path, mode := range map[string]os.FileMode{
		root:                                   managedComposeRootMode,
		projectDir:                             managedComposeProjectMode,
		filepath.Join(projectDir, "revisions"): managedComposeProjectMode,
		filepath.Join(projectDir, "compose.yaml"):                                 managedComposeFileMode,
		filepath.Join(projectDir, ".env"):                                         managedComposeFileMode,
		filepath.Join(projectDir, "metadata.json"):                                managedComposeFileMode,
		filepath.Join(projectDir, "revisions", managedComposeRevisionFileName(1)): managedComposeFileMode,
	} {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != mode {
			t.Fatalf("mode for %s = %o, want %o", path, info.Mode().Perm(), mode)
		}
	}
	metadata, err := os.ReadFile(filepath.Join(projectDir, "metadata.json"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(metadata), "supersecret") {
		t.Fatalf("metadata leaked .env value: %s", metadata)
	}
	revision, err := os.ReadFile(filepath.Join(projectDir, "revisions", managedComposeRevisionFileName(1)))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(revision), "supersecret") {
		t.Fatalf("revision leaked .env value: %s", revision)
	}

	if len(calls) != 3 {
		t.Fatalf("calls = %#v", calls)
	}
	projectName := managedComposeProjectName(preview.Project.ManagedProjectID)
	previewArgs := calls[0]
	if len(previewArgs) != 9 || previewArgs[0] != "compose" || previewArgs[1] != "--project-name" || previewArgs[2] != projectName || previewArgs[3] != "--file" || previewArgs[5] != "--env-file" || previewArgs[7] != "config" || previewArgs[8] != "--quiet" {
		t.Fatalf("preview args = %#v", previewArgs)
	}
	if got, want := calls[2], []string{"compose", "--project-name", projectName, "--file", filepath.Join(projectDir, "compose.yaml"), "--env-file", filepath.Join(projectDir, ".env"), "up", "-d"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("up args = %#v, want %#v", got, want)
	}
}

func TestManagedComposePreviewRejectsBuildAndReportsRisks(t *testing.T) {
	root := filepath.Join(t.TempDir(), "compose")
	calls := 0
	handler := newManagedComposeTestHandler(root, func(context.Context, ...string) (string, string, error) {
		calls++
		return "", "", nil
	})
	build := handler.HandleDockerComposeDeployment(context.Background(), protocol.DockerComposeDeploymentRequest{
		Action:      "preview",
		DisplayName: "Demo App",
		ComposeYAML: "services:\n  app:\n    build: .\n",
	})
	if build.Success || build.Error == "" || calls != 0 {
		t.Fatalf("build response = %#v, calls = %d", build, calls)
	}
	composeControl := handler.HandleDockerComposeDeployment(context.Background(), protocol.DockerComposeDeploymentRequest{
		Action:      "preview",
		DisplayName: "Demo App",
		ComposeYAML: testManagedComposeYAML("alpine:3"),
		EnvFile:     "COMPOSE_PROFILES=admin\n",
	})
	if composeControl.Success || composeControl.Error == "" || calls != 0 {
		t.Fatalf("Compose control variable response = %#v, calls = %d", composeControl, calls)
	}
	for name, source := range map[string]string{
		"include":         "include: ./other.yaml\nservices:\n  app:\n    image: alpine:3\n",
		"extends":         "services:\n  app:\n    image: alpine:3\n    extends:\n      file: ./base.yaml\n      service: base\n",
		"env_file":        "services:\n  app:\n    image: alpine:3\n    env_file: ./other.env\n",
		"config_file":     "configs:\n  external:\n    file: ./config.txt\nservices:\n  app:\n    image: alpine:3\n",
		"profiles":        "services:\n  app:\n    image: alpine:3\n    profiles: [admin]\n",
		"merged_env_file": "x-base: &base\n  env_file: ./other.env\nservices:\n  app:\n    <<: *base\n    image: alpine:3\n",
	} {
		t.Run("reject_"+name, func(t *testing.T) {
			if _, err := analyzeManagedComposeYAML(source); err == nil {
				t.Fatalf("unsafe Compose feature %q was accepted", name)
			}
		})
	}

	risky := previewManagedCompose(t, handler, protocol.DockerComposeDeploymentRequest{ComposeYAML: `services:
  app:
    image: alpine:3
    privileged: true
    network_mode: host
    devices:
      - /dev/fuse:/dev/fuse
    volumes:
      - /var/run/docker.sock:/var/run/docker.sock
      - /srv/data:/data
`})
	codes := make(map[string]bool)
	for _, risk := range risky.Risks {
		codes[risk.Code] = true
	}
	for _, code := range []string{"privileged", "host_network", "devices", "docker_socket", "absolute_bind_mount"} {
		if !codes[code] {
			t.Fatalf("missing risk %q in %#v", code, risky.Risks)
		}
	}

	merged := previewManagedCompose(t, handler, protocol.DockerComposeDeploymentRequest{ComposeYAML: `x-risk: &risk
  privileged: true
  network_mode: host
  devices:
    - /dev/fuse:/dev/fuse
  volumes:
    - /var/run/docker.sock:/var/run/docker.sock
    - /srv/data:/data
services:
  app:
    <<: *risk
    image: alpine:3
`})
	mergedCodes := make(map[string]bool)
	for _, risk := range merged.Risks {
		mergedCodes[risk.Code] = true
	}
	for _, code := range []string{"privileged", "host_network", "devices", "docker_socket", "absolute_bind_mount"} {
		if !mergedCodes[code] {
			t.Fatalf("YAML merge hid risk %q in %#v", code, merged.Risks)
		}
	}

	dynamic := previewManagedCompose(t, handler, protocol.DockerComposeDeploymentRequest{
		ComposeYAML: `services:
  app:
    image: alpine:3
    privileged: ${PRIVILEGED}
    network_mode: ${NETWORK_MODE}
    volumes:
      - ${HOST_SOURCE}:/data
`,
		EnvFile: "PRIVILEGED=true\nNETWORK_MODE=host\nHOST_SOURCE=/run/user/1000/docker.sock\n",
	})
	dynamicCodes := make(map[string]bool)
	for _, risk := range dynamic.Risks {
		dynamicCodes[risk.Code] = true
	}
	for _, code := range []string{"privileged", "host_network", "docker_socket", "absolute_bind_mount"} {
		if !dynamicCodes[code] {
			t.Fatalf("Compose interpolation hid risk %q in %#v", code, dynamic.Risks)
		}
	}

	topLevelVolume := previewManagedCompose(t, handler, protocol.DockerComposeDeploymentRequest{ComposeYAML: `volumes:
  docker-data:
    driver_opts:
      type: none
      o: bind
      device: /run/user/1000/docker.sock
services:
  app:
    image: alpine:3
    volumes:
      - docker-data:/data
`})
	topLevelCodes := make(map[string]bool)
	for _, risk := range topLevelVolume.Risks {
		topLevelCodes[risk.Code] = true
	}
	for _, code := range []string{"docker_socket", "absolute_bind_mount"} {
		if !topLevelCodes[code] {
			t.Fatalf("top-level volume hid risk %q in %#v", code, topLevelVolume.Risks)
		}
	}
}

func TestManagedComposeApplyRequiresMatchingConfirmationAndRedactsSecrets(t *testing.T) {
	root := filepath.Join(t.TempDir(), "compose")
	var calls [][]string
	handler := newManagedComposeTestHandler(root, func(_ context.Context, args ...string) (string, string, error) {
		calls = append(calls, append([]string(nil), args...))
		return "", "", nil
	})
	request := protocol.DockerComposeDeploymentRequest{ComposeYAML: testManagedComposeYAML("alpine:3"), EnvFile: "PASSWORD=topsecret\n"}
	preview := previewManagedCompose(t, handler, request)
	missing := handler.HandleDockerComposeDeployment(context.Background(), protocol.DockerComposeDeploymentRequest{
		Action:      "apply",
		ProjectID:   preview.Project.ManagedProjectID,
		DisplayName: preview.Project.DisplayName,
		ComposeYAML: request.ComposeYAML,
		EnvFile:     request.EnvFile,
	})
	if missing.Success || !strings.Contains(missing.Error, "确认令牌") {
		t.Fatalf("missing token response = %#v", missing)
	}
	changed := handler.HandleDockerComposeDeployment(context.Background(), protocol.DockerComposeDeploymentRequest{
		Action:            "apply",
		ProjectID:         preview.Project.ManagedProjectID,
		DisplayName:       preview.Project.DisplayName,
		ComposeYAML:       testManagedComposeYAML("busybox:1"),
		EnvFile:           request.EnvFile,
		ConfirmationToken: preview.ConfirmationToken,
	})
	if changed.Success || !strings.Contains(changed.Error, "确认令牌") {
		t.Fatalf("changed draft response = %#v", changed)
	}
	for _, call := range calls {
		if len(call) > 0 && (call[len(call)-1] == "pull" || call[len(call)-1] == "-d") {
			t.Fatalf("apply command ran without a matching token: %#v", calls)
		}
	}
	if _, err := os.Stat(filepath.Join(root, preview.Project.ManagedProjectID)); !os.IsNotExist(err) {
		t.Fatalf("project directory exists after rejected applies: %v", err)
	}

	const opaqueSecret = "opaque-value-7f12"
	const submittedImage = "registry.example.invalid/private/app:leak-check"
	composeYAML := "services:\n  app:\n    image: " + submittedImage + "\n    environment:\n      NONSTANDARD_FIELD: " + opaqueSecret + "\n"
	failing := newManagedComposeTestHandler(filepath.Join(t.TempDir(), "compose"), func(context.Context, ...string) (string, string, error) {
		return "runner stdout: " + composeYAML, "runner stderr: NONSTANDARD_FIELD=" + opaqueSecret + " image=" + submittedImage, os.ErrInvalid
	})
	redacted := failing.HandleDockerComposeDeployment(context.Background(), protocol.DockerComposeDeploymentRequest{
		Action:      "preview",
		DisplayName: "Demo App",
		ComposeYAML: composeYAML,
		EnvFile:     "UNUSUAL_NAME=" + opaqueSecret + "\n",
	})
	combined := redacted.Error + "\n" + redacted.Output
	if redacted.Success || strings.Contains(combined, opaqueSecret) || strings.Contains(combined, submittedImage) || strings.Contains(combined, composeYAML) || strings.Contains(combined, "NONSTANDARD_FIELD") {
		t.Fatalf("secret leaked in response: %#v", redacted)
	}
}

func TestManagedComposeRollbackAndArchive(t *testing.T) {
	root := filepath.Join(t.TempDir(), "compose")
	var calls [][]string
	handler := newManagedComposeTestHandler(root, func(_ context.Context, args ...string) (string, string, error) {
		calls = append(calls, append([]string(nil), args...))
		return "ok", "", nil
	})
	firstRequest := protocol.DockerComposeDeploymentRequest{ComposeYAML: testManagedComposeYAML("alpine:3"), EnvFile: "FIRST=one\n"}
	firstPreview := previewManagedCompose(t, handler, firstRequest)
	first := applyManagedCompose(t, handler, firstPreview, firstRequest)
	secondRequest := protocol.DockerComposeDeploymentRequest{
		ProjectID:   first.Project.ManagedProjectID,
		DisplayName: first.Project.DisplayName,
		ComposeYAML: testManagedComposeYAML("busybox:1"),
		EnvFile:     "SECOND=two\n",
	}
	secondPreview := previewManagedCompose(t, handler, secondRequest)
	second := applyManagedCompose(t, handler, secondPreview, secondRequest)
	if second.Project.Revision != 2 || !second.Project.RollbackAvailable {
		t.Fatalf("second response = %#v", second)
	}

	rollback := handler.HandleDockerComposeDeployment(context.Background(), protocol.DockerComposeDeploymentRequest{Action: "rollback", ProjectID: first.Project.ManagedProjectID})
	if !rollback.Success || rollback.Project.Revision != 3 {
		t.Fatalf("rollback response = %#v", rollback)
	}
	projectDir := filepath.Join(root, first.Project.ManagedProjectID)
	current, err := os.ReadFile(filepath.Join(projectDir, "compose.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if string(current) != firstRequest.ComposeYAML {
		t.Fatalf("rollback compose = %q, want %q", current, firstRequest.ComposeYAML)
	}
	envFile, err := os.ReadFile(filepath.Join(projectDir, ".env"))
	if err != nil {
		t.Fatal(err)
	}
	if string(envFile) != secondRequest.EnvFile {
		t.Fatalf("rollback unexpectedly changed .env = %q", envFile)
	}

	archive := handler.HandleDockerComposeDeployment(context.Background(), protocol.DockerComposeDeploymentRequest{Action: "archive", ProjectID: first.Project.ManagedProjectID})
	if !archive.Success || archive.Project.Status != "archived" {
		t.Fatalf("archive response = %#v", archive)
	}
	if _, err := os.Stat(projectDir); !os.IsNotExist(err) {
		t.Fatalf("project was not moved to archive: %v", err)
	}
	entries, err := os.ReadDir(filepath.Join(root, "archive"))
	if err != nil || len(entries) != 1 || !entries[0].IsDir() {
		t.Fatalf("archive entries = %#v, err = %v", entries, err)
	}
	archivedDir := filepath.Join(root, "archive", entries[0].Name())
	archivedEnv, err := os.ReadFile(filepath.Join(archivedDir, ".env"))
	if err != nil || string(archivedEnv) != secondRequest.EnvFile {
		t.Fatalf("archive .env = %q, err = %v", archivedEnv, err)
	}
	archivedMetadata, err := os.ReadFile(filepath.Join(archivedDir, "metadata.json"))
	if err != nil || strings.Contains(string(archivedMetadata), "SECOND=two") {
		t.Fatalf("archive metadata = %q, err = %v", archivedMetadata, err)
	}
	last := calls[len(calls)-1]
	if got, want := last, []string{"compose", "--project-name", managedComposeProjectName(first.Project.ManagedProjectID), "--file", filepath.Join(projectDir, "compose.yaml"), "--env-file", filepath.Join(projectDir, ".env"), "down"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("archive args = %#v, want %#v", got, want)
	}
}

func TestManagedComposeApplyRestoresPriorFilesWhenUpFails(t *testing.T) {
	root := filepath.Join(t.TempDir(), "compose")
	handler := newManagedComposeTestHandler(root, func(context.Context, ...string) (string, string, error) {
		return "ok", "", nil
	})
	initialRequest := protocol.DockerComposeDeploymentRequest{ComposeYAML: testManagedComposeYAML("alpine:3"), EnvFile: "FIRST=one\n"}
	initialPreview := previewManagedCompose(t, handler, initialRequest)
	initial := applyManagedCompose(t, handler, initialPreview, initialRequest)

	handler.runner = func(_ context.Context, args ...string) (string, string, error) {
		if len(args) > 0 && args[len(args)-1] == "-d" {
			return "", "up failed", os.ErrInvalid
		}
		return "ok", "", nil
	}
	updateRequest := protocol.DockerComposeDeploymentRequest{
		ProjectID:   initial.Project.ManagedProjectID,
		DisplayName: initial.Project.DisplayName,
		ComposeYAML: testManagedComposeYAML("busybox:1"),
		EnvFile:     "SECOND=two\n",
	}
	updatePreview := previewManagedCompose(t, handler, updateRequest)
	failed := handler.HandleDockerComposeDeployment(context.Background(), protocol.DockerComposeDeploymentRequest{
		Action:            "apply",
		ProjectID:         updatePreview.Project.ManagedProjectID,
		DisplayName:       updatePreview.Project.DisplayName,
		ComposeYAML:       updateRequest.ComposeYAML,
		EnvFile:           updateRequest.EnvFile,
		ConfirmationToken: updatePreview.ConfirmationToken,
	})
	if failed.Success || failed.Error == "" {
		t.Fatalf("failed update response = %#v", failed)
	}
	projectDir := filepath.Join(root, initial.Project.ManagedProjectID)
	composeYAML, err := os.ReadFile(filepath.Join(projectDir, "compose.yaml"))
	if err != nil || string(composeYAML) != initialRequest.ComposeYAML {
		t.Fatalf("compose after failed update = %q, err = %v", composeYAML, err)
	}
	envFile, err := os.ReadFile(filepath.Join(projectDir, ".env"))
	if err != nil || string(envFile) != initialRequest.EnvFile {
		t.Fatalf("env after failed update = %q, err = %v", envFile, err)
	}
	metadata, _, err := handler.loadManagedComposeProject(initial.Project.ManagedProjectID)
	if err != nil || metadata.Revision != 1 {
		t.Fatalf("metadata after failed update = %#v, err = %v", metadata, err)
	}
}

func TestManagedComposeRetainsOnlyBoundedYAMLRevisions(t *testing.T) {
	root := filepath.Join(t.TempDir(), "compose")
	handler := newManagedComposeTestHandler(root, func(context.Context, ...string) (string, string, error) {
		return "ok", "", nil
	})
	request := protocol.DockerComposeDeploymentRequest{ComposeYAML: testManagedComposeYAML("alpine:1"), EnvFile: "SECRET=value\n"}
	preview := previewManagedCompose(t, handler, request)
	result := applyManagedCompose(t, handler, preview, request)
	for version := 2; version <= managedComposeRevisionLimit+2; version++ {
		request = protocol.DockerComposeDeploymentRequest{
			ProjectID:   result.Project.ManagedProjectID,
			DisplayName: result.Project.DisplayName,
			ComposeYAML: testManagedComposeYAML("alpine:" + strconv.Itoa(version)),
			EnvFile:     "SECRET=value\n",
		}
		preview = previewManagedCompose(t, handler, request)
		result = applyManagedCompose(t, handler, preview, request)
	}
	_, paths, err := handler.loadManagedComposeProject(result.Project.ManagedProjectID)
	if err != nil {
		t.Fatal(err)
	}
	revisions, err := listManagedComposeRevisions(paths)
	if err != nil {
		t.Fatal(err)
	}
	if len(revisions) != managedComposeRevisionLimit {
		t.Fatalf("retained revisions = %#v, want %d", revisions, managedComposeRevisionLimit)
	}
	for _, revision := range revisions {
		contents, err := os.ReadFile(revision.Path)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(contents), "SECRET=value") {
			t.Fatalf("revision leaked .env contents: %s", contents)
		}
	}
}

func TestManagedComposeListHidesPathsAndKeepsStoppedProjectActionable(t *testing.T) {
	root := filepath.Join(t.TempDir(), "compose")
	var calls [][]string
	handler := newManagedComposeTestHandler(root, func(_ context.Context, args ...string) (string, string, error) {
		calls = append(calls, append([]string(nil), args...))
		if reflect.DeepEqual(args, []string{"compose", "ls", "--all", "--format", "json"}) {
			return "[]", "", nil
		}
		if len(args) >= 4 && args[len(args)-4] == "ps" {
			return "[]", "", nil
		}
		if len(args) >= 2 && args[len(args)-2] == "up" && args[len(args)-1] == "-d" {
			return "NONSTANDARD_SECRET=action-sentinel " + root, "", nil
		}
		return "ok", "", nil
	})
	request := protocol.DockerComposeDeploymentRequest{ComposeYAML: testManagedComposeYAML("alpine:3")}
	preview := previewManagedCompose(t, handler, request)
	applyManagedCompose(t, handler, preview, request)
	list := handler.HandleDockerComposeList(context.Background(), protocol.DockerComposeListRequest{})
	if !list.Success || !list.DeploymentSupported || len(list.Projects) != 1 {
		t.Fatalf("list response = %#v", list)
	}
	project := list.Projects[0]
	if project.Management != "managed" || project.ManagedProjectID != preview.Project.ManagedProjectID || len(project.ConfigFiles) != 0 {
		t.Fatalf("managed list project = %#v", project)
	}
	cache, err := os.ReadFile(handler.cachePath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(cache), filepath.Join(root, preview.Project.ManagedProjectID)) {
		t.Fatalf("legacy cache retained private managed path: %s", cache)
	}
	action := handler.HandleDockerComposeAction(context.Background(), protocol.DockerComposeActionRequest{ProjectName: project.Name, Action: "up"})
	if !action.Success || strings.Contains(action.Output, "action-sentinel") || strings.Contains(action.Output, root) {
		t.Fatalf("stopped managed project action = %#v", action)
	}

	handler.runner = func(_ context.Context, args ...string) (string, string, error) {
		if reflect.DeepEqual(args, []string{"compose", "ls", "--all", "--format", "json"}) {
			return "[]", "", nil
		}
		if len(args) >= 4 && args[len(args)-4] == "ps" {
			return "", "private path: " + root + " NONSTANDARD_SECRET=sentinel", os.ErrInvalid
		}
		return "", "", nil
	}
	failedList := handler.HandleDockerComposeList(context.Background(), protocol.DockerComposeListRequest{})
	if !failedList.Success || len(failedList.Projects) != 1 || failedList.Projects[0].Error == "" {
		t.Fatalf("failed list response = %#v", failedList)
	}
	serialized := failedList.Projects[0].Error
	if strings.Contains(serialized, root) || strings.Contains(serialized, "sentinel") || strings.Contains(serialized, "NONSTANDARD_SECRET") {
		t.Fatalf("managed list error leaked private diagnostics: %q", serialized)
	}

	handler.runner = func(_ context.Context, args ...string) (string, string, error) {
		if reflect.DeepEqual(args, []string{"compose", "ls", "--all", "--format", "json"}) {
			return "", "private path: " + root + " NONSTANDARD_SECRET=global-sentinel", os.ErrInvalid
		}
		return "", "", nil
	}
	failedDiscovery := handler.HandleDockerComposeList(context.Background(), protocol.DockerComposeListRequest{})
	if failedDiscovery.Success || failedDiscovery.Error == "" || strings.Contains(failedDiscovery.Error, root) || strings.Contains(failedDiscovery.Error, "global-sentinel") {
		t.Fatalf("managed discovery error leaked private diagnostics: %#v", failedDiscovery)
	}
}
