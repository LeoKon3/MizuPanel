package alerting

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/mizupanel/mizupanel/internal/server/store"
)

type TaskTargetSummary struct {
	NodeName string `json:"node_name"`
	Status   string `json:"status"`
	ExitCode *int   `json:"exit_code,omitempty"`
}

// TaskPayload intentionally contains no script body, command output,
// environment, or notification credentials.
type TaskPayload struct {
	RunID             int64               `json:"run_id"`
	TaskName          string              `json:"task_name,omitempty"`
	ScriptName        string              `json:"script_name"`
	Trigger           string              `json:"trigger"`
	Status            string              `json:"status"`
	TotalTargets      int                 `json:"total_targets"`
	SuccessfulTargets int                 `json:"successful_targets"`
	FailedTargets     int                 `json:"failed_targets"`
	SkippedTargets    int                 `json:"skipped_targets"`
	DurationMS        int64               `json:"duration_ms"`
	Failures          []TaskTargetSummary `json:"failures"`
	FinishedAt        time.Time           `json:"finished_at"`
}

type TaskDeliveryResult = UptimeDeliveryResult

func (n *Notifier) DeliverTask(ctx context.Context, channels []store.NotificationChannel, payload TaskPayload) TaskDeliveryResult {
	return n.deliverChannels(ctx, channels, func(sendCtx context.Context, channel NotificationChannel) error {
		return n.SendTask(sendCtx, channel, payload)
	})
}

func (n *Notifier) SendTask(ctx context.Context, channel NotificationChannel, payload TaskPayload) error {
	switch channel.Type {
	case "webhook":
		return n.sendTaskWebhook(ctx, channel, payload)
	case "dingtalk":
		return n.sendTaskDingTalk(ctx, channel, payload)
	case "feishu":
		return n.sendTaskFeishu(ctx, channel, payload)
	default:
		return newDeliveryError(false, "unsupported channel type %s", channel.Type)
	}
}

func (n *Notifier) sendTaskWebhook(ctx context.Context, channel NotificationChannel, payload TaskPayload) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return newDeliveryError(false, "encode webhook payload failed")
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, channel.WebhookURL, bytes.NewReader(body))
	if err != nil {
		return newDeliveryError(false, "invalid webhook configuration")
	}
	request.Header.Set("Content-Type", "application/json")
	for key, value := range channel.Headers {
		request.Header.Set(key, value)
	}
	response, err := n.client.Do(request)
	if err != nil {
		return requestDeliveryError(err)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return statusDeliveryError("webhook", response.StatusCode)
	}
	return nil
}

