package sockets

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/domino14/liwords/pkg/entity"
	pb "github.com/domino14/liwords/rpc/api/proto/realtime"
	"github.com/rs/zerolog/log"
	"google.golang.org/protobuf/proto"
)

const (
	ipcTimeout = 2 * time.Second
)

func extendTopic(c *Client, topic string) string {
	// The publish topic should encode the user ID and the login status.
	// This is so we don't have to wastefully unmarshal and remarshal here,
	// and also because NATS plays nicely with hierarchical subject names.
	first := ""
	second := ""
	if !c.authenticated {
		first = "anon"
	} else {
		first = "auth"
	}
	second = c.userID

	return topic + "." + first + "." + second
}

func (h *Hub) parseAndExecuteMessage(ctx context.Context, msg []byte, c *Client) error {
	// All socket messages are encoded entity.Events.
	// (or they better be)

	// The type byte is [2] ([0] and [1] are length of the packet)
	switch pb.MessageType(msg[2]) {

	case pb.MessageType_SEEK_REQUEST:
		log.Debug().Msg("publishing seek request to NATS")
		err := h.pubsub.natsconn.Publish(extendTopic(c, "ipc.pb.seekRequest"), msg[3:])
		if err != nil {
			return err
		}

	case pb.MessageType_MATCH_REQUEST:
		log.Debug().Msg("publishing match request to NATS")
		err := h.pubsub.natsconn.Publish(extendTopic(c, "ipc.pb.matchRequest"), msg[3:])
		if err != nil {
			return err
		}

	case pb.MessageType_DECLINE_MATCH_REQUEST:
		log.Debug().Msg("publishing decline match request to NATS")
		err := h.pubsub.natsconn.Publish(extendTopic(c, "ipc.pb.declineMatchRequest"), msg[3:])
		if err != nil {
			return err
		}

	case pb.MessageType_SOUGHT_GAME_PROCESS_EVENT:
		log.Debug().Msg("publishing sought game process to NATS")

		err := h.pubsub.natsconn.Publish(extendTopic(c, "ipc.pb.soughtGameProcess"), msg[3:])
		if err != nil {
			return err
		}

	case pb.MessageType_CLIENT_GAMEPLAY_EVENT:
		err := h.pubsub.natsconn.Publish(extendTopic(c, "ipc.pb.gameplayEvent"), msg[3:])
		if err != nil {
			return err
		}

	case pb.MessageType_CHAT_MESSAGE:
		err := h.pubsub.natsconn.Publish(extendTopic(c, "ipc.pb.chat"), msg[3:])
		if err != nil {
			return err
		}

	case pb.MessageType_JOIN_PATH:
		ew, err := entity.EventFromByteArray(msg)
		if err != nil {
			return err
		}
		evt, ok := ew.Event.(*pb.JoinPath)
		if !ok {
			// This really shouldn't happen
			return errors.New("rr unexpected typing error")
		}
		return registerRealm(c, evt, h)

	case pb.MessageType_UNJOIN_REALM:
		h.removeFromRealm(c)

	case pb.MessageType_TIMED_OUT:
		err := h.pubsub.natsconn.Publish(extendTopic(c, "ipc.pb.timedOut"), msg[3:])
		if err != nil {
			return err
		}

	default:
		return fmt.Errorf("message type %v not yet handled", msg[2])
	}

	return nil
}

func registerRealm(c *Client, evt *pb.JoinPath, h *Hub) error {
	// There are a variety of possible realms that a person joining a game
	// can be in. We should not trust the user to send the right realm
	// (for example they can send a TV mode realm if they're a player
	// in the game or vice versa). The backend should determine the right realm
	// and assign it accordingly.
	log.Debug().Str("path", evt.Path).Msg("register-realm-path")
	var realm string
	if evt.Path == "/" {
		// This is the lobby; no need to request a realm.
		h.addToRealm(LobbyRealm, c)
		realm = string(LobbyRealm)
	} else {
		// First, create a request and send to the IPC api:
		rrr := &pb.RegisterRealmRequest{}
		rrr.Realm = evt.Path
		rrr.UserId = c.userID
		data, err := proto.Marshal(rrr)
		if err != nil {
			return err
		}
		resp, err := h.pubsub.natsconn.Request("ipc.request.registerRealm", data, ipcTimeout)
		if err != nil {
			log.Err(err).Msg("timeout registering realm")
			return err
		}
		log.Debug().Msg("got response from registerRealmReq")
		// The response contains the correct realm for the user.
		rrResp := &pb.RegisterRealmResponse{}
		err = proto.Unmarshal(resp.Data, rrResp)
		if err != nil {
			return err
		}
		realm = rrResp.Realm
		if Realm(realm) != NullRealm {
			// Only add to the realm that the API says to add to.
			h.addToRealm(Realm(realm), c)
		}
	}
	// Meow, depending on the realm, request that the API publish
	// initial information pertaining to this realm. For example,
	// lobby visitors will want to see a list of sought games,
	// or newcomers to a game realm will want to see the history
	// of the game so far.
	return h.sendRealmInitInfo(realm, c.userID)
	// The API will publish the initial realm information to this user's channel.
	// (user.userID - see pubsub.go)
}

func (h *Hub) sendRealmInitInfo(realm string, userID string) error {
	req := &pb.InitRealmInfo{
		Realm:  realm,
		UserId: userID,
	}
	data, err := proto.Marshal(req)
	if err != nil {
		return err
	}
	log.Debug().Interface("initRealmInfo", req).Msg("req-init-realm-info")

	return h.pubsub.natsconn.Publish("ipc.pb.initRealmInfo", data)
}
