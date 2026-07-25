package docker

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"unicode"

	"github.com/mizupanel/mizupanel/internal/protocol"
)

const (
	dockerComposeProjectLabel = "com.docker.compose.project"
	maxDockerTargetLength     = 512
	maxDockerErrorBytes       = 32 * 1024
)

var imageReferencePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:/@-]*$`)
var dockerImageTagPattern = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9_.-]{0,127}$`)
var dockerImageNamePartPattern = regexp.MustCompile(`^[a-z0-9]+(?:[._-][a-z0-9]+)*$`)
var dockerImageDomainPattern = regexp.MustCompile(`^(?:localhost|(?:[a-z0-9](?:[a-z0-9-]*[a-z0-9])?)(?:\.[a-z0-9](?:[a-z0-9-]*[a-z0-9])?)*)(?::[0-9]{1,5})?$`)
var dockerImageDigestPattern = regexp.MustCompile(`^[a-z][a-z0-9]*(?:[+._-][a-z][a-z0-9]*)*:[a-f0-9]{32,}$`)

type resourceClientAPI interface {
	DiskUsage(context.Context) (dockerDiskUsageResponse, error)
	ListNetworks(context.Context) ([]dockerNetworkListItem, error)
	PullImage(context.Context, string) error
	RemoveImage(context.Context, string) error
	RemoveVolume(context.Context, string) error
	RemoveNetwork(context.Context, string) error
}

type dockerDiskUsageResponse struct {
	LayersSize *int64                   `json:"LayersSize"`
	Images     *[]dockerImageUsage      `json:"Images"`
	Containers *[]dockerContainerUsage  `json:"Containers"`
	Volumes    *[]dockerVolumeUsage     `json:"Volumes"`
	BuildCache *[]dockerBuildCacheUsage `json:"BuildCache"`
}

type dockerImageUsage struct {
	ID         string   `json:"Id"`
	RepoTags   []string `json:"RepoTags"`
	Created    int64    `json:"Created"`
	Size       *int64   `json:"Size"`
	SharedSize *int64   `json:"SharedSize"`
	Containers *int64   `json:"Containers"`
}

type dockerContainerUsage struct {
	SizeRW *int64 `json:"SizeRw"`
}

type dockerVolumeUsage struct {
	Name       string                 `json:"Name"`
	Driver     string                 `json:"Driver"`
	Mountpoint string                 `json:"Mountpoint"`
	Labels     map[string]string      `json:"Labels"`
	Scope      string                 `json:"Scope"`
	UsageData  *dockerVolumeUsageData `json:"UsageData"`
}

type dockerVolumeUsageData struct {
	Size     *int64 `json:"Size"`
	RefCount *int64 `json:"RefCount"`
}

type dockerBuildCacheUsage struct {
	Size *int64 `json:"Size"`
}

type dockerNetworkListItem struct {
	Name       string                            `json:"Name"`
	ID         string                            `json:"Id"`
	Scope      string                            `json:"Scope"`
	Driver     string                            `json:"Driver"`
	Internal   bool                              `json:"Internal"`
	Ingress    bool                              `json:"Ingress"`
	IPAM       dockerNetworkIPAM                 `json:"IPAM"`
	Containers map[string]dockerNetworkContainer `json:"Containers"`
}

type dockerNetworkIPAM struct {
	Config []dockerNetworkIPAMConfig `json:"Config"`
}

type dockerNetworkIPAMConfig struct {
	Subnet string `json:"Subnet"`
}

type dockerNetworkContainer struct {
	Name string `json:"Name"`
}

type dockerAPIStatusError struct {
	status  int
	message string
}

func (e *dockerAPIStatusError) Error() string {
	if e.message == "" {
		return fmt.Sprintf("Docker API status %d", e.status)
	}
	return fmt.Sprintf("Docker API status %d: %s", e.status, e.message)
}