func (n *Notifier) sendTaskDingTalk(ctx context.Context, channel NotificationChannel, payload TaskPayload) error {
	icon, title := taskStatusText(payload.Status)
	markdown := fmt.Sprintf(`### %s %s：%s

- **脚本**: %s
- **触发方式**: %s
- **执行状态**: %s
- **目标统计**: 成功 %d / 失败 %d / 跳过 %d / 总计 %d
- **耗时**: %d ms
- **完成时间**: %s`,
		icon, title, taskDisplayName(payload), payload.ScriptName, payload.Trigger, payload.Status,
		payload.SuccessfulTargets, payload.FailedTargets, payload.SkippedTargets, payload.TotalTargets,
		payload.DurationMS, payload.FinishedAt.Format("2006-01-02 15:04:05"))
	markdown += taskFailureMarkdown(payload.Failures, "\n- **失败节点**: ")
	body, err := json.Marshal(map[string]interface{}{
		"msgtype": "markdown",
		"markdown": map[string]string{
			"title": fmt.Sprintf("%s: %s", title, taskDisplayName(payload)),
			"text":  markdown,
		},
	})
	if err != nil {
		return newDeliveryError(false, "encode dingtalk payload failed")
	}
	webhookURL := channel.WebhookURL
	if channel.Secret != "" {
		timestamp := n.currentTime().UnixMilli()
		separator := "?"
		if strings.Contains(webhookURL, "?") {
			separator = "&"
		}
		webhookURL = fmt.Sprintf("%s%stimestamp=%d&sign=%s", webhookURL, separator, timestamp, url.QueryEscape(n.dingTalkSign(timestamp, channel.Secret)))
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, webhookURL, bytes.NewReader(body))
	if err != nil {
		return newDeliveryError(false, "invalid dingtalk configuration")
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := n.client.Do(request)
	if err != nil {
		return requestDeliveryError(err)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return statusDeliveryError("dingtalk", response.StatusCode)
	}
	var result struct {
		ErrCode int `json:"errcode"`
	}
	if err := json.NewDecoder(response.Body).Decode(&result); err == nil && result.ErrCode != 0 {
		return newDeliveryError(false, "dingtalk rejected request (code %d)", result.ErrCode)
	}
	return nil
}

func (n *Notifier) sendTaskFeishu(ctx context.Context, channel NotificationChannel, payload TaskPayload) error {
	icon, title := taskStatusText(payload.Status)
	content := fmt.Sprintf("**脚本**: %s\n**触发方式**: %s\n**执行状态**: %s\n**目标统计**: 成功 %d / 失败 %d / 跳过 %d / 总计 %d\n**耗时**: %d ms\n**完成时间**: %s",
		payload.ScriptName, payload.Trigger, payload.Status, payload.SuccessfulTargets,
		payload.FailedTargets, payload.SkippedTargets, payload.TotalTargets, payload.DurationMS,
		payload.FinishedAt.Format("2006-01-02 15:04:05"))
	content += taskFailureMarkdown(payload.Failures, "\n**失败节点**: ")
	card := map[string]interface{}{
		"msg_type": "interactive",
		"card": map[string]interface{}{
			"header": map[string]interface{}{
				"title":    map[string]interface{}{"tag": "plain_text", "content": fmt.Sprintf("%s %s - %s", icon, title, taskDisplayName(payload))},
				"template": taskStatusTemplate(payload.Status),
			},
			"elements": []map[string]interface{}{{"tag": "markdown", "content": content}},
		},
	}
	if channel.Secret != "" {
		timestamp := n.currentTime().Unix()
		card["timestamp"] = fmt.Sprintf("%d", timestamp)
		card["sign"] = n.feishuSign(timestamp, channel.Secret)
	}
	body, err := json.Marshal(card)
	if err != nil {
		return newDeliveryError(false, "encode feishu payload failed")
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, channel.WebhookURL, bytes.NewReader(body))
	if err != nil {
		return newDeliveryError(false, "invalid feishu configuration")
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := n.client.Do(request)
	if err != nil {
		return requestDeliveryError(err)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return statusDeliveryError("feishu", response.StatusCode)
	}
	var result struct {
		Code int `json:"code"`
	}
	if err := json.NewDecoder(response.Body).Decode(&result); err == nil && result.Code != 0 {
		return newDeliveryError(false, "feishu rejected request (code %d)", result.Code)
	}
	return nil
}

func taskDisplayName(payload TaskPayload) string {
	if payload.TaskName != "" {
		return payload.TaskName
	}
	return payload.ScriptName
}

func taskStatusText(status string) (string, string) {
	switch status {
	case "success":
		return "✅", "任务执行成功"
	case "partial":
		return "⚠️", "任务部分失败"
	case "skipped":
		return "⚠️", "任务已跳过"
	default:
		return "🔴", "任务执行失败"
	}
}

func taskStatusTemplate(status string) string {
	switch status {
	case "success":
		return "green"
	case "partial", "skipped":
		return "orange"
	default:
		return "red"
	}
}

func taskFailureMarkdown(failures []TaskTargetSummary, prefix string) string {
	if len(failures) == 0 {
		return ""
	}
	items := make([]string, 0, len(failures))
	for _, failure := range failures {
		item := fmt.Sprintf("%s (%s)", failure.NodeName, failure.Status)
		if failure.ExitCode != nil {
			item += fmt.Sprintf(" exit=%d", *failure.ExitCode)
		}
		items = append(items, item)
	}
	return prefix + strings.Join(items, "，")
}
