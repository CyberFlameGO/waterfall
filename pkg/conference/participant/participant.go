package participant

import (
	"fmt"

	"github.com/matrix-org/waterfall/pkg/peer"
	"github.com/matrix-org/waterfall/pkg/signaling"
	"github.com/sirupsen/logrus"
	"maunium.net/go/mautrix/event"
	"maunium.net/go/mautrix/id"
)

// Things that we assume as identifiers for the participants in the call.
// There could be no 2 participants in the room with identical IDs.
type ID struct {
	UserID   id.UserID
	DeviceID id.DeviceID
	CallID   string
}

// Participant represents a participant in the conference.
type Participant struct {
	ID              ID
	Logger          *logrus.Entry
	Peer            *peer.Peer[ID]
	RemoteSessionID id.SessionID
	Pong            chan<- Pong
}

func (p *Participant) AsMatrixRecipient() signaling.MatrixRecipient {
	return signaling.MatrixRecipient{
		UserID:          p.ID.UserID,
		DeviceID:        p.ID.DeviceID,
		CallID:          p.ID.CallID,
		RemoteSessionID: p.RemoteSessionID,
	}
}

func (p *Participant) SendDataChannelMessage(toSend event.Event) error {
	jsonToSend, err := toSend.MarshalJSON()
	if err != nil {
		return fmt.Errorf("Failed to marshal data channel message: %w", err)
	}

	if err := p.Peer.SendOverDataChannel(string(jsonToSend)); err != nil {
		// TODO: We must buffer the message in this case and re-send it once the data channel is recovered!
		return fmt.Errorf("Failed to send data channel message: %w", err)
	}

	return nil
}
