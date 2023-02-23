package p2p

import (
	"context"
	"sync/atomic"
	"time"
)

func PingOption(option string) {

	connections := UniqueConnections()

	// for { // we must send all blocks atleast once, once we are done, break ut

	for _, c := range connections {
		select {
		case <-Exit_Event:
			return
		default:
		}

		if atomic.LoadUint32(&c.State) != HANDSHAKE_PENDING && c.Peer_ID != GetPeerID() { // skip pre-handshake connections

			switch option {
			case "seeds":

				if !IsSyncNode(c.Addr.String()) {
					continue
				}

			case "trusted":

				if !IsTrustedIP(c.Addr.String()) {
					continue
				}
			}

			go c.PingMe(1)

		}
	}
}

func GetModPeerTag(addr string) string {

	// for { // we must send all blocks atleast once, once we are done, break ut
	tag := ""

	if IsSyncNode(addr) {
		tag = "🕵"
		return tag
	}

	if IsTrustedIP(addr) {
		// tag = "✨"
		// tag = "📡"
		// tag = "🥰"
		// tag = "😍"
		tag = "👽"
		// tag = "🤖"
		return tag
	}

	return tag
}

func PingNode(target string) (err error, response Dummy) {

	found := false
	connections := UniqueConnections()

	// for { // we must send all blocks atleast once, once we are done, break ut

	for _, c := range connections {
		select {
		case <-Exit_Event:
			return
		default:
		}

		if ParseIPNoError(target) != ParseIPNoError(c.Addr.String()) {
			continue
		}

		if atomic.LoadUint32(&c.State) != HANDSHAKE_PENDING && c.Peer_ID != GetPeerID() { // skip pre-handshake connections
			c.logger.Info("Sending 3x PING Request(s) - 1 sec delay")
			err, response = c.PingMe(3)
			if err == nil {
				found = true
			}
		}
	}
	if !found {
		logger.Info("Node not connected", "addr", target)
	}

	return
}

func (c *Connection) PingMe(count int) (err error, response Dummy) {
	defer handle_connection_panic(c)

	var request Dummy
	fill_common(&request.Common) // fill common info

	// ping 3 times
	for i := 0; i < count; i++ {
		ping_start := time.Now()

		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		if err := c.Client.CallWithContext(ctx, "Peer.Ping", request, &response); err != nil {
			c.logger.V(0).Error(err, "PING Failed")
			// c.exit(fmt.Sprintf("Ping Failed: %s", err.Error()))
		} else {
			c.logger.Info("REPLY", "height", response.Common.Height, "rtt", time.Now().Sub(ping_start).Round(time.Millisecond).String())
		}
		if count > 1 {
			time.Sleep(1 * time.Second)
		}
	}

	return
}
