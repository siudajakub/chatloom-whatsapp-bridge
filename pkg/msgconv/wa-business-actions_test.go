package msgconv

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"
	"testing"

	"go.mau.fi/whatsmeow/proto/waE2E"
	"google.golang.org/protobuf/proto"
	"maunium.net/go/mautrix/bridgev2"
	"maunium.net/go/mautrix/event"
)

func nativeButton(name, params string) *waE2E.InteractiveMessage_NativeFlowMessage_NativeFlowButton {
	return &waE2E.InteractiveMessage_NativeFlowMessage_NativeFlowButton{Name: proto.String(name), ButtonParamsJSON: proto.String(params)}
}

func nativeMessage(buttons ...*waE2E.InteractiveMessage_NativeFlowMessage_NativeFlowButton) *waE2E.InteractiveMessage {
	return &waE2E.InteractiveMessage{
		Body:   &waE2E.InteractiveMessage_Body{Text: proto.String("*Message* body")},
		Header: &waE2E.InteractiveMessage_Header{Title: proto.String("Title"), Subtitle: proto.String("Subtitle")},
		Footer: &waE2E.InteractiveMessage_Footer{Text: proto.String("Footer")},
		InteractiveMessage: &waE2E.InteractiveMessage_NativeFlowMessage_{
			NativeFlowMessage: &waE2E.InteractiveMessage_NativeFlowMessage{Buttons: buttons},
		},
	}
}

func readInteractive(t *testing.T, part *bridgev2.ConvertedMessagePart) businessInteractiveContent {
	t.Helper()
	raw, ok := part.Extra[WhatsAppInteractiveKey]
	if !ok {
		t.Fatal("missing interactive extension")
	}
	encoded, err := json.Marshal(raw)
	if err != nil {
		t.Fatal(err)
	}
	var parsed businessInteractiveContent
	if err = json.Unmarshal(encoded, &parsed); err != nil {
		t.Fatal(err)
	}
	if len(encoded) > maxBusinessPayload || parsed.SchemaVersion != 1 {
		t.Fatalf("invalid payload size/version: %d/%d", len(encoded), parsed.SchemaVersion)
	}
	return parsed
}

func TestNativeURLPreservesBodyAndEscapesFallback(t *testing.T) {
	mc := &MessageConverter{}
	msg := nativeMessage(nativeButton("cta_url", `{"display_text":"View <post> & details","url":"https://example.org/post?a=1&b=2","token":"must-not-be-copied"}`))
	part, _ := mc.convertInteractiveMessage(context.Background(), nil, msg)
	got := readInteractive(t, part)
	if got.Body != "Title\nSubtitle\n\n*Message* body\n\nFooter" || len(got.Actions) != 1 {
		t.Fatalf("wrong body/actions: %#v", got)
	}
	if !strings.Contains(got.FormattedBody, "<strong>Message</strong>") {
		t.Fatalf("lost WhatsApp body formatting: %s", got.FormattedBody)
	}
	if strings.Contains(got.Body, "https://") || strings.Contains(got.Body, "Use the WhatsApp") {
		t.Fatal("rich body contains URL fallback or unnecessary native-app instruction")
	}
	if !strings.Contains(part.Content.Body, "View <post> & details: https://example.org/post?a=1&b=2") ||
		!strings.Contains(part.Content.FormattedBody, `<a href="https://example.org/post?a=1&amp;b=2">View &lt;post&gt; &amp; details</a>`) {
		t.Fatalf("missing or unsafe fallback: %#v", part.Content)
	}
	encoded, _ := json.Marshal(part.Extra)
	if strings.Contains(string(encoded), "must-not-be-copied") || strings.Contains(string(encoded), "buttonParams") {
		t.Fatal("raw native-flow parameters leaked")
	}
}

func TestMixedNativeActionsKeepUnsupportedInstruction(t *testing.T) {
	mc := &MessageConverter{}
	part, _ := mc.convertInteractiveMessage(context.Background(), nil, nativeMessage(
		nativeButton("cta_url", `{"display_text":"First","url":"https://example.org/one"}`),
		nativeButton("quick_reply", `{"display_text":"Reply","id":"private-callback"}`),
		nativeButton("cta_url", `{"display_text":"Second","url":"https://example.org/two"}`),
	))
	got := readInteractive(t, part)
	if len(got.Actions) != 2 || got.Actions[0].Label != "First" || got.Actions[1].Label != "Second" ||
		!strings.Contains(got.Body, "<quick_reply>\nUse the WhatsApp app to click buttons") {
		t.Fatalf("mixed actions lost information: %#v", got)
	}
	if !strings.Contains(got.FormattedBody, "&lt;quick_reply&gt;") {
		t.Fatal("unsupported button was interpreted as HTML")
	}
	encoded, _ := json.Marshal(part)
	if strings.Contains(string(encoded), "private-callback") {
		t.Fatal("unimplemented callback parameters escaped into event")
	}
}

