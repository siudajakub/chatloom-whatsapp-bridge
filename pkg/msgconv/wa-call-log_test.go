// SPDX-License-Identifier: AGPL-3.0-or-later

package msgconv

import (
	"testing"

	"go.mau.fi/whatsmeow/proto/waE2E"
	"google.golang.org/protobuf/proto"
	"maunium.net/go/mautrix/event"
)

func TestWhatsAppCallLog(t *testing.T) {
	part := convertCallLogMessage(&waE2E.CallLogMessage{
		IsVideo:      proto.Bool(true),
		CallOutcome:  waE2E.CallLogMessage_CONNECTED.Enum(),
		DurationSecs: proto.Int64(62),
	})
	fields := part.Extra["com.chatloom.call"].(map[string]any)
	if fields["status"] != "answered" || fields["duration_seconds"] != int64(62) ||
		part.Content.BeeperActionMessage.CallType != event.BeeperActionMessageCallTypeVideo {
		t.Fatal("call log lost the media type, outcome or duration")
	}
}

func TestWhatsAppCallLogWithoutOptionalFields(t *testing.T) {
	part := convertCallLogMessage(&waE2E.CallLogMessage{})
	fields := part.Extra["com.chatloom.call"].(map[string]any)
	if fields["status"] != "unknown" || fields["call_type"] != nil || fields["duration_seconds"] != nil {
		t.Fatal("missing protobuf fields should remain unknown")
	}
}
