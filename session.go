// session.go - mixnet session client
// Copyright (C) 2017  Yawning Angel, Ruben Pollan, David Stainton
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as
// published by the Free Software Foundation, either version 3 of the
// License, or (at your option) any later version.
//
// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU Affero General Public License for more details.
//
// You should have received a copy of the GNU Affero General Public License
// along with this program.  If not, see <http://www.gnu.org/licenses/>.

// Package client provides the Katzenpost midclient
package client

import (
	"errors"
	"fmt"
	"time"

	"github.com/katzenpost/core/crypto/ecdh"
	"github.com/katzenpost/core/crypto/rand"
	"github.com/katzenpost/core/sphinx"
	"github.com/katzenpost/core/sphinx/constants"
	sphinxConstants "github.com/katzenpost/core/sphinx/constants"
	"github.com/katzenpost/core/utils"
	"github.com/katzenpost/minclient"
	"github.com/katzenpost/minclient/block"
	"github.com/op/go-logging"
)

// IngressBlockVersion is the current version of
// the IngressBlock serialization format
const IngressBlockVersion = 0

// UserKeyDiscovery interface for user key discovery
type UserKeyDiscovery interface {
	Get(identity string) (*ecdh.PublicKey, error)
}

// IngressBlock is used for storing decrypted
// blocked received from remote clients
type IngressBlock struct {
	Version      byte
	SenderPubKey *ecdh.PublicKey
	Block        *block.Block
}

// ToBytes serializes an IngressBlock into bytes
func (i *IngressBlock) ToBytes() ([]byte, error) {
	raw := []byte{}
	raw = append(raw, IngressBlockVersion)
	rawSenderPubKey, err := i.SenderPubKey.MarshalBinary()
	if err != nil {
		return nil, err
	}
	raw = append(raw, rawSenderPubKey...)
	rawBlock, err := i.Block.ToBytes()
	if err != nil {
		return nil, err
	}
	raw = append(raw, rawBlock...)
	return raw, nil
}

// FromBytes deserializes bytes into an IngressBlock
func (i *IngressBlock) FromBytes(raw []byte) error {
	if raw[0] != IngressBlockVersion {
		errors.New("failure of FromBytes: IngressBlock vesion mismatch")
	}
	i.Version = raw[0]
	pubKey := new(ecdh.PublicKey)
	err := pubKey.FromBytes(raw[1 : ecdh.PublicKeySize+1])
	if err != nil {
		return err
	}
	i.SenderPubKey = pubKey
	i.Block = new(block.Block)
	return i.Block.FromBytes(raw[ecdh.PublicKeySize+1:])
}

// EgressBlock is all the information needed
// to send the given payload
type EgressBlock struct {
	ReliableSend bool
	Recipient    string
	Provider     string
	BlockID      uint16
	MessageID    *[block.MessageIDLength]byte
	Payload      []byte
	Expiration   time.Time
	SURBID       *[sphinxConstants.SURBIDLength]byte
	SURBKeys     []byte
}

// Storage is an interface user for persisting
// ARQ and fragmentation/reassembly state
type Storage interface {
	GetIngressBlocks(*[block.MessageIDLength]byte) ([][]byte, error)
	PutIngressBlock(*[block.MessageIDLength]byte, []byte) error
	PutEgressBlock(*[block.MessageIDLength]byte, *EgressBlock) error
	AddSURBKeys(*[constants.SURBIDLength]byte, *EgressBlock) error
	RemoveSURBKey(*[constants.SURBIDLength]byte) error
}

// MessageConsumer is an interface used for
// processing received messages
type MessageConsumer interface {
	ReceivedMessage(senderPubKey *ecdh.PublicKey, message []byte)
	ReceivedACK(messageID *[block.MessageIDLength]byte)
}

// SessionConfig is specifies the configuration for a new session
type SessionConfig struct {
	User              string
	Provider          string
	IdentityPrivKey   *ecdh.PrivateKey
	LinkPrivKey       *ecdh.PrivateKey
	MessageConsumer   MessageConsumer
	Storage           Storage
	UserKeyDiscovery  UserKeyDiscovery
	PeriodicSendDelay time.Duration
}

