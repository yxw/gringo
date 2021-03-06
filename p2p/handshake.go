// Copyright 2018 The Gringo Developers. All rights reserved.
// Use of this source code is governed by a GNU GENERAL PUBLIC LICENSE v3
// license that can be found in the LICENSE file.

package p2p

import (
	"bytes"
	"encoding/binary"
	"errors"
	"github.com/dblokhin/gringo/chain"
	"github.com/dblokhin/gringo/consensus"
	"github.com/sirupsen/logrus"
	"io"
	"net"
)

// First part of a handshake, sender advertises its version and
// characteristics.
type hand struct {
	// protocol version of the sender
	Version uint32
	// Capabilities of the sender
	Capabilities consensus.Capabilities
	// randomly generated for each handshake, helps detect self
	Nonce uint64

	// genesis block of our chain, only connect to peers on the same chain
	Genesis consensus.Hash

	// total difficulty accumulated by the sender, used to check whether sync
	// may be needed
	TotalDifficulty consensus.Difficulty
	// network address of the sender
	SenderAddr   *net.TCPAddr
	ReceiverAddr *net.TCPAddr

	// name of version of the software
	UserAgent string
}

func (h *hand) Bytes() []byte {
	buff := new(bytes.Buffer)

	if err := binary.Write(buff, binary.BigEndian, h.Version); err != nil {
		logrus.Fatal(err)
	}

	if err := binary.Write(buff, binary.BigEndian, uint32(h.Capabilities)); err != nil {
		logrus.Fatal(err)
	}

	if err := binary.Write(buff, binary.BigEndian, h.Nonce); err != nil {
		logrus.Fatal(err)
	}

	if err := binary.Write(buff, binary.BigEndian, uint64(h.TotalDifficulty)); err != nil {
		logrus.Fatal(err)
	}

	if (h.SenderAddr == nil) || (h.ReceiverAddr == nil) {
		logrus.Fatal("invalid netaddr (SenderAddr/ReceiverAddr)")
	}

	// Write Sender addr
	serializeTCPAddr(buff, h.SenderAddr)

	// Write Recv addr
	serializeTCPAddr(buff, h.ReceiverAddr)

	// Write user agent [len][string]
	binary.Write(buff, binary.BigEndian, uint64(len(h.UserAgent)))
	buff.WriteString(h.UserAgent)

	binary.Write(buff, binary.BigEndian, h.Genesis)

	return buff.Bytes()
}

func (h *hand) Type() uint8 {
	return consensus.MsgTypeHand
}

func (h *hand) Read(r io.Reader) error {

	if err := binary.Read(r, binary.BigEndian, &h.Version); err != nil {
		return err
	}

	if h.Version != consensus.ProtocolVersion {
		return errors.New("incompatibility protocol version")
	}

	if err := binary.Read(r, binary.BigEndian, (*uint32)(&h.Capabilities)); err != nil {
		return err
	}

	if err := binary.Read(r, binary.BigEndian, &h.Nonce); err != nil {
		return err
	}

	if err := binary.Read(r, binary.BigEndian, (*uint64)(&h.TotalDifficulty)); err != nil {
		return err
	}

	// read Sender addr
	addr, err := deserializeTCPAddr(r)
	if err != nil {
		return err
	}
	h.SenderAddr = addr

	// read Recv addr
	addr, err = deserializeTCPAddr(r)
	if err != nil {
		return err
	}
	h.ReceiverAddr = addr

	// read user agent
	var userAgentLen uint64
	if err := binary.Read(r, binary.BigEndian, &userAgentLen); err != nil {
		return err
	}

	buff := make([]byte, userAgentLen)
	if _, err := io.ReadFull(r, buff); err != nil {
		return err
	}

	h.UserAgent = string(buff)

	genesisBuf := make([]byte, 32)
	if _, err := io.ReadFull(r, genesisBuf); err != nil {
		return err
	}

	h.Genesis = genesisBuf

	return nil
}