func (c *Collector) DockerResources(ctx context.Context) (protocol.DockerResourceListResponse, error) {
	response := emptyDockerResourceListResponse()
	client := c.dockerResourceClient()
	usage, err := client.DiskUsage(ctx)
	if err != nil {
		if dockerResourceAPIUnsupported(err) {
			response.Supported = false
		}
		return response, fmt.Errorf("读取 Docker 磁盘占用失败: %w", err)
	}
	networks, err := client.ListNetworks(ctx)
	if err != nil {
		if dockerResourceAPIUnsupported(err) {
			response.Supported = false
		}
		return response, fmt.Errorf("读取 Docker 网络失败: %w", err)
	}
	response.Success = true
	response.Supported = true
	response.Usage = mapDockerDiskUsage(usage)
	response.Images = mapDockerImages(usage.Images)
	response.Volumes = mapDockerVolumes(usage.Volumes)
	response.Networks = mapDockerNetworks(networks)
	return response, nil
}

func (c *Collector) DockerResourceAction(ctx context.Context, resourceType, resourceID, action string) error {
	resourceType = strings.TrimSpace(resourceType)
	resourceID = strings.TrimSpace(resourceID)
	action = strings.TrimSpace(action)
	if err := validateDockerResourceAction(resourceType, resourceID, action); err != nil {
		return err
	}
	client := c.dockerResourceClient()
	if resourceType == "image" && action == "pull" {
		return client.PullImage(ctx, resourceID)
	}

	switch resourceType {
	case "image":
		usage, err := client.DiskUsage(ctx)
		if err != nil {
			return fmt.Errorf("删除前检查镜像失败: %w", err)
		}
		image, ok := findDockerImage(usage.Images, resourceID)
		if !ok {
			return errors.New("镜像不存在或已经被删除")
		}
		if image.Containers == nil {
			return errors.New("无法确认镜像是否被容器使用，已拒绝删除")
		}
		if *image.Containers > 0 {
			return errors.New("镜像仍被容器引用，无法删除")
		}
		return client.RemoveImage(ctx, image.ID)
	case "volume":
		usage, err := client.DiskUsage(ctx)
		if err != nil {
			return fmt.Errorf("删除前检查数据卷失败: %w", err)
		}
		volume, ok := findDockerVolume(usage.Volumes, resourceID)
		if !ok {
			return errors.New("数据卷不存在或已经被删除")
		}
		if volume.UsageData == nil || volume.UsageData.RefCount == nil {
			return errors.New("无法确认数据卷引用状态，已拒绝删除")
		}
		if *volume.UsageData.RefCount != 0 {
			return errors.New("数据卷仍被容器引用，无法删除")
		}
		return client.RemoveVolume(ctx, volume.Name)
	case "network":
		networks, err := client.ListNetworks(ctx)
		if err != nil {
			return fmt.Errorf("删除前检查网络失败: %w", err)
		}
		network, ok := findDockerNetwork(networks, resourceID)
		if !ok {
			return errors.New("网络不存在或已经被删除")
		}
		if protectedDockerNetwork(network.Name, network.Ingress) {
			return errors.New("Docker 系统网络受保护，无法删除")
		}
		if len(network.Containers) > 0 {
			return errors.New("网络仍有容器连接，无法删除")
		}
		return client.RemoveNetwork(ctx, network.ID)
	default:
		return errors.New("不支持的 Docker 资源类型")
	}
}

func (c *Collector) dockerResourceClient() resourceClientAPI {
	if c.resourceClient != nil {
		return c.resourceClient
	}
	if client, ok := c.client.(resourceClientAPI); ok {
		return client
	}
	socketPath := c.socketPath
	if socketPath == "" {
		socketPath = defaultSocketPath
	}
	timeout := c.statsTimeout
	if timeout <= 0 {
		timeout = defaultStatsTimeout
	}
	return newSocketClient(socketPath, timeout)
}

func emptyDockerResourceListResponse() protocol.DockerResourceListResponse {
	return protocol.DockerResourceListResponse{
		Type:      protocol.MessageTypeDockerResourceListResponse,
		Images:    []protocol.DockerImage{},
		Volumes:   []protocol.DockerVolume{},
		Networks:  []protocol.DockerNetwork{},
		Supported: true,
	}
}

