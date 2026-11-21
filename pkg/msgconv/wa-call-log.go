// SPDX-License-Identifier: AGPL-3.0-or-later

package msgconv

import (
	"fmt"
	"time"

	"go.mau.fi/util/exfmt"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"maunium.net/go/mautrix/bridgev2"
	"maunium.net/go/mautrix/event"
)

func convertCallLogMessage(log *waE2E.CallLogMessage) *bridgev2.ConvertedMessagePart {
	body := "Call"
	callType := event.BeeperActionMessageCallType("")
	if log.IsVideo != nil {
		body = "Voice call"
		callType = event.BeeperActionMessageCallTypeVoice
		if log.GetIsVideo() {
			body = "Video call"
			callType = event.BeeperActionMessageCallTypeVideo
		}
	}
	status, outcome := "unknown", ""
	if log.CallOutcome != nil {
		switch log.GetCallOutcome() {
		case waE2E.CallLogMessage_CONNECTED:
			status, outcome = "answered", " answered"
		case waE2E.CallLogMessage_MISSED:
			status, outcome = "missed", " missed"
		case waE2E.CallLogMessage_FAILED:
			status, outcome = "failed", " failed"
		case waE2E.CallLogMessage_REJECTED:
			status, outcome = "rejected", " rejected"
		case waE2E.CallLogMessage_ACCEPTED_ELSEWHERE:
			status, outcome = "answered", " answered on another device"
		case waE2E.CallLogMessage_ONGOING:
			status, outcome = "started", " in progress"
		case waE2E.CallLogMessage_SILENCED_BY_DND, waE2E.CallLogMessage_SILENCED_UNKNOWN_CALLER:
			status, outcome = "silenced", " silenced"
		}
	}
	body += outcome
	fields := map[string]any{"status": status}
	if callType != "" {
		fields["call_type"] = callType
	}
	if duration := log.GetDurationSecs(); log.DurationSecs != nil && duration >= 0 && duration <= int64((1<<63-1)/time.Second) {
		fields["duration_seconds"] = duration
		if duration > 0 {
			body += fmt.Sprintf(" (%s)", exfmt.Duration(time.Duration(duration)*time.Second))
		}
	}
	return &bridgev2.ConvertedMessagePart{
		Type: event.EventMessage,
		Content: &event.MessageEventContent{
			MsgType: event.MsgNotice,
			Body:    body,
			BeeperActionMessage: &event.BeeperActionMessage{
				Type:     event.BeeperActionMessageCall,
				CallType: callType,
			},
		},
		Extra: map[string]any{"com.chatloom.call": fields},
	}
}
