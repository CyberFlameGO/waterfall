package conference

import (
	"encoding/json"
	"time"

	"github.com/matrix-org/waterfall/pkg/peer"
	"github.com/pion/webrtc/v3"
	"maunium.net/go/mautrix/event"
)

func (c *Conference) processJoinedTheCallMessage(participant *Participant, message peer.JoinedTheCall) {
	participant.logger.Info("Joined the call")
}

func (c *Conference) processLeftTheCallMessage(participant *Participant, msg peer.LeftTheCall) {
	participant.logger.Info("Left the call: %s", msg.Reason)
	c.removeParticipant(participant.id)
	c.signaling.SendHangup(participant.asMatrixRecipient(), msg.Reason)
}

func (c *Conference) processNewTrackPublishedMessage(participant *Participant, msg peer.NewTrackPublished) {
	participant.logger.Infof("Published new track: %s", msg.Track.ID())

	if _, ok := participant.publishedTracks[msg.Track.ID()]; ok {
		c.logger.Errorf("Track already published: %v", msg.Track.ID())
		return
	}

	participant.publishedTracks[msg.Track.ID()] = PublishedTrack{track: msg.Track}
	c.resendMetadataToAllExcept(participant.id)
}

func (c *Conference) processPublishedTrackFailedMessage(participant *Participant, msg peer.PublishedTrackFailed) {
	participant.logger.Infof("Failed published track: %s", msg.Track.ID())
	delete(participant.publishedTracks, msg.Track.ID())

	for _, otherParticipant := range c.participants {
		if otherParticipant.id == participant.id {
			continue
		}

		otherParticipant.peer.UnsubscribeFrom([]*webrtc.TrackLocalStaticRTP{msg.Track})
	}

	c.resendMetadataToAllExcept(participant.id)
}

func (c *Conference) processNewICECandidateMessage(participant *Participant, msg peer.NewICECandidate) {
	participant.logger.Debug("Received a new local ICE candidate")

	// Convert WebRTC ICE candidate to Matrix ICE candidate.
	jsonCandidate := msg.Candidate.ToJSON()
	candidates := []event.CallCandidate{{
		Candidate:     jsonCandidate.Candidate,
		SDPMLineIndex: int(*jsonCandidate.SDPMLineIndex),
		SDPMID:        *jsonCandidate.SDPMid,
	}}
	c.signaling.SendICECandidates(participant.asMatrixRecipient(), candidates)
}

func (c *Conference) processICEGatheringCompleteMessage(participant *Participant, msg peer.ICEGatheringComplete) {
	participant.logger.Info("Completed local ICE gathering")

	// Send an empty array of candidates to indicate that ICE gathering is complete.
	c.signaling.SendCandidatesGatheringFinished(participant.asMatrixRecipient())
}

func (c *Conference) processRenegotiationRequiredMessage(participant *Participant, msg peer.RenegotiationRequired) {
	participant.logger.Info("Started renegotiation")
	participant.sendDataChannelMessage(event.SFUMessage{
		Op:       event.SFUOperationOffer,
		SDP:      msg.Offer.SDP,
		Metadata: c.getAvailableStreamsFor(participant.id),
	})
}

func (c *Conference) processDataChannelMessage(participant *Participant, msg peer.DataChannelMessage) {
	participant.logger.Debug("Received data channel message")
	var sfuMessage event.SFUMessage
	if err := json.Unmarshal([]byte(msg.Message), &sfuMessage); err != nil {
		c.logger.Errorf("Failed to unmarshal SFU message: %v", err)
		return
	}

	switch sfuMessage.Op {
	case event.SFUOperationSelect:
		c.processSelectDCMessage(participant, sfuMessage)
	case event.SFUOperationAnswer:
		c.processAnswerDCMessage(participant, sfuMessage)
	case event.SFUOperationPublish:
		c.processPublishDCMessage(participant, sfuMessage)
	case event.SFUOperationUnpublish:
		c.processUnpublishDCMessage(participant)
	case event.SFUOperationAlive:
		c.processAliveDCMessage(participant)
	case event.SFUOperationMetadata:
		c.processMetadataDCMessage(participant, sfuMessage)
	}
}

func (c *Conference) processDataChannelAvailableMessage(participant *Participant, msg peer.DataChannelAvailable) {
	participant.logger.Info("Connected data channel")
	participant.sendDataChannelMessage(event.SFUMessage{
		Op:       event.SFUOperationMetadata,
		Metadata: c.getAvailableStreamsFor(participant.id),
	})
}

func (c *Conference) processRTCPPackets(msg peer.RTCPReceived) {
	const sendKeyFrameInterval = 500 * time.Millisecond

	for _, participant := range c.participants {
		if published, ok := participant.publishedTracks[msg.TrackID]; ok {
			if published.canSendKeyframeAt.After(time.Now()) {
				if err := participant.peer.WriteRTCP(msg.TrackID, msg.Packets); err == nil {
					published.canSendKeyframeAt = time.Now().Add(sendKeyFrameInterval)
				}
			}
		}
	}
}
