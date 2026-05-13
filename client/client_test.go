package client

import (
	"encoding/binary"
	"testing"
	"time"

	"github.com/golang/mock/gomock"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/topfreegames/pitaya/v2/conn/message"
	"github.com/topfreegames/pitaya/v2/conn/packet"
	"github.com/topfreegames/pitaya/v2/helpers"
	"github.com/topfreegames/pitaya/v2/mocks"
)

func TestSendRequestShouldTimeout(t *testing.T) {
	c := New(logrus.InfoLevel, 100*time.Millisecond)
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockConn := mocks.NewMockPlayerConn(ctrl)
	c.conn = mockConn
	go c.pendingRequestsReaper()

	route := "com.sometest.route"
	data := []byte{0x02, 0x03, 0x04}

	m := message.Message{
		Type:  message.Request,
		ID:    1,
		Route: route,
		Data:  data,
		Err:   false,
	}

	pkt, err := c.buildPacket(m)
	assert.NoError(t, err)

	mockConn.EXPECT().Write(pkt)

	c.IncomingMsgChan = make(chan *message.Message, 10)

	c.nextID = 0
	c.SendRequest(route, data)

	msg := helpers.ShouldEventuallyReceive(t, c.IncomingMsgChan, 2*time.Second).(*message.Message)

	assert.Equal(t, true, msg.Err)
}

func TestKickPacketPublishesKickType(t *testing.T) {
	c := New(logrus.InfoLevel)
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockConn := mocks.NewMockPlayerConn(ctrl)
	mockConn.EXPECT().Close()

	c.conn = mockConn
	c.Connected = true
	c.closeChan = make(chan struct{})

	kickType := int32(123)
	data := make([]byte, 4)
	binary.BigEndian.PutUint32(data, uint32(kickType))

	go c.handlePackets()
	c.packetChan <- &packet.Packet{Type: packet.Kick, Length: len(data), Data: data}

	actual := helpers.ShouldEventuallyReceive(t, c.KickChannel(), 2*time.Second).(int32)
	assert.Equal(t, kickType, actual)
	assert.False(t, c.Connected)
}

func TestDecodeKickTypeCompatibility(t *testing.T) {
	data := make([]byte, 4)
	kickType := int32(-1)
	binary.BigEndian.PutUint32(data, uint32(kickType))

	assert.Equal(t, int32(0), decodeKickType(nil))
	assert.Equal(t, int32(7), decodeKickType([]byte{7}))
	assert.Equal(t, int32(-1), decodeKickType(data))
	assert.Equal(t, int32(0), decodeKickType([]byte{1, 2}))
}
