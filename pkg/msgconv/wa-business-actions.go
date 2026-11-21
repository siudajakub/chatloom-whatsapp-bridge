package msgconv

import (
	"encoding/json"
	"html"
	"maps"
	"net/url"
	"strings"
	"unicode"
	"unicode/utf8"

	"go.mau.fi/whatsmeow/proto/waE2E"
	"maunium.net/go/mautrix/bridgev2"
	"maunium.net/go/mautrix/event"
)

const (
	WhatsAppInteractiveKey = "pl.personal.chat.whatsapp.interactive"
	maxBusinessPayload     = 64 * 1024
	maxBusinessActions     = 20
	maxBusinessLabel       = 1024
	maxBusinessURL         = 8192
	// Reserve the room-event envelope and base64 expansion of E2EE ciphertext.
	maxBusinessEventContent = 44 * 1024
)

// These receive-only fields deliberately exclude WhatsApp's arbitrary action
// parameters. Opening a URL is a local client action, never a bridge callback.
type businessURLAction struct {
	Kind  string `json:"kind"`
	Label string `json:"label"`
	URL   string `json:"url"`
}

type businessInteractiveContent struct {
	SchemaVersion int                 `json:"schema_version"`
	Body          string              `json:"body"`
	FormattedBody string              `json:"formatted_body,omitempty"`
	Actions       []businessURLAction `json:"actions"`
}

func addBusinessURLAction(actions *[]businessURLAction, label, rawURL string) bool {
	if len(*actions) >= maxBusinessActions || !utf8.ValidString(label) || !utf8.ValidString(rawURL) ||
		utf8.RuneCountInString(label) > maxBusinessLabel || len(rawURL) > maxBusinessURL ||
		strings.ContainsRune(rawURL, '\\') ||
		strings.IndexFunc(rawURL, func(r rune) bool { return unicode.IsControl(r) || unicode.IsSpace(r) }) >= 0 {
		return false
	}
	label = strings.TrimSpace(label)
	parsed, err := url.Parse(rawURL)
	if err != nil || label == "" || parsed.User != nil || parsed.Hostname() == "" ||
		(parsed.Scheme != "http" && parsed.Scheme != "https") {
		return false
	}
	// Reject escaped control characters too, so client URI decoding cannot
	// accidentally turn an accepted link into a different kind of instruction.
	decoded, err := url.PathUnescape(rawURL)
	if err != nil || !utf8.ValidString(decoded) || strings.IndexFunc(decoded, unicode.IsControl) >= 0 {
		return false
	}
	*actions = append(*actions, businessURLAction{Kind: "url", Label: label, URL: rawURL})
	return true
}

func addNativeFlowURLAction(actions *[]businessURLAction, button *waE2E.InteractiveMessage_NativeFlowMessage_NativeFlowButton) bool {
	if button.GetName() != "cta_url" || len(button.GetButtonParamsJSON()) > maxBusinessPayload {
		return false
	}
	var params struct {
		Label string `json:"display_text"`
		URL   string `json:"url"`
	}
	if json.Unmarshal([]byte(button.GetButtonParamsJSON()), &params) != nil {
		return false
	}
	return addBusinessURLAction(actions, params.Label, params.URL)
}

func businessButtonDescription(label string) string {
	label = strings.TrimSpace(label)
	if label == "" {
		label = "Unsupported button"
	}
	if utf8.RuneCountInString(label) > maxBusinessLabel {
		label = string([]rune(label)[:maxBusinessLabel]) + "…"
	}
	return "<" + label + ">"
}

// appendBusinessText appends literal button text without feeding it through the
// WhatsApp inline-link parser. Labels must not become arbitrary HTML or URLs.
func appendBusinessText(content *event.MessageEventContent, text, renderedHTML string) {
	if text == "" {
		return
	}
	if content.Format != event.FormatHTML {
		content.FormattedBody = event.TextToHTML(content.Body)
		content.Format = event.FormatHTML
	}
	if content.Body != "" {
		content.Body += "\n\n"
	}
	if content.FormattedBody != "" {
		content.FormattedBody += "<br><br>"
	}
	content.Body += text
	if renderedHTML == "" {
		renderedHTML = event.TextToHTML(text)
	}
	content.FormattedBody += renderedHTML
}

func businessContentFitsBudget(content *event.MessageEventContent, extra map[string]any) bool {
	encoded, err := json.Marshal(&event.Content{Parsed: content, Raw: extra})
	return err == nil && len(encoded) <= maxBusinessEventContent
}

func (mc *MessageConverter) postProcessBusinessActions(body string, header *bridgev2.ConvertedMessagePart, actions []businessURLAction, unsupported []string, metadata map[string]any) *bridgev2.ConvertedMessagePart {
	converted := mc.postProcessBusinessMessage(body, header)
	for key, value := range metadata {
		converted.Extra[key] = value
	}
	if len(unsupported) > 0 {
		appendBusinessText(converted.Content, strings.Join(unsupported, " - ")+"\nUse the WhatsApp app to click buttons", "")
	}
	if len(actions) == 0 {
		return converted
	}
	for count := len(actions); count > 0; count-- {
		candidate := *converted.Content
		if count < len(actions) {
			appendBusinessText(&candidate, "Use the WhatsApp app to open additional URL buttons", "")
		}
		interactive := businessInteractiveContent{
			SchemaVersion: 1,
			Body:          candidate.Body,
			Actions:       actions[:count],
		}
		if candidate.Format == event.FormatHTML {
			interactive.FormattedBody = candidate.FormattedBody
		}
		extra := maps.Clone(converted.Extra)
		extra[WhatsAppInteractiveKey] = interactive
		plain := make([]string, count)
		rendered := make([]string, count)
		for i, action := range actions[:count] {
			plain[i] = action.Label + ": " + action.URL
			rendered[i] = `<a href="` + html.EscapeString(action.URL) + `">` + html.EscapeString(action.Label) + `</a>`
		}
		appendBusinessText(&candidate, strings.Join(plain, "\n"), strings.Join(rendered, "<br>"))
		if businessContentFitsBudget(&candidate, extra) {
			converted.Content = &candidate
			converted.Extra = extra
			return converted
		}
	}
	// Keep the source text and media if even one URL fallback would be too big.
	candidate := *converted.Content
	appendBusinessText(&candidate, "Use the WhatsApp app to open URL buttons", "")
	if businessContentFitsBudget(&candidate, converted.Extra) {
		converted.Content = &candidate
	}
	return converted
}