func mapDockerDiskUsage(raw dockerDiskUsageResponse) protocol.DockerDiskUsage {
	result := protocol.DockerDiskUsage{ImageLayers: nonNegativeInt64(raw.LayersSize)}
	if raw.Containers != nil {
		result.ContainerWritable = int64Pointer(sumKnownSizes(len(*raw.Containers), func(index int) *int64 { return (*raw.Containers)[index].SizeRW }))
	}
	if raw.Volumes != nil {
		result.Volumes = int64Pointer(sumKnownSizes(len(*raw.Volumes), func(index int) *int64 {
			if (*raw.Volumes)[index].UsageData == nil {
				return nil
			}
			return (*raw.Volumes)[index].UsageData.Size
		}))
	}
	if raw.BuildCache != nil {
		result.BuildCache = int64Pointer(sumKnownSizes(len(*raw.BuildCache), func(index int) *int64 { return (*raw.BuildCache)[index].Size }))
	}
	return result
}

func mapDockerImages(raw *[]dockerImageUsage) []protocol.DockerImage {
	if raw == nil {
		return []protocol.DockerImage{}
	}
	images := make([]protocol.DockerImage, 0, len(*raw))
	for _, image := range *raw {
		tags := append([]string(nil), image.RepoTags...)
		if tags == nil {
			tags = []string{}
		}
		sort.Strings(tags)
		images = append(images, protocol.DockerImage{
			ID:         dockerResourceShortID(image.ID),
			FullID:     image.ID,
			Tags:       tags,
			Size:       nonNegativeInt64(image.Size),
			SharedSize: nonNegativeInt64(image.SharedSize),
			CreatedAt:  image.Created,
			Containers: nonNegativeInt64(image.Containers),
		})
	}
	sort.Slice(images, func(i, j int) bool {
		return dockerImageSortKey(images[i]) < dockerImageSortKey(images[j])
	})
	return images
}

func mapDockerVolumes(raw *[]dockerVolumeUsage) []protocol.DockerVolume {
	if raw == nil {
		return []protocol.DockerVolume{}
	}
	volumes := make([]protocol.DockerVolume, 0, len(*raw))
	for _, volume := range *raw {
		var size, refCount *int64
		if volume.UsageData != nil {
			size = nonNegativeInt64(volume.UsageData.Size)
			refCount = nonNegativeInt64(volume.UsageData.RefCount)
		}
		volumes = append(volumes, protocol.DockerVolume{
			Name:           volume.Name,
			Driver:         volume.Driver,
			Scope:          volume.Scope,
			Mountpoint:     volume.Mountpoint,
			ComposeProject: volume.Labels[dockerComposeProjectLabel],
			Size:           size,
			RefCount:       refCount,
		})
	}
	sort.Slice(volumes, func(i, j int) bool { return strings.ToLower(volumes[i].Name) < strings.ToLower(volumes[j].Name) })
	return volumes
}

func mapDockerNetworks(raw []dockerNetworkListItem) []protocol.DockerNetwork {
	networks := make([]protocol.DockerNetwork, 0, len(raw))
	for _, network := range raw {
		subnets := make([]string, 0, len(network.IPAM.Config))
		for _, config := range network.IPAM.Config {
			if subnet := strings.TrimSpace(config.Subnet); subnet != "" {
				subnets = append(subnets, subnet)
			}
		}
		sort.Strings(subnets)
		containers := make([]string, 0, len(network.Containers))
		for id, container := range network.Containers {
			name := strings.TrimSpace(container.Name)
			if name == "" {
				name = dockerResourceShortID(id)
			}
			containers = append(containers, name)
		}
		sort.Strings(containers)
		networks = append(networks, protocol.DockerNetwork{
			ID:         dockerResourceShortID(network.ID),
			FullID:     network.ID,
			Name:       network.Name,
			Driver:     network.Driver,
			Scope:      network.Scope,
			Subnets:    subnets,
			Containers: containers,
			Internal:   network.Internal,
			Ingress:    network.Ingress,
			Protected:  protectedDockerNetwork(network.Name, network.Ingress),
		})
	}
	sort.Slice(networks, func(i, j int) bool { return strings.ToLower(networks[i].Name) < strings.ToLower(networks[j].Name) })
	return networks
}