func TestRejectedNativeActionsRemainReadable(t *testing.T) {
	tests := map[string]string{
		"malformed": `{`, "array": `[]`, "null": `null`,
		"wrong type":         `{"display_text":"Open","url":2}`,
		"empty label":        `{"display_text":"","url":"https://example.org"}`,
		"script":             `{"display_text":"Open","url":"javascript:alert(1)"}`,
		"userinfo":           `{"display_text":"Open","url":"https://user:password@example.org"}`,
		"relative":           `{"display_text":"Open","url":"/post"}`,
		"control":            `{"display_text":"Open","url":"https://example.org/\npost"}`,
		"encoded control":    `{"display_text":"Open","url":"https://example.org/%0apost"}`,
		"raw space":          `{"display_text":"Open","url":"https://example.org/a b"}`,
		"encoded C1 control": `{"display_text":"Open","url":"https://example.org/%C2%85post"}`,
		"backslash path":     `{"display_text":"Open","url":"https://example.org/a\\b"}`,
		"backslash query":    `{"display_text":"Open","url":"https://example.org/?q=a\\b"}`,
		"oversized JSON":     strings.Repeat(" ", maxBusinessPayload+1),
		"oversized label":    `{"display_text":"` + strings.Repeat("a", maxBusinessLabel+1) + `","url":"https://example.org"}`,
		"oversized URL":      `{"display_text":"Open","url":"https://example.org/` + strings.Repeat("a", maxBusinessURL) + `"}`,
	}
	for name, params := range tests {
		t.Run(name, func(t *testing.T) {
			part, _ := (&MessageConverter{}).convertInteractiveMessage(context.Background(), nil, nativeMessage(nativeButton("cta_url", params)))
			if _, ok := part.Extra[WhatsAppInteractiveKey]; ok {
				t.Fatal("rejected native action became interactive")
			}
			if !strings.Contains(part.Content.Body, "<cta_url>\nUse the WhatsApp app to click buttons") {
				t.Fatal("missing readable fallback")
			}
		})
	}
}

func TestURLBoundsAndUnknownOrNilButtons(t *testing.T) {
	var actions []businessURLAction
	encodedSpaceURL := "https://example.org/a%20b?q=hello%20world"
	if !addBusinessURLAction(&actions, "Space", encodedSpaceURL) || actions[0].URL != encodedSpaceURL {
		t.Fatal("legal encoded spaces were rejected or URL was rewritten")
	}
	if !addBusinessURLAction(&actions, strings.Repeat("ą", maxBusinessLabel), "https://example.org/"+strings.Repeat("a", maxBusinessURL-len("https://example.org/"))) {
		t.Fatal("valid exact limits were rejected")
	}
	if addNativeFlowURLAction(&actions, nil) || addNativeFlowURLAction(&actions, nativeButton("new_flow", `{"display_text":"Open","url":"https://example.org"}`)) {
		t.Fatal("nil or unknown type accepted")
	}
	buttons := make([]*waE2E.InteractiveMessage_NativeFlowMessage_NativeFlowButton, maxBusinessActions+1)
	for i := range buttons {
		buttons[i] = nativeButton("cta_url", `{"display_text":"Open","url":"https://example.org"}`)
	}
	part, _ := (&MessageConverter{}).convertInteractiveMessage(context.Background(), nil, nativeMessage(buttons...))
	got := readInteractive(t, part)
	if len(got.Actions) != maxBusinessActions || !strings.Contains(got.Body, "Use the WhatsApp app") {
		t.Fatal("action limit or overflow fallback was not preserved")
	}
}

func TestHydratedURLButtonsUseSameContract(t *testing.T) {
	msg := &waE2E.TemplateMessage{HydratedTemplate: &waE2E.TemplateMessage_HydratedFourRowTemplate{
		HydratedContentText: proto.String("Body"), HydratedFooterText: proto.String("Footer"),
		HydratedButtons: []*waE2E.HydratedTemplateButton{
			{HydratedButton: &waE2E.HydratedTemplateButton_UrlButton{UrlButton: &waE2E.HydratedTemplateButton_HydratedURLButton{DisplayText: proto.String("Visit"), URL: proto.String("https://example.org")}}},
			{HydratedButton: &waE2E.HydratedTemplateButton_QuickReplyButton{QuickReplyButton: &waE2E.HydratedTemplateButton_HydratedQuickReplyButton{DisplayText: proto.String("Reply"), ID: proto.String("callback")}}},
		},
	}}
	part, _ := (&MessageConverter{}).convertTemplateMessage(context.Background(), nil, msg)
	got := readInteractive(t, part)
	if len(got.Actions) != 1 || got.Actions[0].Label != "Visit" || !strings.Contains(got.Body, "Body\n\nFooter") || !strings.Contains(got.Body, "<Reply>") {
		t.Fatalf("bad hydrated conversion: %#v", got)
	}
}