// Second part of a handshake, receiver of the first part replies with its own
// version and characteristics.
type shake struct {
	// protocol version of the sender
	Version uint32
	// capabilities of the sender
	Capabilities consensus.Capabilities
	// total difficulty accumulated by the sender, used to check whether sync
	// may be needed
	TotalDifficulty consensus.Difficulty

	// name of version of the software
	UserAgent string

	// Genesis is the initial block hash on our chain. Used to prevent
	// connections to distinct chains.
	Genesis consensus.Hash
}

func (h *shake) Bytes() []byte {
	buff := new(bytes.Buffer)

	if err := binary.Write(buff, binary.BigEndian, h.Version); err != nil {
		logrus.Fatal(err)
	}

	if err := binary.Write(buff, binary.BigEndian, uint32(h.Capabilities)); err != nil {
		logrus.Fatal(err)
	}

	if err := binary.Write(buff, binary.BigEndian, uint64(h.TotalDifficulty)); err != nil {
		logrus.Fatal(err)
	}

	// Write user agent [len][string]
	binary.Write(buff, binary.BigEndian, uint64(len(h.UserAgent)))
	buff.WriteString(h.UserAgent)

	binary.Write(buff, binary.BigEndian, h.Genesis)

	return buff.Bytes()
}

func (h *shake) Type() uint8 {
	return consensus.MsgTypeShake
}

func (h *shake) Read(r io.Reader) error {

	if err := binary.Read(r, binary.BigEndian, &h.Version); err != nil {
		return err
	}

	if h.Version != consensus.ProtocolVersion {
		return errors.New("incompatibility protocol version")
	}

	if err := binary.Read(r, binary.BigEndian, (*uint32)(&h.Capabilities)); err != nil {
		return err
	}

	if err := binary.Read(r, binary.BigEndian, (*uint64)(&h.TotalDifficulty)); err != nil {
		return err
	}

	var userAgentLen uint64
	if err := binary.Read(r, binary.BigEndian, &userAgentLen); err != nil {
		return err
	}

	buff := make([]byte, userAgentLen)
	if _, err := io.ReadFull(r, buff); err != nil {
		return err
	}

	h.UserAgent = string(buff)

	genesisBuf := make([]byte, 32)
	if _, err := io.ReadFull(r, genesisBuf); err != nil {
		return err
	}

	h.Genesis = genesisBuf

	return nil
}

// shakeByHand sends hand to receive shake
func shakeByHand(conn net.Conn) (*shake, error) {
	// create hand
	// TODO: use the server listen addr
	sender, err := net.ResolveTCPAddr("tcp", "0.0.0.0:0")
	if err != nil {
		logrus.Fatal(err)
	}

	receiver := conn.RemoteAddr().(*net.TCPAddr)

	msg := hand{
		Version:         consensus.ProtocolVersion,
		Capabilities:    consensus.CapFullNode,
		Nonce:           serverNonces.NextNonce(),
		TotalDifficulty: consensus.Difficulty(1),
		SenderAddr:      sender,
		ReceiverAddr:    receiver,
		UserAgent:       userAgent,
		Genesis:         chain.Testnet4.Hash(),
	}

	// Send own hand
	if _, err := WriteMessage(conn, &msg); err != nil {
		return nil, err
	}

	// Read peer shake
	sh := new(shake)
	if _, err := ReadMessage(conn, sh); err != nil {
		return nil, err
	}

	return sh, nil
}

// handByShake sends shake and return received hand
func handByShake(conn net.Conn) (*hand, error) {

	var h hand

	// Recv remote hand
	if _, err := ReadMessage(conn, &h); err != nil {
		return nil, err
	}

	// Check nonce to detect connection to ourselves
	if serverNonces.Consist(h.Nonce) {
		return &h, errors.New("detect connection to ourselves by nonce")
	}

	// Send shake
	msg := shake{
		Version:         consensus.ProtocolVersion,
		Capabilities:    consensus.CapFullNode,
		TotalDifficulty: consensus.Difficulty(1),
		UserAgent:       userAgent,
	}
	if _, err := WriteMessage(conn, &msg); err != nil {
		return nil, err
	}

	return &h, nil
}
