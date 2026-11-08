# 界面截图

[返回 README](../README.md) · [English](screenshots.en.md)

拍摄版本 **v0.1.33**，日期 **2026-11-09**，统一视口 **1440 × 900**。中英文 README 共用这组当前中文界面的截图，均来自正式构建，不是 `8081` 设计样板或模拟 API 页面。

IP 地址、Provider 地址和私人会话内容已按需遮盖；不展示安装凭据或 kubeconfig 内容。没有为展示效果添加业务数据，空数据、禁用和离线状态均如实保留；各页面仍处于逐步统一视觉的过程中。


## 概览与明暗主题

<table>
  <tr>
    <td width="50%">
      <a href="../assets/screenshots/overview-light.png"><img src="../assets/screenshots/overview-light.png" alt="浅色概览：首页汇总真实状态、需要关注的对象和近期任务。" width="100%" /></a>
      <br /><sub>浅色概览：首页汇总真实状态、需要关注的对象和近期任务。</sub>
    </td>
    <td width="50%">
      <a href="../assets/screenshots/overview-dark.png"><img src="../assets/screenshots/overview-dark.png" alt="同一页面的深色主题，信息结构保持一致。" width="100%" /></a>
      <br /><sub>同一页面的深色主题，信息结构保持一致。</sub>
    </td>
  </tr>
</table>

概览为默认入口；主机、告警、拨测、应用服务和 K8s 的问题可直接进入对应管理页。没有任务记录时显示真实空状态。


## 主机工作区

<table>
  <tr>
    <td width="50%">
      <a href="../assets/screenshots/hosts.png"><img src="../assets/screenshots/hosts.png" alt="独立主机列表：稳定排序、状态与分组筛选、真实资源指标。" width="100%" /></a>
      <br /><sub>独立主机列表：稳定排序、状态与分组筛选、真实资源指标。</sub>
    </td>
    <td width="50%">
      <a href="../assets/screenshots/host-overview.png"><img src="../assets/screenshots/host-overview.png" alt="全宽主机概览：资源趋势、基础信息、网络与磁盘 I/O。" width="100%" /></a>
      <br /><sub>全宽主机概览：资源趋势、基础信息、网络与磁盘 I/O。</sub>
    </td>
  </tr>
  <tr>
    <td width="100%">
      <a href="../assets/screenshots/host-processes.png"><img src="../assets/screenshots/host-processes.png" alt="进程分区：搜索、排序和对齐的数值列。" width="100%" /></a>
      <br /><sub>进程分区：搜索、排序和对齐的数值列。</sub>
    </td>
  </tr>
</table>

IP 地址已遮盖。截图中的主机名称、状态和指标来自当前环境；没有执行终端命令、重启或其他主机变更。


## 应用服务

<table>
  <tr>
    <td width="50%">
      <a href="../assets/screenshots/application-services.png"><img src="../assets/screenshots/application-services.png" alt="真实空列表：当前环境尚未创建应用服务。" width="100%" /></a>
      <br /><sub>真实空列表：当前环境尚未创建应用服务。</sub>
    </td>
    <td width="50%">
      <a href="../assets/screenshots/service-editor.png"><img src="../assets/screenshots/service-editor.png" alt="资源分类编辑器：按类型选择、搜索和查看已选清单。" width="100%" /></a>
      <br /><sub>资源分类编辑器：按类型选择、搜索和查看已选清单。</sub>
    </td>
  </tr>
</table>

这里只打开创建窗口展示布局，没有提交创建或保存。服务详情的排障工作区与统一事件流需在创建逻辑服务后使用；不使用模拟数据冒充已配置服务。


## 告警

<table>
  <tr>
    <td width="50%">
      <a href="../assets/screenshots/alert-history.png"><img src="../assets/screenshots/alert-history.png" alt="告警历史与触发、恢复通知状态。" width="100%" /></a>
      <br /><sub>告警历史与触发、恢复通知状态。</sub>
    </td>
    <td width="50%">
      <a href="../assets/screenshots/alert-rule-list.png"><img src="../assets/screenshots/alert-rule-list.png" alt="现有规则列表：截图中的规则处于禁用状态。" width="100%" /></a>
      <br /><sub>现有规则列表：截图中的规则处于禁用状态。</sub>
    </td>
  </tr>
</table>

当前记录和规则如实展示；截图期间未启用规则、删除记录或发送通知。


## 任务中心与服务拨测

<table>
  <tr>
    <td width="50%">
      <a href="../assets/screenshots/task-center.png"><img src="../assets/screenshots/task-center.png" alt="计划任务、脚本库与执行记录入口；当前没有计划任务。" width="100%" /></a>
      <br /><sub>计划任务、脚本库与执行记录入口；当前没有计划任务。</sub>
    </td>
    <td width="50%">
      <a href="../assets/screenshots/uptime-monitoring.png"><img src="../assets/screenshots/uptime-monitoring.png" alt="HTTP、HTTPS、TCP 拨测入口；当前没有配置拨测。" width="100%" /></a>
      <br /><sub>HTTP、HTTPS、TCP 拨测入口；当前没有配置拨测。</sub>
    </td>
  </tr>
</table>

没有为了截图执行脚本、创建计划或触发探测。


## Kubernetes 与审计

<table>
  <tr>
    <td width="50%">
      <a href="../assets/screenshots/kubernetes-clusters.png"><img src="../assets/screenshots/kubernetes-clusters.png" alt="集群列表与真实离线原因，节点 IP 已遮盖。" width="100%" /></a>
      <br /><sub>集群列表与真实离线原因，节点 IP 已遮盖。</sub>
    </td>
    <td width="50%">
      <a href="../assets/screenshots/audit-log.png"><img src="../assets/screenshots/audit-log.png" alt="审计筛选入口，当前时间范围内没有审计记录。" width="100%" /></a>
      <br /><sub>审计筛选入口，当前时间范围内没有审计记录。</sub>
    </td>
  </tr>
</table>

当前 Kubernetes API 不可达，因此不沿用旧版的在线集群详情或资源创建截图。未创建 Kubernetes 资源，也未清理审计日志。


## 系统设置

<table>
  <tr>
    <td width="50%">
      <a href="../assets/screenshots/settings-general.png"><img src="../assets/screenshots/settings-general.png" alt="常规设置独立页面：指标保留时间与当前版本。" width="100%" /></a>
      <br /><sub>常规设置独立页面：指标保留时间与当前版本。</sub>
    </td>
    <td width="50%">
      <a href="../assets/screenshots/settings-models.png"><img src="../assets/screenshots/settings-models.png" alt="AI 模型页面：Provider 与子模型，Provider 地址已遮盖。" width="100%" /></a>
      <br /><sub>AI 模型页面：Provider 与子模型，Provider 地址已遮盖。</sub>
    </td>
  </tr>
</table>

常规设置与 AI 模型是互斥页面。模型页保留连接、能力检测和路由配置；不再展示已移除的 AI 自动执行策略。截图期间未修改配置或检测模型。


## AI 运维助手

<table>
  <tr>
    <td width="100%">
      <a href="../assets/screenshots/ai-assistant.png"><img src="../assets/screenshots/ai-assistant.png" alt="按需打开的覆盖式 AI 抽屉；已有会话内容已隐藏。" width="100%" /></a>
      <br /><sub>按需打开的覆盖式 AI 抽屉；已有会话内容已隐藏。</sub>
    </td>
  </tr>
</table>

只展示抽屉结构、模型选择和输入区，不发送 AI 消息、不执行工具操作。图中的“会话内容已隐藏”是截图脱敏说明，不是产品空状态。
