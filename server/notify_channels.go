// notify_channels.go manages global outbound notification channels (WeCom /
// Feishu / DingTalk / Slack bot webhooks and generic JSON receivers): CRUD
// APIs, credential masking, provider payload formats and the test-send
// endpoint. Signing secrets live in the vault; webhook URLs may embed keys
// (WeCom) or carry them in the path (Feishu/Slack) and are masked on every
// API read — plaintext never leaves the server.
package server

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/flarco/g"
)

// notifyProviders are the supported channel providers.
var notifyProviders = map[string]bool{
	"wecom":    true,
	"feishu":   true,
	"dingtalk": true,
	"slack":    true,
	"generic":  true,
}

// notifyEventTypes are the subscribable event types (empty subscription = all).
var notifyEventTypes = []string{"run_success", "run_failed", "run_cancelled", "paused", "system"}

// channelSecretPath is the vault path of a channel's signing secret
// (DingTalk signed bots / Feishu signature verification).
func channelSecretPath(id int64) string {
	return g.F("notify/channel/%d/secret", id)
}

// SubscribesTo reports whether the channel wants this event type; an empty
// events list subscribes to everything.
func (c NotifyChannel) SubscribesTo(event string) bool {
	events := c.Events()
	if len(events) == 0 {
		return true
	}
	for _, e := range events {
		if e == event || e == "*" {
			return true
		}
	}
	return false
}

