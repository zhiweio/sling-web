// notify.go delivers events to the in-app notification center, to the job's
// own webhook URLs (generic JSON) and to the configured global channels
// (WeCom / Feishu / DingTalk / Slack / generic, see notify_channels.go).
package server

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/flarco/g"
)

// Notifier sends notifications.
type Notifier struct {
	db     *DB
	vault  *Vault
	broker *Broker
	client *http.Client
}

func NewNotifier(db *DB, vault *Vault, broker *Broker) *Notifier {
	return &Notifier{
		db:     db,
		vault:  vault,
		broker: broker,
		client: &http.Client{Timeout: 10 * time.Second},
	}
}

// notifyCenter records an in-app notification and pushes it over SSE.
func (n *Notifier) notifyCenter(typ, level, title, body string, meta map[string]any) {
	metaJSON := ""
	if meta != nil {
		if b, err := json.Marshal(meta); err == nil {
			metaJSON = string(b)
		}
	}
	notif := Notification{
		Type:    typ,
		Level:   level,
		Title:   title,
		Body:    body,
		MetaJSON: metaJSON,
	}
	if err := n.db.Create(&notif).Error; err != nil {
		g.Debug("could not create notification: %s", err)
	}
	if n.broker != nil {
		n.broker.Publish(Event{Type: "notification", Payload: notif})
	}
}

// RunFinished notifies on a finished job run. Success notifications are
// opt-in (notify.on_success, default off); failures can be silenced with
// notify.on_failure=off.
func (n *Notifier) RunFinished(job *Job, snap Snapshot) {
	level, typ := "info", "run_success"
	if snap.Status == "error" || snap.Status == "terminated" {
		level, typ = "error", "run_failed"
	} else if snap.Status == "warning" {
		level = "warning"
	} else if snap.Status == "cancelled" {
		level, typ = "warning", "run_cancelled"
	}

	if typ == "run_success" && !n.settingBool("notify.on_success", false) {
		return
	}
	if typ == "run_failed" && !n.settingBool("notify.on_failure", true) {
		return
	}

	title := g.F("%s %s", job.Name, statusVerbCN(snap.Status))
	body := g.F("Rows: %d | Duration: %.1fs", snap.Rows, float64(snap.DurationMs)/1000)
	if snap.Error != "" {
		body += " | " + snap.Error
	}
	meta := map[string]any{
		"job_id": job.ID, "run_id": snap.RunID, "status": snap.Status,
	}
	n.notifyCenter(typ, level, title, body, meta)

	ev := &webhookEvent{
		Event: typ, Title: title, Body: body, Level: level,
		Meta: meta, At: time.Now().Format(time.RFC3339),
	}
	// job-level webhooks keep the flat JSON shape they always had
	n.fireJobWebhooks(job, map[string]any{
		"event":       typ,
		"job_id":      job.ID,
		"job_name":    job.Name,
		"run_id":      snap.RunID,
		"status":      snap.Status,
		"rows":        snap.Rows,
		"bytes":       snap.Bytes,
		"duration_ms": snap.DurationMs,
		"error":       snap.Error,
		"timestamp":   ev.At,
	})
	n.fireChannels(ev)
}

// JobAutoPaused notifies that the circuit breaker paused a job.
func (n *Notifier) JobAutoPaused(job *Job, failures int) {
	title := g.F("%s has been paused automatically", job.Name)
	body := g.F("%d consecutive failures. Fix the issue and resume the schedule.", failures)
	n.notifyCenter("paused", "error", title, body, map[string]any{"job_id": job.ID})
	at := time.Now().Format(time.RFC3339)
	ev := &webhookEvent{
		Event: "paused", Title: title, Body: body, Level: "error",
		Meta: map[string]any{"job_id": job.ID, "consecutive_failures": failures},
		At:   at,
	}
	n.fireJobWebhooks(job, map[string]any{
		"event":                "job_auto_paused",
		"job_id":               job.ID,
		"job_name":             job.Name,
		"consecutive_failures": failures,
		"timestamp":            at,
	})
	n.fireChannels(ev)
}