// Session holds the client session
type Session struct {
	cfg             *SessionConfig
	minclient       *minclient.Client
	queue           chan string
	log             *logging.Logger
	messageConsumer MessageConsumer
	connected       chan bool
	identityPrivKey *ecdh.PrivateKey
	surbKeyMap      map[[constants.SURBIDLength]byte][]byte
	sendQueue       *SendQueue
	arqScheduler    *ARQScheduler
}

// NewSession stablishes a session with provider using key.
// This method will block until session is connected to the Provider.
// This method takes the following arguments:
// user: the username of the account
// provider: the Provider name indicates which Provider the user account is on
// identityKeyPriv: the private messaging key for end to end message exchanges with other users
// linkKeyPriv: the private link layer key for our noise wire protocol
// consumer: the message consumer consumes received messages
func (c *Client) NewSession(cfg *SessionConfig) (*Session, error) {
	var err error
	session := new(Session)
	clientCfg := &minclient.ClientConfig{
		User:        cfg.User,
		Provider:    cfg.Provider,
		LinkKey:     cfg.LinkPrivKey,
		LogBackend:  c.cfg.LogBackend,
		PKIClient:   c.cfg.PKIClient,
		OnConnFn:    session.onConnection,
		OnMessageFn: session.onMessage,
		OnACKFn:     session.onACK,
	}
	session.cfg = cfg
	session.identityPrivKey = cfg.IdentityPrivKey
	session.connected = make(chan bool, 0)
	session.messageConsumer = cfg.MessageConsumer
	session.log = c.cfg.LogBackend.GetLogger(fmt.Sprintf("%s@%s_session", cfg.User, cfg.Provider))
	session.surbKeyMap = make(map[[constants.SURBIDLength]byte][]byte)
	session.minclient, err = minclient.New(clientCfg)
	if err != nil {
		return nil, err
	}
	session.sendQueue = NewSendQueue(c.cfg.LogBackend, fmt.Sprintf("%s@%s", cfg.User, cfg.Provider), cfg.Storage, cfg.PeriodicSendDelay, session.minclient, session)
	session.arqScheduler = session.sendQueue.arqScheduler
	err = session.waitForConnection()
	if err != nil {
		return nil, err
	}
	return session, nil
}

// Shutdown the session
func (s *Session) Shutdown() {
	s.minclient.Shutdown()
}

// waitForConnection blocks until the client is
// connected to the Provider
func (s *Session) waitForConnection() error {
	isConnected := <-s.connected
	if !isConnected {
		return errors.New("status is not connected even with status change")
	}
	return nil
}

// Send reliably delivers the message to the recipient's queue
// on the destination provider or returns an error
func (s *Session) Send(recipient, provider string, message []byte) (*[block.MessageIDLength]byte, error) {
	s.log.Debugf("Send")
	messageID := [block.MessageIDLength]byte{}
	_, err := rand.Reader.Read(messageID[:])
	if err != nil {
		return nil, err
	}
	recipientPubKey, err := s.cfg.UserKeyDiscovery.Get(recipient)
	if err != nil {
		return nil, err
	}
	blocks, err := block.EncryptMessage(&messageID, message, s.identityPrivKey, recipientPubKey)
	if err != nil {
		return nil, err
	}
	for blockID, block := range blocks {
		egressBlock := EgressBlock{
			Recipient:    recipient,
			Provider:     provider,
			SURBID:       nil,
			BlockID:      uint16(blockID),
			Payload:      block,
			ReliableSend: true,
			MessageID:    &messageID,
		}
		err := s.cfg.Storage.PutEgressBlock(&messageID, &egressBlock) // XXX must serialize first
		if err != nil {
			s.log.Errorf("failure: egress storage error: %s", err)
		}
		s.sendQueue.Enqueue(&egressBlock)
	}
	return &messageID, nil
}

