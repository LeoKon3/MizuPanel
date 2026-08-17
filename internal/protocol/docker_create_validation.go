package protocol

import (
	"errors"
	"path"
	"regexp"
	"strings"
	"unicode/utf8"
)

var (
	dockerEnvironmentKeyPattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]{0,127}$`)
	dockerVolumeNamePattern     = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,127}$`)
)

func ValidateDockerContainerEnvironment(values []DockerContainerEnvironment) error {
	if len(values) > DockerContainerMaxEnvironment {
		return errors.New("invalid Docker environment")
	}
	seen := make(map[string]struct{}, len(values))
	total := 0
	for _, item := range values {
		if !dockerEnvironmentKeyPattern.MatchString(item.Key) || !utf8.ValidString(item.Value) || len(item.Value) > DockerContainerMaxEnvValue || strings.ContainsRune(item.Value, '\x00') {
			return errors.New("invalid Docker environment")
		}
		if _, exists := seen[item.Key]; exists {
			return errors.New("invalid Docker environment")
		}
		seen[item.Key] = struct{}{}
		total += len(item.Key) + len(item.Value) + 1
		if total > DockerContainerMaxEnvBytes {
			return errors.New("invalid Docker environment")
		}
	}
	return nil
}

func ValidateDockerContainerMounts(mounts []DockerContainerMount) error {
	if len(mounts) > DockerContainerMaxMounts {
		return errors.New("invalid Docker mounts")
	}
	seenTargets := make(map[string]struct{}, len(mounts))
	for _, mount := range mounts {
		source := mount.Source
		target := mount.Target
		if !validDockerContainerPath(target) || target == "/" || deniedDockerContainerPath(target) {
			return errors.New("invalid Docker mounts")
		}
		if _, exists := seenTargets[target]; exists {
			return errors.New("invalid Docker mounts")
		}
		seenTargets[target] = struct{}{}
		switch mount.Type {
		case "bind":
			if !validDockerContainerPath(source) || deniedDockerHostPath(source) {
				return errors.New("invalid Docker mounts")
			}
		case "volume":
			if !dockerVolumeNamePattern.MatchString(source) {
				return errors.New("invalid Docker mounts")
			}
		default:
			return errors.New("invalid Docker mounts")
		}
	}
	return nil
}

func validDockerContainerPath(value string) bool {
	return value != "" && strings.TrimSpace(value) == value && len(value) <= 4096 && utf8.ValidString(value) && strings.HasPrefix(value, "/") && path.Clean(value) == value && !strings.ContainsRune(value, '\x00')
}

func deniedDockerHostPath(value string) bool {
	if value == "/" || deniedDockerContainerPath(value) {
		return true
	}
	return value == "/var/run/docker.sock" || value == "/run/docker.sock" || strings.HasSuffix(value, "/docker.sock")
}

func deniedDockerContainerPath(value string) bool {
	for _, root := range []string{"/proc", "/sys", "/dev", "/run", "/var/run"} {
		if value == root || strings.HasPrefix(value, root+"/") {
			return true
		}
	}
	return false
}
