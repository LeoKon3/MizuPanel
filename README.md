<p align="center">
  <img src="assets/mizupanel-banner.svg" alt="MizuPanel banner" width="100%" />
</p>

<h1 align="center">MizuPanel</h1>

<p align="center">
  轻量级自托管运维面板，用一个干净的控制台管理主机、Docker、告警和 Kubernetes 资源。
</p>

<p align="center">
  中文 · <a href="README.en.md">English</a>
</p>

<p align="center">
  <a href="https://go.dev/"><img alt="Go" src="https://img.shields.io/badge/Go-1.24-00ADD8?logo=go&logoColor=white"></a>
  <a href="https://react.dev/"><img alt="React" src="https://img.shields.io/badge/React-UI-61DAFB?logo=react&logoColor=0F172A"></a>
  <a href="https://vite.dev/"><img alt="Vite" src="https://img.shields.io/badge/Vite-build-646CFF?logo=vite&logoColor=white"></a>
  <a href="https://www.sqlite.org/"><img alt="SQLite" src="https://img.shields.io/badge/SQLite-default-003B57?logo=sqlite&logoColor=white"></a>
  <a href="https://www.docker.com/"><img alt="Docker" src="https://img.shields.io/badge/Docker-compose-2496ED?logo=docker&logoColor=white"></a>
  <img alt="Version" src="https://img.shields.io/badge/version-0.1.0-14B8A6">
</p>

<p align="center">
  <a href="assets/screenshots/dashboard.png">
    <img src="assets/screenshots/dashboard.png" alt="MizuPanel dashboard" width="92%" />
  </a>
</p>

<p align="center">
  Server + Dashboard + Agent 架构。Agent 主动连接 Server，上报指标并承载允许的运维操作；适合个人服务器、家庭实验室、小型主机集群和轻量 Kubernetes 管理。
</p>

<table>
  <tr>
    <td width="25%">
      <a href="assets/screenshots/host-detail.png"><img src="assets/screenshots/host-detail.png" alt="主机详情" width="100%" /></a>
    </td>
    <td width="25%">
      <a href="assets/screenshots/k8s-detail.png"><img src="assets/screenshots/k8s-detail.png" alt="Kubernetes 详情" width="100%" /></a>
    </td>
    <td width="25%">
      <a href="assets/screenshots/k8s-create-resource.png"><img src="assets/screenshots/k8s-create-resource.png" alt="创建 Kubernetes 资源" width="100%" /></a>
    </td>
    <td width="25%">
      <a href="assets/screenshots/alerts.png"><img src="assets/screenshots/alerts.png" alt="告警中心" width="100%" /></a>
    </td>
  </tr>
</table>

<p align="center"><strong>功能介绍</strong></p>

<table>
  <tr>
    <td width="33%"><strong>主机监控</strong><br /><sub>节点状态、CPU、内存、磁盘、网络、负载、历史趋势。</sub></td>
    <td width="33%"><strong>主机运维</strong><br /><sub>进程、Docker 容器、容器日志、文件管理、Web 终端、Agent 管理。</sub></td>
    <td width="33%"><strong>告警中心</strong><br /><sub>指标规则、持续时间判断、活跃告警、历史告警、手动收敛。</sub></td>
  </tr>
  <tr>
    <td width="33%"><strong>Kubernetes 管理</strong><br /><sub>集群接入、资源概览、Namespace、Node、Pod、Workload、Service、Ingress。</sub></td>
    <td width="33%"><strong>K8s 诊断</strong><br /><sub>Pod 日志、Events、Describe、YAML 查看与编辑、资源操作。</sub></td>
    <td width="33%"><strong>资源创建</strong><br /><sub>Deployment、Pod、Service、Ingress、ConfigMap、Secret、PVC、Job、CronJob。</sub></td>
  </tr>
</table>

<strong>Release 包部署运行</strong>

优先使用 Release 包部署。按 Server 所在机器架构选择构建目标：

```bash
# x86_64 / amd64
make package-linux-amd64

# ARM64 / aarch64
make package-linux-arm64
```

解压发布包并准备本机配置：

```bash
tar -xzf dist/mizupanel-linux-amd64.tar.gz
cd mizupanel-linux-amd64
cp server.example.yaml server.yaml
```

如果 Agent 会从其他机器访问面板，建议先在 `server.yaml` 里设置面板地址：

```yaml
server:
  listen: ":8080"
  public_url: "http://你的服务器IP:8080"
```

启动 Server：

```bash
./mizupanel-server -config server.yaml
```

打开 `http://你的服务器IP:8080`，进入 Dashboard 后点击 **添加服务器**，复制 Linux 或 Windows Agent 安装命令到目标主机执行。

发布包内已经包含 Web 静态资源、安装脚本和 Agent 下载文件。Docker、MySQL、管理员认证、systemd 托管和 Token 模型等细节请看 [配置部署文档](docs/configuration.md)，更多界面可以查看 [完整截图](docs/screenshots.md)。

<sub>特别感谢 <a href="https://linux.do/">Linux.do</a> 社区的反馈、讨论和启发。</sub>