func TestBusinessActionsPreserveMediaMetadataAndCaption(t *testing.T) {
	info := &event.FileInfo{Width: 640, Height: 480}
	metadata := &struct{ Marker bool }{true}
	header := &bridgev2.ConvertedMessagePart{
		Content: &event.MessageEventContent{MsgType: event.MsgImage, Body: "Photo caption", FileName: "photo.jpg", URL: "mxc://example.org/image", Info: info},
		Extra:   map[string]any{"media_marker": true}, DBMetadata: metadata,
	}
	actions := []businessURLAction{{Kind: "url", Label: "Open", URL: "https://example.org"}}
	part := (&MessageConverter{}).postProcessBusinessActions("Business body", header, actions, nil, nil)
	got := readInteractive(t, part)
	if part.Content.MsgType != event.MsgImage || part.Content.URL != "mxc://example.org/image" || part.Content.FileName != "photo.jpg" ||
		part.Content.Info != info || part.DBMetadata != metadata || part.Extra["media_marker"] != true || got.Body != "Photo caption\n\nBusiness body" {
		t.Fatalf("media or caption was lost: %#v / %#v", part, got)
	}
}

func TestOversizedExtensionIsOmittedWithoutLosingSource(t *testing.T) {
	actions := []businessURLAction{{Kind: "url", Label: "Open", URL: "https://example.org"}}
	part := (&MessageConverter{}).postProcessBusinessActions(strings.Repeat("x", maxBusinessPayload), nil, actions, nil, nil)
	if _, ok := part.Extra[WhatsAppInteractiveKey]; ok {
		t.Fatal("oversized extension emitted")
	}
	if part.Content.Body != strings.Repeat("x", maxBusinessPayload) {
		t.Fatal("already oversized source text was changed")
	}
}

func TestBusinessEventBudgetLimitsLongURLFallbacks(t *testing.T) {
	actions := make([]businessURLAction, maxBusinessActions)
	for i := range actions {
		actions[i] = businessURLAction{Kind: "url", Label: "Open", URL: "https://example.org/" + strings.Repeat("a", 8000)}
	}
	part := (&MessageConverter{}).postProcessBusinessActions("Original body", nil, actions, nil, nil)
	got := readInteractive(t, part)
	content, _ := json.Marshal(part.Content)
	extra, _ := json.Marshal(part.Extra)
	if len(got.Actions) >= maxBusinessActions || len(content)+len(extra) > maxBusinessEventContent || !strings.Contains(got.Body, "additional URL buttons") {
		t.Fatalf("Matrix event budget not respected: %d actions, %d bytes", len(got.Actions), len(content)+len(extra))
	}
}

func TestMixedNativeBackslashURLDoesNotHideValidURL(t *testing.T) {
	part, _ := (&MessageConverter{}).convertInteractiveMessage(context.Background(), nil, nativeMessage(
		nativeButton("cta_url", `{"display_text":"Bad","url":"https://example.org/a\\b"}`),
		nativeButton("cta_url", `{"display_text":"Good","url":"https://example.org/good"}`),
	))
	got := readInteractive(t, part)
	if len(got.Actions) != 1 || got.Actions[0].Label != "Good" || !strings.Contains(got.Body, "Use the WhatsApp app") {
		t.Fatalf("invalid URL affected valid CTA: %#v", got)
	}
	if strings.Contains(part.Content.Body, `example.org/a\b`) || !strings.Contains(part.Content.Body, "Good: https://example.org/good") {
		t.Fatal("invalid URL leaked or valid URL fallback disappeared")
	}
}

func TestHydratedTemplateMetadataIncludedInFinalEventBudget(t *testing.T) {
	for _, idLength := range []int{maxBusinessEventContent - 1000, maxBusinessPayload + 1000} {
		t.Run(strconv.Itoa(idLength), func(t *testing.T) {
			templateID := strings.Repeat("i", idLength)
			msg := &waE2E.TemplateMessage{HydratedTemplate: &waE2E.TemplateMessage_HydratedFourRowTemplate{
				TemplateID: proto.String(templateID), HydratedContentText: proto.String("Original source body"),
				HydratedButtons: []*waE2E.HydratedTemplateButton{
					{HydratedButton: &waE2E.HydratedTemplateButton_UrlButton{UrlButton: &waE2E.HydratedTemplateButton_HydratedURLButton{DisplayText: proto.String("Visit"), URL: proto.String("https://example.org/" + strings.Repeat("x", 2000))}}},
				},
			}}
			part, _ := (&MessageConverter{}).convertTemplateMessage(context.Background(), nil, msg)
			encoded, err := json.Marshal(&event.Content{Parsed: part.Content, Raw: part.Extra})
			if err != nil {
				t.Fatal(err)
			}
			if part.Extra["fi.mau.whatsapp.hydrated_template_id"] != templateID || part.Extra["fi.mau.whatsapp.business_message_type"] != "template" || !strings.HasPrefix(part.Content.Body, "Original source body") {
				t.Fatal("upstream source body or metadata changed")
			}
			if _, ok := part.Extra[WhatsAppInteractiveKey]; ok {
				t.Fatal("interactive extension escaped final metadata budget")
			}
			if idLength < maxBusinessEventContent && len(encoded) > maxBusinessEventContent {
				t.Fatalf("final merged content exceeds budget: %d", len(encoded))
			}
			if idLength > maxBusinessPayload && part.Content.Body != "Original source body" {
				t.Fatal("already oversized upstream payload was inflated or cut")
			}
		})
	}
}
