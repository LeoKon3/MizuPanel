package docker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/mizupanel/mizupanel/internal/protocol"
)

const (
	defaultSocketPath           = "/var/run/docker.sock"
	defaultStatsTimeout         = 5 * time.Second  // 单容器 stats 超时
	defaultCollectionTimeout    = 30 * time.Second // 整体采集超时（支持并发采集多容器）
	defaultContainerLimit       = 100
	containerCreateProbeTimeout = 750 * time.Millisecond
	maxConcurrentStats          = 10 // 最大并发采集容器数
)

type Collector struct {
	socketPath        string
	statsTimeout      time.Duration
	collectionTimeout time.Duration
	containerLimit    int
	client            clientAPI
	resourceClient    resourceClientAPI
}

type clientAPI interface {
	Version(ctx context.Context) (string, error)
	ListContainers(ctx context.Context, limit int) ([]containerListItem, error)
	InspectContainer(ctx context.Context, id string) (containerInspect, error)
	ContainerStats(ctx context.Context, id string) (containerStats, error)
	ContainerLogs(ctx context.Context, id string, tail int, follow bool, timestamps bool) (io.ReadCloser, error)
}

type containerClientAPI interface {
	CreateContainer(context.Context, protocol.DockerContainerCreateRequest) (containerCreateResult, error)
}

type containerCreateResult struct {
	ID   string
	Name string
}

func NewCollector() *Collector {
	return &Collector{socketPath: defaultSocketPath, statsTimeout: defaultStatsTimeout, collectionTimeout: defaultCollectionTimeout, containerLimit: defaultContainerLimit}
}

func (c *Collector) Collect() protocol.DockerSnapshot {
	if c.socketPath == "" {
		c.socketPath = defaultSocketPath
	}
	if c.statsTimeout <= 0 {
		c.statsTimeout = defaultStatsTimeout
	}
	if c.collectionTimeout <= 0 {
		c.collectionTimeout = defaultCollectionTimeout
	}
	if c.containerLimit <= 0 {
		c.containerLimit = defaultContainerLimit
	}
	snapshot := protocol.DockerSnapshot{CollectedAt: time.Now().Unix(), Containers: []protocol.ContainerInfo{}}
	client := c.client
	if client == nil {
		if _, err := os.Stat(c.socketPath); err != nil {
			if errors.Is(err, os.ErrNotExist) {
				snapshot.Error = "Docker socket not found"
			} else {
				snapshot.Error = "Docker socket unavailable: " + err.Error()
			}
			return snapshot
		}
		client = newSocketClient(c.socketPath, c.statsTimeout)
	}
	ctx, cancel := context.WithTimeout(context.Background(), c.collectionTimeout)
	defer cancel()
	version, err := client.Version(ctx)
	if err != nil {
		snapshot.Error = "Docker unavailable: " + err.Error()
		return snapshot
	}
	snapshot.Available = true
	snapshot.Version = version
	containers, err := client.ListContainers(ctx, c.containerLimit)
	if err != nil {
		snapshot.Error = "Docker containers unavailable: " + err.Error()
		return snapshot
	}

	// 并发采集容器信息
	type containerResult struct {
		info protocol.ContainerInfo
		err  string
	}

	results := make([]containerResult, len(containers))
	semaphore := make(chan struct{}, maxConcurrentStats) // 限制并发数
	var wg sync.WaitGroup

	for i, item := range containers {
		if ctx.Err() != nil {
			results[i].err = "Collection timeout"
			continue
		}

		wg.Add(1)
		go func(idx int, item containerListItem) {
			defer wg.Done()
			semaphore <- struct{}{}        // 获取信号量
			defer func() { <-semaphore }() // 释放信号量

			container := protocol.ContainerInfo{
				ID:        shortID(item.ID),
				FullID:    item.ID,
				Name:      cleanName(firstName(item.Names)),
				Image:     item.Image,
				State:     item.State,
				Status:    item.Status,
				CreatedAt: item.Created,
			}

			// Inspect 元数据
			if inspect, err := client.InspectContainer(ctx, item.ID); err == nil {
				container.RestartCount = inspect.RestartCount
				container.StartedAt = parseDockerTime(inspect.State.StartedAt)
			} else if ctx.Err() == nil {
				results[idx].err = shortID(item.ID) + " inspect: " + err.Error()
			}

			// Stats 资源使用（仅运行中的容器）
			if ctx.Err() == nil && item.State == "running" {
				if stats, err := client.ContainerStats(ctx, item.ID); err == nil {
					applyStats(&container, stats)
				} else {
					if results[idx].err != "" {
						results[idx].err += "; "
					}
					results[idx].err += shortID(item.ID) + " stats: " + err.Error()
				}
			}

			results[idx].info = container
		}(i, item)
	}

	wg.Wait()

	// 收集结果和错误
	var errorsSeen []string
	for _, result := range results {
		if result.err != "" {
			errorsSeen = appendLimited(errorsSeen, result.err)
		}
		// 即使有错误也添加容器基础信息
		if result.info.ID != "" {
			snapshot.Containers = append(snapshot.Containers, result.info)
		}
	}

	if len(errorsSeen) > 0 {
		snapshot.Error = strings.Join(errorsSeen, "; ")
	}
	return snapshot
}