// SendUnreliable unreliably sends a message to the recipient's queue
// on the destination provider or returns an error
func (s *Session) SendUnreliable(recipient, provider string, message []byte) error {
	s.log.Debugf("SendUnreliable")
	messageID := [block.MessageIDLength]byte{}
	_, err := rand.Reader.Read(messageID[:])
	if err != nil {
		return err
	}
	recipientPubKey, err := s.cfg.UserKeyDiscovery.Get(fmt.Sprintf("%s@%s", recipient, provider))
	if err != nil {
		return err
	}
	if recipientPubKey == nil {
		return errors.New("nil recipient key")
	}
	s.log.Debugf("recipient key: %s\n", recipientPubKey)
	blocks, err := block.EncryptMessage(&messageID, message, s.identityPrivKey, recipientPubKey)
	if err != nil {
		return err
	}
	for blockID, block := range blocks {
		egressBlock := EgressBlock{
			Recipient:    recipient,
			Provider:     provider,
			SURBID:       nil,
			BlockID:      uint16(blockID),
			Payload:      block,
			ReliableSend: false,
			MessageID:    &messageID,
		}
		s.cfg.Storage.PutEgressBlock(&messageID, &egressBlock) // XXX must serialize first
		s.sendQueue.Enqueue(&egressBlock)
	}
	return nil
}

// OnConnection will be called by the minclient api
// upon connecting to the Provider
func (s *Session) onConnection(isConnected bool) {
	s.log.Debugf("OnConnection")
	s.connected <- isConnected
}

// OnMessage will be called by the minclient api
// upon receiving a message
func (s *Session) onMessage(ciphertextBlock []byte) error {
	s.log.Debugf("OnMessage")
	rBlock, senderPubKey, err := block.DecryptBlock(ciphertextBlock, s.identityPrivKey)
	if err != nil {
		return err
	}
	if rBlock.TotalBlocks == 1 {
		s.messageConsumer.ReceivedMessage(senderPubKey, rBlock.Payload)
		return nil
	}
	ingressBlock := IngressBlock{
		SenderPubKey: senderPubKey,
		Block:        rBlock,
	}
	rawStoredBlocks, err := s.cfg.Storage.GetIngressBlocks(&rBlock.MessageID)
	if err != nil {
		return err
	}
	rawBlock, err := ingressBlock.ToBytes()
	if err != nil {
		return err
	}
	rawBlocks := append(rawStoredBlocks, rawBlock)
	ingressBlocks := make([]*IngressBlock, len(rawBlocks))
	for i, b := range rawBlocks {
		ingressBlock := &IngressBlock{}
		err := ingressBlock.FromBytes(b)
		if err != nil {
			return err
		}
		ingressBlocks[i] = ingressBlock
	}
	message, err := reassemble(ingressBlocks)
	if err != nil {
		err = s.cfg.Storage.PutIngressBlock(&ingressBlock.Block.MessageID, rawBlock)
		if err != nil {
			return err
		}
	}
	s.messageConsumer.ReceivedMessage(senderPubKey, message)
	return nil
}

func (s *Session) AddSURBKeys(surbid *[constants.SURBIDLength]byte, surbKeyManifest *EgressBlock) error {
	_, ok := s.surbKeyMap[*surbKeyManifest.SURBID]
	if ok {
		s.log.Error("failure: SURB ID already present in surbKeyMap")
		return errors.New("failure: SURB ID already present in surbKeyMap")
	}
	s.surbKeyMap[*surbid] = surbKeyManifest.SURBKeys
	return s.cfg.Storage.AddSURBKeys(surbid, surbKeyManifest)
}

// OnACK is called by our minclient instance
// when we receive an ACK message
func (s *Session) onACK(surbid *[constants.SURBIDLength]byte, message []byte) error {
	s.log.Debugf("OnACK")
	surbKeys, ok := s.surbKeyMap[*surbid]
	if !ok {
		s.log.Errorf("failure: SURB key not found for received ACK")
		return nil
	}
	delete(s.surbKeyMap, *surbid)
	err := s.cfg.Storage.RemoveSURBKey(surbid)
	if err != nil {
		s.log.Errorf("failure: failure to remove SURB key: %s", err)
		return nil
	}
	plaintext, err := sphinx.DecryptSURBPayload(message, surbKeys)
	if err != nil {
		s.log.Errorf("failure: ACK SURB replay message decrypt error: %s", err)
		return nil
	}
	if !utils.CtIsZero(plaintext) {
		s.log.Errorf("failure: decrypted ACK payload is not all 0x00")
		return nil
	}
	err = s.arqScheduler.CancelRetransmission(surbid)
	if err != nil {
		s.log.Errorf("failure: retransmission cancellation error: %s", err)
	}
	return nil
}