func (c *socketClient) DiskUsage(ctx context.Context) (dockerDiskUsageResponse, error) {
	var response dockerDiskUsageResponse
	if err := c.getResourceJSON(ctx, "/system/df", &response); err != nil {
		return dockerDiskUsageResponse{}, err
	}
	return response, nil
}

func (c *socketClient) ListNetworks(ctx context.Context) ([]dockerNetworkListItem, error) {
	var response []dockerNetworkListItem
	if err := c.getResourceJSON(ctx, "/networks", &response); err != nil {
		return nil, err
	}
	if response == nil {
		response = []dockerNetworkListItem{}
	}
	// The Docker Engine's GET /networks response is a summary and does not
	// include the Containers map. Inspect every network before exposing it or
	// relying on it for a deletion safety check.
	for index := range response {
		identifier := response[index].ID
		if identifier == "" {
			identifier = response[index].Name
		}
		var network dockerNetworkListItem
		if err := c.getResourceJSON(ctx, "/networks/"+url.PathEscape(identifier), &network); err != nil {
			return nil, err
		}
		response[index] = network
	}
	return response, nil
}

func (c *socketClient) PullImage(ctx context.Context, imageReference string) error {
	values := url.Values{}
	values.Set("fromImage", imageReference)
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/images/create?"+values.Encode(), nil)
	if err != nil {
		return err
	}
	response, err := c.httpClientNoTimeout.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return dockerAPIResponseError(response)
	}
	scanner := bufio.NewScanner(response.Body)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var event struct {
			Error       string `json:"error"`
			ErrorDetail struct {
				Message string `json:"message"`
			} `json:"errorDetail"`
		}
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			return errors.New("Docker 返回了无法识别的镜像拉取结果")
		}
		if event.ErrorDetail.Message != "" {
			return errors.New(event.ErrorDetail.Message)
		}
		if event.Error != "" {
			return errors.New(event.Error)
		}
	}
	return scanner.Err()
}

func (c *socketClient) RemoveImage(ctx context.Context, id string) error {
	return c.deleteDockerResource(ctx, "/images/"+url.PathEscape(id))
}

func (c *socketClient) RemoveVolume(ctx context.Context, name string) error {
	return c.deleteDockerResource(ctx, "/volumes/"+url.PathEscape(name))
}

func (c *socketClient) RemoveNetwork(ctx context.Context, id string) error {
	return c.deleteDockerResource(ctx, "/networks/"+url.PathEscape(id))
}

func (c *socketClient) getResourceJSON(ctx context.Context, path string, target any) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+path, nil)
	if err != nil {
		return err
	}
	response, err := c.httpClientNoTimeout.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return dockerAPIResponseError(response)
	}
	return json.NewDecoder(response.Body).Decode(target)
}

func (c *socketClient) deleteDockerResource(ctx context.Context, path string) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodDelete, c.baseURL+path, nil)
	if err != nil {
		return err
	}
	response, err := c.httpClientNoTimeout.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return dockerAPIResponseError(response)
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, maxDockerErrorBytes))
	return nil
}

func dockerAPIResponseError(response *http.Response) error {
	body, _ := io.ReadAll(io.LimitReader(response.Body, maxDockerErrorBytes))
	var payload struct {
		Message string `json:"message"`
	}
	message := ""
	if json.Unmarshal(body, &payload) == nil {
		message = strings.TrimSpace(payload.Message)
	}
	if message == "" {
		message = strings.TrimSpace(string(body))
	}
	if message == "" {
		return &dockerAPIStatusError{status: response.StatusCode}
	}
	return &dockerAPIStatusError{status: response.StatusCode, message: message}
}

func dockerResourceAPIUnsupported(err error) bool {
	var statusError *dockerAPIStatusError
	return errors.As(err, &statusError) && (statusError.status == http.StatusNotFound || statusError.status == http.StatusNotImplemented)
}

