# ChatLoom WhatsApp bridge

This branch carries ChatLoom call history and receive-only URL actions on
mautrix-whatsapp v26.10.

## Behavior and contract

The native-flow `cta_url` parser reads only JSON `display_text` and `url`.
Hydrated template URL buttons use the same normalized representation:

```json
{
  "pl.personal.chat.whatsapp.interactive": {
    "schema_version": 1,
    "body": "Title\n\nBody\n\nFooter",
    "formatted_body": "Title<br><br>Body<br><br>Footer",
    "actions": [{"kind": "url", "label": "Open", "url": "https://example.org"}]
  }
}
```

The extension's body is the message text before URL fallbacks, including media
captions and readable instructions for unsupported buttons in mixed messages.
`formatted_body` is optional. The normal message body and HTML retain real URL
links for other clients. Labels and link attributes are HTML escaped. Headers,
footers, attachment metadata, media URLs, and existing business metadata remain.

Only absolute HTTP(S) URLs without userinfo, literal backslashes, raw whitespace,
or literal/percent-encoded control characters are accepted. Percent-encoded spaces
remain valid and URLs are not normalized. Native parameter JSON and the serialized extension are
limited to 64 KiB; at most 20 URL actions are exposed. Labels are limited to 1024
Unicode characters; URLs to 8192 bytes. Malformed, unknown, or rejected buttons
retain a native-app fallback. Raw native parameters and callback IDs are not copied.
The complete merged content, including business type/template metadata, has a conservative 44 KiB budget, leaving room for
E2EE ciphertext base64 expansion and the Matrix event envelope. Excess URL actions
retain a native-app instruction. If no URL fits, the extension is omitted without
cutting source text or media. The upstream event-size limit still applies to an
already oversized original message.

## Verification

Run the conversion tests with the bridge's configured crypto backend.

```sh
go test -tags goolm ./pkg/msgconv
```
