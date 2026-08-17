package protocol

import (
	"strings"
	"testing"
)

func TestValidateDockerContainerEnvironmentBoundsAndRedactsErrors(t *testing.T) {
	const secret = "protocol-environment-secret-marker"
	if err := ValidateDockerContainerEnvironment([]DockerContainerEnvironment{{Key: "API_TOKEN", Value: secret}}); err != nil {
		t.Fatalf("valid environment rejected: %v", err)
	}
	tests := []struct {
		name   string
		values []DockerContainerEnvironment
	}{
		{name: "invalid key", values: []DockerContainerEnvironment{{Key: "BAD-KEY", Value: secret}}},
		{name: "duplicate key", values: []DockerContainerEnvironment{{Key: "TOKEN", Value: secret}, {Key: "TOKEN", Value: "second"}}},
		{name: "nul value", values: []DockerContainerEnvironment{{Key: "TOKEN", Value: secret + "\x00"}}},
		{name: "oversized value", values: []DockerContainerEnvironment{{Key: "TOKEN", Value: strings.Repeat("x", DockerContainerMaxEnvValue+1)}}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := ValidateDockerContainerEnvironment(test.values)
			if err == nil || strings.Contains(err.Error(), secret) {
				t.Fatalf("environment validation error = %v", err)
			}
		})
	}
}

func TestValidateDockerContainerMountsRejectsDangerousAndUncleanPaths(t *testing.T) {
	valid := []DockerContainerMount{
		{Type: "bind", Source: "/srv/mizupanel/data", Target: "/app/data", ReadOnly: true},
		{Type: "volume", Source: "web-data", Target: "/var/lib/web", ReadOnly: false},
	}
	if err := ValidateDockerContainerMounts(valid); err != nil {
		t.Fatalf("valid mounts rejected: %v", err)
	}
	tests := []struct {
		name  string
		mount DockerContainerMount
	}{
		{name: "host root", mount: DockerContainerMount{Type: "bind", Source: "/", Target: "/data"}},
		{name: "docker socket", mount: DockerContainerMount{Type: "bind", Source: "/var/run/docker.sock", Target: "/socket"}},
		{name: "virtual host path", mount: DockerContainerMount{Type: "bind", Source: "/proc/1", Target: "/data"}},
		{name: "dangerous target", mount: DockerContainerMount{Type: "volume", Source: "web-data", Target: "/run/secrets"}},
		{name: "unclean source", mount: DockerContainerMount{Type: "bind", Source: "/srv/data/../secret", Target: "/data"}},
		{name: "source whitespace", mount: DockerContainerMount{Type: "bind", Source: " /srv/data", Target: "/data"}},
		{name: "target whitespace", mount: DockerContainerMount{Type: "volume", Source: "web-data", Target: "/data "}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if err := ValidateDockerContainerMounts([]DockerContainerMount{test.mount}); err == nil {
				t.Fatal("unsafe mount was accepted")
			}
		})
	}
	duplicateTarget := append(append([]DockerContainerMount(nil), valid...), DockerContainerMount{Type: "volume", Source: "other-data", Target: "/app/data"})
	if err := ValidateDockerContainerMounts(duplicateTarget); err == nil {
		t.Fatal("duplicate mount target was accepted")
	}
}