// channelView is the API projection of a NotifyChannel: URL masked, secret
// presence only.
type channelView struct {
	ID        int64     `json:"id"`
	Name      string    `json:"name"`
	Provider  string    `json:"provider"`
	URL       string    `json:"url"` // masked
	HasSecret bool      `json:"has_secret"`
	Events    []string  `json:"events"`
	Enabled   bool      `json:"enabled"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

func (a *App) channelView(c *NotifyChannel) channelView {
	v := channelView{
		ID:        c.ID,
		Name:      c.Name,
		Provider:  c.Provider,
		URL:       maskChannelURL(c.Provider, c.URL),
		HasSecret: false,
		Events:    c.Events(),
		Enabled:   c.Enabled,
		CreatedAt: c.CreatedAt,
		UpdatedAt: c.UpdatedAt,
	}
	if v.Events == nil {
		v.Events = []string{}
	}
	if a.Vault != nil {
		if s, err := a.Vault.LoadSecret(channelSecretPath(c.ID)); err == nil && s != "" {
			v.HasSecret = true
		}
	}
	return v
}

// maskChannelURL hides credentials: query values are always masked; for
// providers whose secret rides in the path (feishu/slack/generic) the last
// path segment is masked too.
func maskChannelURL(provider, raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return "***"
	}
	q := u.Query()
	if len(q) > 0 {
		for k := range q {
			q.Set(k, "***")
		}
		u.RawQuery = q.Encode()
	}
	switch provider {
	case "feishu", "slack", "generic":
		if segments := strings.Split(u.Path, "/"); len(segments) > 1 {
			segments[len(segments)-1] = "***"
			u.Path = strings.Join(segments, "/")
		}
	}
	return u.String()
}

type channelInput struct {
	Name     string   `json:"name"`
	Provider string   `json:"provider"`
	URL      string   `json:"url"`
	Secret   string   `json:"secret,omitempty"`
	Events   []string `json:"events,omitempty"`
	Enabled  *bool    `json:"enabled,omitempty"`
}

func (a *App) handleNotifyChannelsList(w http.ResponseWriter, r *http.Request) {
	var channels []NotifyChannel
	if err := a.DB.Order("id ASC").Find(&channels).Error; err != nil {
		apiErr(w, err)
		return
	}
	out := make([]channelView, 0, len(channels))
	for i := range channels {
		out = append(out, a.channelView(&channels[i]))
	}
	writeJSON(w, 200, map[string]any{"channels": out})
}

func (a *App) handleNotifyChannelsCreate(w http.ResponseWriter, r *http.Request) {
	var in channelInput
	if err := decodeBody(r, &in); err != nil {
		apiErr(w, err)
		return
	}
	if err := a.validateChannelInput(&in); err != nil {
		apiErr(w, err)
		return
	}

	ch := &NotifyChannel{Provider: in.Provider, URL: in.URL, Enabled: true}
	a.applyChannelInput(ch, &in)
	if err := a.DB.Create(ch).Error; err != nil {
		apiErr(w, err)
		return
	}
	if in.Secret != "" && a.Vault != nil {
		if err := a.Vault.SaveSecret(channelSecretPath(ch.ID), in.Secret); err != nil {
			apiErr(w, err)
			return
		}
	}
	a.DB.Audit("create", "channel", ch.Name, ch.Provider)
	writeJSON(w, 201, a.channelView(ch))
}

func (a *App) handleNotifyChannelsUpdate(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	var in channelInput
	if err := decodeBody(r, &in); err != nil {
		apiErr(w, err)
		return
	}
	if err := a.validateChannelInput(&in); err != nil {
		apiErr(w, err)
		return
	}

	ch := &NotifyChannel{}
	if err := a.DB.First(ch, "id = ?", id).Error; err != nil {
		apiErr(w, g.Error("channel not found"))
		return
	}
	// a masked URL/secret means "keep the stored value"
	if in.URL == maskChannelURL(ch.Provider, ch.URL) || strings.Contains(in.URL, "***") {
		in.URL = ch.URL
	}
	if strings.Contains(in.Secret, "***") {
		in.Secret = ""
	}
	a.applyChannelInput(ch, &in)
	if err := a.DB.Save(ch).Error; err != nil {
		apiErr(w, err)
		return
	}
	if in.Secret != "" && a.Vault != nil {
		if err := a.Vault.SaveSecret(channelSecretPath(ch.ID), in.Secret); err != nil {
			apiErr(w, err)
			return
		}
	}
	a.DB.Audit("update", "channel", ch.Name, ch.Provider)
	writeJSON(w, 200, a.channelView(ch))
}

func (a *App) handleNotifyChannelsDelete(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	ch := &NotifyChannel{}
	if err := a.DB.First(ch, "id = ?", id).Error; err != nil {
		apiErr(w, g.Error("channel not found"))
		return
	}
	if err := a.DB.Delete(ch).Error; err != nil {
		apiErr(w, err)
		return
	}
	if a.Vault != nil {
		a.Vault.DeleteSecrets(channelSecretPath(id))
	}
	a.DB.Audit("delete", "channel", ch.Name, ch.Provider)
	writeJSON(w, 200, map[string]any{"deleted": true})
}

// handleNotifyChannelTest sends a test message through the channel and
// reports the delivery outcome synchronously.
func (a *App) handleNotifyChannelTest(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	ch := &NotifyChannel{}
	if err := a.DB.First(ch, "id = ?", id).Error; err != nil {
		apiErr(w, g.Error("channel not found"))
		return
	}
	secret := ""
	if a.Vault != nil {
		secret, _ = a.Vault.LoadSecret(channelSecretPath(id))
	}
	ev := &webhookEvent{
		Event: "system",
		Level: "info",
		Title: "sling-server test message",
		Body:  g.F("channel %q is configured correctly", ch.Name),
		At:    time.Now().Format(time.RFC3339),
	}
	if err := a.Notifier.sendToChannel(ch, secret, ev); err != nil {
		writeJSON(w, 200, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true})
}

// validateChannelInput checks name/provider/url/events; url must be http(s).
func (a *App) validateChannelInput(in *channelInput) error {
	in.Name = strings.TrimSpace(in.Name)
	if in.Name == "" {
		return g.Error("name is required")
	}
	if !notifyProviders[in.Provider] {
		return g.Error("invalid provider: %s", in.Provider)
	}
	u, err := url.Parse(strings.TrimSpace(in.URL))
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return g.Error("invalid webhook url (must be http/https)")
	}
	for _, e := range in.Events {
		valid := false
		for _, t := range notifyEventTypes {
			if e == t || e == "*" {
				valid = true
				break
			}
		}
		if !valid {
			return g.Error("invalid event: %s", e)
		}
	}
	return nil
}

// applyChannelInput copies editable fields onto the channel (URL is expected
// to be resolved to plaintext already).
func (a *App) applyChannelInput(ch *NotifyChannel, in *channelInput) {
	ch.Name = in.Name
	ch.Provider = in.Provider
	if in.URL != "" {
		ch.URL = strings.TrimSpace(in.URL)
	}
	if b, err := json.Marshal(in.Events); err == nil {
		ch.EventsJSON = string(b)
	}
	if in.Enabled != nil {
		ch.Enabled = *in.Enabled
	}
}

/* --------------------------- provider payloads ---------------------------- */

// webhookEvent is the provider-neutral event description handed to channel
// senders.
type webhookEvent struct {
	Event string         `json:"event"` // run_success|run_failed|run_cancelled|paused|system
	Title string         `json:"title"`
	Body  string         `json:"body"`
	Level string         `json:"level"`
	Meta  map[string]any `json:"meta,omitempty"`
	At    string         `json:"timestamp"`
}

// buildChannelPayload renders ev into the provider's wire format. secret is
// the optional signing secret (feishu only; dingtalk signs the URL instead).
func buildChannelPayload(provider string, ev *webhookEvent, secret string) ([]byte, error) {
	switch provider {
	case "wecom":
		return json.Marshal(map[string]any{
			"msgtype":  "markdown",
			"markdown": map[string]any{"content": g.F("**%s**\n%s", ev.Title, quotedDetail(ev))},
		})
	case "feishu":
		body := map[string]any{
			"msg_type": "text",
			"content":  map[string]any{"text": g.F("【sling】%s\n%s", ev.Title, ev.Body)},
		}
		if secret != "" {
			ts := time.Now().Unix()
			body["timestamp"] = g.F("%d", ts)
			body["sign"] = feishuSign(secret, ts)
		}
		return json.Marshal(body)
	case "dingtalk":
		return json.Marshal(map[string]any{
			"msgtype":  "markdown",
			"markdown": map[string]any{"title": ev.Title, "text": g.F("#### %s\n\n%s", ev.Title, ev.Body)},
		})
	case "slack":
		return json.Marshal(map[string]any{"text": g.F("*%s*\n%s", ev.Title, ev.Body)})
	case "generic":
		flat := map[string]any{
			"event":     ev.Event,
			"title":     ev.Title,
			"body":      ev.Body,
			"level":     ev.Level,
			"timestamp": ev.At,
		}
		for k, v := range ev.Meta {
			flat[k] = v
		}
		return json.Marshal(flat)
	default:
		return nil, g.Error("unknown provider: %s", provider)
	}
}

// quotedDetail renders body + meta as WeCom markdown quote lines.
func quotedDetail(ev *webhookEvent) string {
	var b strings.Builder
	if ev.Body != "" {
		b.WriteString("> " + strings.ReplaceAll(ev.Body, "\n", "\n> ") + "\n")
	}
	keys := make([]string, 0, len(ev.Meta))
	for k := range ev.Meta {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		b.WriteString(g.F("> %s: %v\n", k, ev.Meta[k]))
	}
	return b.String()
}

// feishuSign computes the Feishu custom-bot signature:
// base64(hmac-sha256(key = "<ts>\n<secret>", msg = "")).
func feishuSign(secret string, ts int64) string {
	mac := hmacSha256([]byte(g.F("%d\n%s", ts, secret)), nil)
	return base64.StdEncoding.EncodeToString(mac)
}

// signDingTalkURL appends the timestamp+sign query params required by
// DingTalk signed bots ("加签"): sign = url-escape(base64(hmac-sha256(secret, "<ts>\n<secret>"))).
func signDingTalkURL(rawURL, secret string) string {
	ts := time.Now().UnixMilli()
	sign := base64.StdEncoding.EncodeToString(hmacSha256([]byte(secret), []byte(g.F("%d\n%s", ts, secret))))
	sep := "?"
	if strings.Contains(rawURL, "?") {
		sep = "&"
	}
	return g.F("%s%stimestamp=%d&sign=%s", rawURL, sep, ts, url.QueryEscape(sign))
}

func hmacSha256(key, data []byte) []byte {
	mac := hmac.New(sha256.New, key)
	mac.Write(data)
	return mac.Sum(nil)
}