// ContainerLogs gets logs from a container (delegates to the underlying client)
func (c *Collector) ContainerLogs(ctx context.Context, id string, tail int, follow bool, timestamps bool) (io.ReadCloser, error) {
	client := c.client
	if client == nil {
		if _, err := os.Stat(c.socketPath); err != nil {
			return nil, fmt.Errorf("Docker socket unavailable: %w", err)
		}
		client = newSocketClient(c.socketPath, c.statsTimeout)
	}
	return client.ContainerLogs(ctx, id, tail, follow, timestamps)
}

func (c *Collector) SupportsContainerCreate() bool {
	if c == nil {
		return false
	}
	if c.client != nil {
		_, ok := c.client.(containerClientAPI)
		return ok
	}
	socketPath := c.socketPath
	if socketPath == "" {
		socketPath = defaultSocketPath
	}
	if _, err := os.Stat(socketPath); err != nil {
		return false
	}
	ctx, cancel := context.WithTimeout(context.Background(), containerCreateProbeTimeout)
	defer cancel()
	return supportsDockerEngine(ctx, newSocketClient(socketPath, containerCreateProbeTimeout))
}

func supportsDockerEngine(ctx context.Context, client interface {
	Version(context.Context) (string, error)
}) bool {
	version, err := client.Version(ctx)
	return err == nil && strings.TrimSpace(version) != ""
}

func (c *Collector) CreateContainer(ctx context.Context, request protocol.DockerContainerCreateRequest) (containerCreateResult, error) {
	if c == nil {
		return containerCreateResult{}, errors.New("Docker container creation unsupported")
	}
	client := c.client
	if client == nil {
		if _, err := os.Stat(c.socketPath); err != nil {
			return containerCreateResult{}, fmt.Errorf("Docker socket unavailable: %w", err)
		}
		client = newSocketClient(c.socketPath, c.statsTimeout)
	}
	containerClient, ok := client.(containerClientAPI)
	if !ok {
		return containerCreateResult{}, errors.New("Docker container creation unsupported")
	}
	return containerClient.CreateContainer(ctx, request)
}

type socketClient struct {
	httpClient          *http.Client
	httpClientNoTimeout *http.Client
	baseURL             string
}

func newSocketClient(socketPath string, timeout time.Duration) *socketClient {
	transport := &http.Transport{
		DialContext: func(ctx context.Context, network string, address string) (net.Conn, error) {
			var dialer net.Dialer
			return dialer.DialContext(ctx, "unix", socketPath)
		},
	}
	// Clone transport for no-timeout client
	transportNoTimeout := &http.Transport{
		DialContext: func(ctx context.Context, network string, address string) (net.Conn, error) {
			var dialer net.Dialer
			return dialer.DialContext(ctx, "unix", socketPath)
		},
	}
	return &socketClient{
		httpClient:          &http.Client{Transport: transport, Timeout: timeout},
		httpClientNoTimeout: &http.Client{Transport: transportNoTimeout, Timeout: 0}, // No timeout for streaming
		baseURL:             "http://docker",
	}
}

func (c *socketClient) Version(ctx context.Context) (string, error) {
	var response struct {
		Version string `json:"Version"`
	}
	if err := c.getJSON(ctx, "/version", &response); err != nil {
		return "", err
	}
	return response.Version, nil
}

func (c *socketClient) ListContainers(ctx context.Context, limit int) ([]containerListItem, error) {
	path := "/containers/json?all=1"
	if limit > 0 {
		path += fmt.Sprintf("&limit=%d", limit)
	}
	var response []containerListItem
	if err := c.getJSON(ctx, path, &response); err != nil {
		return nil, err
	}
	if limit > 0 && len(response) > limit {
		response = response[:limit]
	}
	return response, nil
}

func (c *socketClient) InspectContainer(ctx context.Context, id string) (containerInspect, error) {
	var response containerInspect
	if err := c.getJSON(ctx, "/containers/"+id+"/json", &response); err != nil {
		return containerInspect{}, err
	}
	return response, nil
}

func (c *socketClient) ContainerStats(ctx context.Context, id string) (containerStats, error) {
	var response containerStats
	if err := c.getJSON(ctx, "/containers/"+id+"/stats?stream=false", &response); err != nil {
		return containerStats{}, err
	}
	return response, nil
}

