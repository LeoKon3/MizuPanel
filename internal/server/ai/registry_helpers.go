package ai

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
	"unicode/utf8"

	"github.com/mizupanel/mizupanel/internal/protocol"
	"github.com/mizupanel/mizupanel/internal/server/servicecenter"
	"github.com/mizupanel/mizupanel/internal/server/store"
)

func validIdentifier(value string, maxLength int) bool {
	value = strings.TrimSpace(value)
	return value != "" && utf8.ValidString(value) && len(value) <= maxLength && !strings.ContainsAny(value, "\x00\r\n")
}

func oneOf(value string, allowed ...string) bool {
	for _, item := range allowed {
		if value == item {
			return true
		}
	}
	return false
}

func boundedString(value string, maximum int) string {
	value = strings.ToValidUTF8(value, "")
	if len(value) <= maximum {
		return value
	}
	for maximum > 0 && !utf8.RuneStart(value[maximum]) {
		maximum--
	}
	return value[:maximum]
}

func sanitizeModelText(value string, maximum int) string {
	value = strings.ToValidUTF8(value, "")
	value = modelSensitiveHeaderPattern.ReplaceAllString(value, "$1: [REDACTED]")
	value = modelBearerPattern.ReplaceAllString(value, "Bearer [REDACTED]")
	value = modelNamedSecretPattern.ReplaceAllString(value, "$1=[REDACTED]")
	value = modelEnvironmentPattern.ReplaceAllString(value, "$1=[REDACTED]")
	return boundedString(value, maximum)
}

func countOnline(nodes []store.Node) int {
	count := 0
	for _, node := range nodes {
		if node.Status == "online" {
			count++
		}
	}
	return count
}

func countMonitorFailures(monitors []store.UptimeMonitor) int {
	count := 0
	for _, monitor := range monitors {
		if monitor.Status != "up" && monitor.Status != "pending" {
			count++
		}
	}
	return count
}

func countUnhealthyServices(services []servicecenter.ServiceSummary) int {
	count := 0
	for _, service := range services {
		if service.Health == servicecenter.HealthUnhealthy || service.Health == servicecenter.HealthDegraded {
			count++
		}
	}
	return count
}

func metricProjection(metric store.Metric) map[string]any {
	return map[string]any{"cpu_percent": round(metric.CPUUsage), "memory_percent": round(metric.MemoryUsage), "disk_percent": round(metric.DiskUsage), "load_1": round(metric.Load1), "collected_at": metric.CreatedAt}
}

func round(value float64) float64 { return math.Round(value*100) / 100 }

func aggregateMetrics(metrics []store.Metric) map[string]any {
	if len(metrics) == 0 {
		return map[string]any{"samples": 0}
	}
	var cpu, memory, disk, maxCPU float64
	for _, metric := range metrics {
		cpu += metric.CPUUsage
		memory += metric.MemoryUsage
		disk += metric.DiskUsage
		if metric.CPUUsage > maxCPU {
			maxCPU = metric.CPUUsage
		}
	}
	count := float64(len(metrics))
	return map[string]any{"samples": len(metrics), "latest": metricProjection(metrics[len(metrics)-1]), "average_cpu_percent": round(cpu / count), "average_memory_percent": round(memory / count), "average_disk_percent": round(disk / count), "max_cpu_percent": round(maxCPU)}
}

func safeRemoteError(err error) error {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	return fmt.Errorf("remote operation failed")
}

func composeTargetExists(projects []protocol.DockerComposeProject, projectName, serviceName string) bool {
	for _, project := range projects {
		if project.Name != projectName && project.ManagedProjectID != projectName {
			continue
		}
		if serviceName == "" {
			return true
		}
		for _, service := range project.Services {
			if service.Name == serviceName {
				return true
			}
		}
	}
	return false
}

func systemdTargetExists(services []protocol.SystemdService, name string) bool {
	for _, service := range services {
		if service.Name == name {
			return true
		}
	}
	return false
}

func dockerContainerTarget(snapshot protocol.DockerSnapshot, found bool, id string) (protocol.ContainerInfo, bool) {
	if !found || !snapshot.Available {
		return protocol.ContainerInfo{}, false
	}
	for _, container := range snapshot.Containers {
		if container.ID == id || container.FullID == id || container.Name == id {
			return container, true
		}
	}
	return protocol.ContainerInfo{}, false
}

func containerID(container protocol.ContainerInfo) string {
	if container.FullID != "" {
		return container.FullID
	}
	return container.ID
}