func validateDockerResourceAction(resourceType, resourceID, action string) error {
	if resourceID == "" {
		return errors.New("Docker 资源标识不能为空")
	}
	if len(resourceID) > maxDockerTargetLength || strings.ContainsAny(resourceID, "\r\n\x00") || strings.IndexFunc(resourceID, unicode.IsControl) >= 0 {
		return errors.New("Docker 资源标识不合法")
	}
	switch resourceType {
	case "image":
		if action != "pull" && action != "remove" {
			return errors.New("不支持的镜像操作")
		}
		if action == "pull" && !validDockerImageReference(resourceID) {
			return errors.New("镜像引用格式不合法")
		}
	case "volume", "network":
		if action != "remove" {
			return errors.New("该资源只支持删除操作")
		}
	default:
		return errors.New("不支持的 Docker 资源类型")
	}
	return nil
}

func validDockerImageReference(reference string) bool {
	if len(reference) > 255 || !imageReferencePattern.MatchString(reference) || strings.Contains(reference, "://") {
		return false
	}
	nameAndTag, digest, hasDigest := strings.Cut(reference, "@")
	if hasDigest {
		if digest == "" || strings.Contains(digest, "@") || !dockerImageDigestPattern.MatchString(digest) {
			return false
		}
	}
	lastSlash := strings.LastIndex(nameAndTag, "/")
	name := nameAndTag
	if tagIndex := strings.LastIndex(nameAndTag, ":"); tagIndex > lastSlash {
		tag := nameAndTag[tagIndex+1:]
		if !dockerImageTagPattern.MatchString(tag) {
			return false
		}
		name = nameAndTag[:tagIndex]
	}
	if name == "" {
		return false
	}
	parts := strings.Split(name, "/")
	if len(parts) == 0 {
		return false
	}
	pathStart := 0
	if len(parts) > 1 && (parts[0] == "localhost" || strings.ContainsAny(parts[0], ".:")) {
		if !dockerImageDomainPattern.MatchString(parts[0]) {
			return false
		}
		pathStart = 1
	}
	for _, part := range parts[pathStart:] {
		if !dockerImageNamePartPattern.MatchString(part) {
			return false
		}
	}
	return pathStart < len(parts)
}

func findDockerImage(images *[]dockerImageUsage, id string) (dockerImageUsage, bool) {
	if images == nil {
		return dockerImageUsage{}, false
	}
	for _, image := range *images {
		if image.ID == id || dockerResourceShortID(image.ID) == id {
			return image, true
		}
		for _, tag := range image.RepoTags {
			if tag == id {
				return image, true
			}
		}
	}
	return dockerImageUsage{}, false
}

func findDockerVolume(volumes *[]dockerVolumeUsage, name string) (dockerVolumeUsage, bool) {
	if volumes == nil {
		return dockerVolumeUsage{}, false
	}
	for _, volume := range *volumes {
		if volume.Name == name {
			return volume, true
		}
	}
	return dockerVolumeUsage{}, false
}

func findDockerNetwork(networks []dockerNetworkListItem, id string) (dockerNetworkListItem, bool) {
	for _, network := range networks {
		if network.ID == id || dockerResourceShortID(network.ID) == id || network.Name == id {
			return network, true
		}
	}
	return dockerNetworkListItem{}, false
}

func protectedDockerNetwork(name string, ingress bool) bool {
	if ingress {
		return true
	}
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "bridge", "host", "none":
		return true
	default:
		return false
	}
}

func dockerResourceShortID(id string) string {
	id = strings.TrimPrefix(id, "sha256:")
	return shortID(id)
}

func dockerImageSortKey(image protocol.DockerImage) string {
	if len(image.Tags) > 0 {
		return strings.ToLower(image.Tags[0])
	}
	return image.ID
}

func nonNegativeInt64(value *int64) *int64 {
	if value == nil || *value < 0 {
		return nil
	}
	copy := *value
	return &copy
}

func int64Pointer(value int64, known bool) *int64 {
	if !known {
		return nil
	}
	return &value
}

func sumKnownSizes(length int, value func(int) *int64) (int64, bool) {
	var total int64
	for index := 0; index < length; index++ {
		size := value(index)
		if size == nil || *size < 0 {
			return 0, false
		}
		total += *size
	}
	return total, true
}