// System posts a system-level event (maintenance, backups) to the center and
// to channels subscribed to "system".
func (n *Notifier) System(level, title, body string) {
	n.notifyCenter("system", level, title, body, nil)
	n.fireChannels(&webhookEvent{
		Event: "system", Title: title, Body: body, Level: level,
		At: time.Now().Format(time.RFC3339),
	})
}

// fireJobWebhooks posts a flat JSON payload to the job's own webhook URLs
// (legacy per-job configuration, unchanged shape).
func (n *Notifier) fireJobWebhooks(job *Job, flat map[string]any) {
	if job == nil {
		return
	}
	for _, url := range job.Webhooks() {
		if url == "" {
			continue
		}
		go n.postWebhook(url, flat)
	}
}

// fireChannels delivers the event to all enabled global channels subscribed
// to it. Sending is best-effort and asynchronous.
func (n *Notifier) fireChannels(ev *webhookEvent) {
	if n.db == nil {
		return
	}
	var channels []NotifyChannel
	if err := n.db.Where("enabled = ?", true).Find(&channels).Error; err != nil {
		g.Debug("could not load notify channels: %s", err)
		return
	}
	for i := range channels {
		ch := channels[i]
		if !ch.SubscribesTo(ev.Event) {
			continue
		}
		go func() {
			secret := ""
			if n.vault != nil {
				secret, _ = n.vault.LoadSecret(channelSecretPath(ch.ID))
			}
			if err := n.sendToChannel(&ch, secret, ev); err != nil {
				g.Warn("notify channel %s (%s) failed: %s", ch.Name, ch.Provider, err)
			}
		}()
	}
}

// sendToChannel delivers ev to one channel synchronously, returning a
// human-readable error. Both transport failures and provider error codes
// (WeCom/Feishu/DingTalk return 200 with errcode in the body) are reported.
func (n *Notifier) sendToChannel(ch *NotifyChannel, secret string, ev *webhookEvent) error {
	body, err := buildChannelPayload(ch.Provider, ev, secret)
	if err != nil {
		return err
	}
	urlStr := ch.URL
	if ch.Provider == "dingtalk" && secret != "" {
		urlStr = signDingTalkURL(urlStr, secret)
	}
	req, err := http.NewRequest("POST", urlStr, bytes.NewReader(body))
	if err != nil {
		return g.Error("invalid webhook url")
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := n.client.Do(req)
	if err != nil {
		return g.Error("request failed: %s", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return g.Error("http status %d", resp.StatusCode)
	}
	var out struct {
		ErrCode int    `json:"errcode"` // wecom / dingtalk
		Code    int    `json:"code"`    // feishu
		ErrMsg  string `json:"errmsg"`
		Msg     string `json:"msg"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 4096)).Decode(&out); err == nil {
		if out.ErrCode != 0 {
			return g.Error("provider error %d: %s", out.ErrCode, out.ErrMsg)
		}
		if out.Code != 0 {
			return g.Error("provider error %d: %s", out.Code, out.Msg)
		}
	}
	return nil
}

func (n *Notifier) postWebhook(url string, payload map[string]any) {
	body, err := json.Marshal(payload)
	if err != nil {
		return
	}
	req, err := http.NewRequest("POST", url, bytes.NewReader(body))
	if err != nil {
		g.Debug("invalid webhook url: %s", err)
		return
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := n.client.Do(req)
	if err != nil {
		g.Debug("webhook %s failed: %s", url, err)
		return
	}
	resp.Body.Close()
}

// settingBool reads a boolean setting with a default.
func (n *Notifier) settingBool(key string, def bool) bool {
	switch strings.ToLower(n.db.GetSetting(key, "")) {
	case "true", "1", "yes", "on":
		return true
	case "false", "0", "no", "off":
		return false
	}
	return def
}

// statusVerbCN renders a friendly status verb (bilingual: the frontend
// re-renders statuses from codes; this is the webhook/plain-text form).
func statusVerbCN(status string) string {
	switch status {
	case "success":
		return "succeeded"
	case "error":
		return "failed"
	case "warning":
		return "finished with warnings"
	case "cancelled":
		return "was cancelled"
	case "terminated":
		return "was terminated"
	default:
		return "finished (" + status + ")"
	}
}