func (c *socketClient) ContainerLogs(ctx context.Context, id string, tail int, follow bool, timestamps bool) (io.ReadCloser, error) {
	path := fmt.Sprintf("/containers/%s/logs?stdout=1&stderr=1", id)
	if tail > 0 {
		path += fmt.Sprintf("&tail=%d", tail)
	} else {
		path += "&tail=all"
	}
	if follow {
		path += "&follow=1"
	}
	if timestamps {
		path += "&timestamps=1"
	}

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+path, nil)
	if err != nil {
		return nil, err
	}

	// Important: Set Connection: close to prevent HTTP/1.1 keep-alive issues with streaming
	request.Header.Set("Connection", "close")

	// Use no-timeout client for streaming logs
	response, err := c.httpClientNoTimeout.Do(request)
	if err != nil {
		return nil, err
	}

	if response.StatusCode < 200 || response.StatusCode >= 300 {
		response.Body.Close()
		return nil, fmt.Errorf("Docker API status %d", response.StatusCode)
	}

	return response.Body, nil
}

func (c *socketClient) getJSON(ctx context.Context, path string, target any) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+path, nil)
	if err != nil {
		return err
	}
	response, err := c.httpClient.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("Docker API status %d", response.StatusCode)
	}
	return json.NewDecoder(response.Body).Decode(target)
}

type containerListItem struct {
	ID      string   `json:"Id"`
	Names   []string `json:"Names"`
	Image   string   `json:"Image"`
	State   string   `json:"State"`
	Status  string   `json:"Status"`
	Created int64    `json:"Created"`
}

type containerInspect struct {
	Name         string                `json:"Name"`
	RestartCount int                   `json:"RestartCount"`
	State        containerInspectState `json:"State"`
}

type containerInspectState struct {
	StartedAt string `json:"StartedAt"`
}

type containerStats struct {
	CPUStats    cpuStats                `json:"cpu_stats"`
	PreCPUStats cpuStats                `json:"precpu_stats"`
	MemoryStats memoryStats             `json:"memory_stats"`
	Networks    map[string]networkStats `json:"networks"`
}

type cpuStats struct {
	CPUUsage       cpuUsage `json:"cpu_usage"`
	SystemCPUUsage uint64   `json:"system_cpu_usage"`
	OnlineCPUs     uint32   `json:"online_cpus"`
}

type cpuUsage struct {
	TotalUsage  uint64   `json:"total_usage"`
	PercpuUsage []uint64 `json:"percpu_usage"`
}

type memoryStats struct {
	Usage uint64 `json:"usage"`
	Limit uint64 `json:"limit"`
}

type networkStats struct {
	RxBytes uint64 `json:"rx_bytes"`
	TxBytes uint64 `json:"tx_bytes"`
}

func appendLimited(values []string, value string) []string {
	if len(values) >= 3 {
		return values
	}
	return append(values, value)
}

func applyStats(container *protocol.ContainerInfo, stats containerStats) {
	cpuDelta := stats.CPUStats.CPUUsage.TotalUsage - stats.PreCPUStats.CPUUsage.TotalUsage
	systemDelta := stats.CPUStats.SystemCPUUsage - stats.PreCPUStats.SystemCPUUsage
	cpuCount := stats.CPUStats.OnlineCPUs
	if cpuCount == 0 {
		cpuCount = uint32(len(stats.CPUStats.CPUUsage.PercpuUsage))
	}
	if cpuDelta > 0 && systemDelta > 0 && cpuCount > 0 {
		container.CPUUsage = float64(cpuDelta) / float64(systemDelta) * float64(cpuCount) * 100
	}
	container.MemoryUsage = stats.MemoryStats.Usage
	container.MemoryLimit = stats.MemoryStats.Limit
	if stats.MemoryStats.Limit > 0 {
		container.MemoryPercent = float64(stats.MemoryStats.Usage) / float64(stats.MemoryStats.Limit) * 100
	}
	keys := make([]string, 0, len(stats.Networks))
	for name := range stats.Networks {
		keys = append(keys, name)
	}
	sort.Strings(keys)
	for _, name := range keys {
		container.NetworkRX += stats.Networks[name].RxBytes
		container.NetworkTX += stats.Networks[name].TxBytes
	}
}

func shortID(id string) string {
	if len(id) <= 12 {
		return id
	}
	return id[:12]
}

func firstName(names []string) string {
	if len(names) == 0 {
		return ""
	}
	return names[0]
}

func cleanName(name string) string {
	return strings.TrimPrefix(name, "/")
}

func parseDockerTime(value string) int64 {
	value = strings.TrimSpace(value)
	if value == "" || strings.HasPrefix(value, "0001-01-01") {
		return 0
	}
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return 0
	}
	return parsed.Unix()
}
