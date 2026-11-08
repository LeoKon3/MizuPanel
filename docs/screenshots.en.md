# Screenshots

[Back to README](../README.en.md) · [中文](screenshots.md)

Captured from the **v0.1.33** production build on **2026-11-09**, at **1440 × 900**. Both README languages use this same current Chinese-language interface capture set. These are real pages, not the `8081` design prototype or mock API fixtures.

IP addresses, the Provider endpoint, and private conversation content are redacted where needed; installation credentials and kubeconfig contents are omitted. No data was added for presentation. Empty, disabled, and offline states are shown as they appeared, and not every page has received the same depth of visual redesign.


## Overview And Themes

<table>
  <tr>
    <td width="50%">
      <a href="../assets/screenshots/overview-light.png"><img src="../assets/screenshots/overview-light.png" alt="Light overview: real status summaries, attention items, and recent tasks." width="100%" /></a>
      <br /><sub>Light overview: real status summaries, attention items, and recent tasks.</sub>
    </td>
    <td width="50%">
      <a href="../assets/screenshots/overview-dark.png"><img src="../assets/screenshots/overview-dark.png" alt="The same overview in dark mode, with the same information structure." width="100%" /></a>
      <br /><sub>The same overview in dark mode, with the same information structure.</sub>
    </td>
  </tr>
</table>

Overview is the default entry point, with links to the corresponding host, alert, probe, application-service, and Kubernetes pages. An empty run history is shown honestly.


## Host Workspace

<table>
  <tr>
    <td width="50%">
      <a href="../assets/screenshots/hosts.png"><img src="../assets/screenshots/hosts.png" alt="Separate host list: stable ordering, status/group filters, and live resource metrics." width="100%" /></a>
      <br /><sub>Separate host list: stable ordering, status/group filters, and live resource metrics.</sub>
    </td>
    <td width="50%">
      <a href="../assets/screenshots/host-overview.png"><img src="../assets/screenshots/host-overview.png" alt="Full-width host overview: resource trends, system information, network and disk I/O." width="100%" /></a>
      <br /><sub>Full-width host overview: resource trends, system information, network and disk I/O.</sub>
    </td>
  </tr>
  <tr>
    <td width="100%">
      <a href="../assets/screenshots/host-processes.png"><img src="../assets/screenshots/host-processes.png" alt="Process view: search, sorting, and aligned numeric columns." width="100%" /></a>
      <br /><sub>Process view: search, sorting, and aligned numeric columns.</sub>
    </td>
  </tr>
</table>

IP addresses are redacted. Host names, states, and metrics come from the running environment; no terminal commands, reboot, or other host changes were performed.


## Application Services

<table>
  <tr>
    <td width="50%">
      <a href="../assets/screenshots/application-services.png"><img src="../assets/screenshots/application-services.png" alt="Real empty list: this environment has no application services yet." width="100%" /></a>
      <br /><sub>Real empty list: this environment has no application services yet.</sub>
    </td>
    <td width="50%">
      <a href="../assets/screenshots/service-editor.png"><img src="../assets/screenshots/service-editor.png" alt="Resource editor: type-specific selection, search, and a cross-category selection list." width="100%" /></a>
      <br /><sub>Resource editor: type-specific selection, search, and a cross-category selection list.</sub>
    </td>
  </tr>
</table>

The create dialog was opened only to show its layout; nothing was created or saved. The incident workspace and unified event stream are available after a logical service is created. No mock data is presented as configured services.


## Alerts

<table>
  <tr>
    <td width="50%">
      <a href="../assets/screenshots/alert-history.png"><img src="../assets/screenshots/alert-history.png" alt="Alert history with trigger and recovery delivery states." width="100%" /></a>
      <br /><sub>Alert history with trigger and recovery delivery states.</sub>
    </td>
    <td width="50%">
      <a href="../assets/screenshots/alert-rule-list.png"><img src="../assets/screenshots/alert-rule-list.png" alt="Existing rules: the rule shown is disabled." width="100%" /></a>
      <br /><sub>Existing rules: the rule shown is disabled.</sub>
    </td>
  </tr>
</table>

Existing records and rules are shown as-is; no rules were enabled, records deleted, or notifications sent while capturing.


## Task Center And Uptime Monitoring

<table>
  <tr>
    <td width="50%">
      <a href="../assets/screenshots/task-center.png"><img src="../assets/screenshots/task-center.png" alt="Scheduled tasks, script library, and run-history entries; no schedules are configured." width="100%" /></a>
      <br /><sub>Scheduled tasks, script library, and run-history entries; no schedules are configured.</sub>
    </td>
    <td width="50%">
      <a href="../assets/screenshots/uptime-monitoring.png"><img src="../assets/screenshots/uptime-monitoring.png" alt="HTTP, HTTPS, and TCP monitoring entry point; no probes are configured." width="100%" /></a>
      <br /><sub>HTTP, HTTPS, and TCP monitoring entry point; no probes are configured.</sub>
    </td>
  </tr>
</table>

No scripts were run, schedules created, or probes triggered for these screenshots.


## Kubernetes And Audit

<table>
  <tr>
    <td width="50%">
      <a href="../assets/screenshots/kubernetes-clusters.png"><img src="../assets/screenshots/kubernetes-clusters.png" alt="Cluster list with the actual offline reason; the node IP is redacted." width="100%" /></a>
      <br /><sub>Cluster list with the actual offline reason; the node IP is redacted.</sub>
    </td>
    <td width="50%">
      <a href="../assets/screenshots/audit-log.png"><img src="../assets/screenshots/audit-log.png" alt="Audit filters; no events exist in the selected time window." width="100%" /></a>
      <br /><sub>Audit filters; no events exist in the selected time window.</sub>
    </td>
  </tr>
</table>

The configured Kubernetes API is currently unreachable, so old online-cluster and resource-creation screenshots are not reused. No Kubernetes resources were created and no audit events were cleaned up.


## System Settings

<table>
  <tr>
    <td width="50%">
      <a href="../assets/screenshots/settings-general.png"><img src="../assets/screenshots/settings-general.png" alt="Separate general settings view: metric retention and the current version." width="100%" /></a>
      <br /><sub>Separate general settings view: metric retention and the current version.</sub>
    </td>
    <td width="50%">
      <a href="../assets/screenshots/settings-models.png"><img src="../assets/screenshots/settings-models.png" alt="AI model settings: Providers and child models, with the Provider endpoint redacted." width="100%" /></a>
      <br /><sub>AI model settings: Providers and child models, with the Provider endpoint redacted.</sub>
    </td>
  </tr>
</table>

General settings and AI models are separate views. Model settings retain connections, capability checks, and routing; removed autonomous-execution policy controls are not shown. No settings or models were changed or tested during capture.


## AI Operations Assistant

<table>
  <tr>
    <td width="100%">
      <a href="../assets/screenshots/ai-assistant.png"><img src="../assets/screenshots/ai-assistant.png" alt="On-demand AI overlay drawer; existing conversation content is hidden." width="100%" /></a>
      <br /><sub>On-demand AI overlay drawer; existing conversation content is hidden.</sub>
    </td>
  </tr>
</table>

This capture shows the drawer, model selector, and composer without sending messages or executing tools. “会话内容已隐藏” is a screenshot redaction notice, not a product empty state.
