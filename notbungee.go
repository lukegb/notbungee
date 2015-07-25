package main

import (
	"net"
	"log"
	"strings"
	"bytes"
	"errors"
	"io"
)

func makeDownstreamConnection() (net.Conn, error) {
	return net.Dial("tcp", "localhost:25666")
}

func readVarint(usConn io.Reader) (int64, int64, error) {
	br := int64(0)
	readByte := func() (byte, error) {
		buff := make([]byte, 1)
		n, err := usConn.Read(buff)
		br += int64(n)
		return buff[0], err
	}

	var out int64
	var err error
	out = 0

	b := byte(0x80)
	sh := uint(0)
	for (b&0x80 != 0) {
		b, err = readByte()
		if err != nil {
			return out, br, err
		}

		x := b & 0x7f
		out = out | (int64(x) << sh)
		sh += 7
	}

	return out, br, err
}

func writeVarint(usConn *bytes.Buffer, n int64) (error) {
	hasDone := false
	for !hasDone || n != 0 {
		hasDone = true

		x := n & 0x7f
		n = n >> 7
		if n != 0 {
			x = x | 0x80
		}

		err := usConn.WriteByte(byte(x))
		if err != nil {
			return err
		}
	}

	return nil
}

type HandshakePacket struct {
	protoVer int64
	serverAddr string
	serverPort int64
	nextState int64
}

func ReadHandshakePacket(c io.Reader) (hp *HandshakePacket, err error) {
	hp = &HandshakePacket{}

	packLen, _, err := readVarint(c)
	if err != nil {
		return hp, err
	}

	packId, packIdBytes, err := readVarint(c)
	if err != nil {
		return hp, err
	} else if packId != 0x0 {
		return hp, errors.New("bad packId")
	}

	packBuf := make([]byte, packLen - packIdBytes)
	n, err := c.Read(packBuf)
	if err != nil  {
		return hp, err
	} else if int64(n) != (packLen - packIdBytes) {
		return hp, errors.New("bad packLen")
	}

	bbuff := bytes.NewBuffer(packBuf)

	hp.protoVer, _, err = readVarint(bbuff)
	if err != nil {
		return hp, err
	}

	serverAddrLen, _, err := readVarint(bbuff)
	if err != nil {
		return hp, err
	}

	serverAddrBuf := make([]byte, serverAddrLen)
	_, err = bbuff.Read(serverAddrBuf)
	if err != nil {
		return hp, err
	}

	hp.serverAddr = string(serverAddrBuf)

	serverPortBuf := make([]byte, 2)
	_, err = bbuff.Read(serverPortBuf)
	if err != nil {
		return hp, err
	}

	hp.serverPort = (int64(serverPortBuf[0]) << 8 | int64(serverPortBuf[1]))

	hp.nextState, _, err = readVarint(bbuff)

	return hp, err
}

func MangleHandshakePacket(hp *HandshakePacket) (*HandshakePacket) {
	hp2 := HandshakePacket{}

	hp2.protoVer = hp.protoVer
	hp2.serverPort = hp.serverPort
	hp2.nextState = hp.nextState

	hp2.serverAddr = hp.serverAddr
	nullByte := strings.IndexByte(hp2.serverAddr, 0)
	if nullByte != -1 {
		hp2.serverAddr = hp2.serverAddr[:nullByte]
	}

	return &hp2
}

func WriteHandshakePacket(c io.Writer, hp *HandshakePacket) (error) {
	buf := bytes.NewBuffer([]byte{})

	if err := writeVarint(buf, 0); err != nil {
		return err
	}

	if err := writeVarint(buf, hp.protoVer); err != nil {
		return err
	}

	if err := writeVarint(buf, int64(len(hp.serverAddr))); err != nil {
		return err
	}

	if _, err := buf.Write([]byte(hp.serverAddr)); err != nil {
		return err
	}

	if _, err := buf.Write([]byte{byte((hp.serverPort & 0xff00) >> 8), byte(hp.serverPort & 0xff)}); err != nil {
		return err
	}

	if err := writeVarint(buf, hp.nextState); err != nil {
		return err
	}

	// now to writer
	b := new(bytes.Buffer)
	if err := writeVarint(b, int64(buf.Len())); err != nil {
		return err
	}

	if _, err := b.WriteTo(c); err != nil {
		return err
	}

	if _, err := buf.WriteTo(c); err != nil {
		return err
	}

	return nil
}


func handleConn(cid int, usConn net.Conn) {
	defer usConn.Close()

	log.Println(cid, "Handling new connection from", usConn.RemoteAddr())
	log.Println(cid, "Making downstream connection...")
	dsConn, err := makeDownstreamConnection()
	defer dsConn.Close()
	if err != nil {
		log.Println(cid, "Failed upstream", err)
		return
	}

	hp, err := ReadHandshakePacket(usConn)
	if err != nil {
		log.Println(cid, "Failed RHP", err)
		return
	}

	hp2 := MangleHandshakePacket(hp)

	err = WriteHandshakePacket(dsConn, hp2)
	if err != nil {
		log.Println(cid, "Failed WHP", err)
		return
	}

	go io.Copy(usConn, dsConn)
	io.Copy(dsConn, usConn)
}

func main() {
	log.Println("Starting up NotBungee")
	ln, err := net.Listen("tcp", ":25565")
	if err != nil {
		log.Fatalln(err)
	}

	cid := 0

	for {
		conn, err := ln.Accept()
		if err != nil {
			log.Println(err)
		} else {
			cid += 1
			go handleConn(cid, conn)
		}
	}
}
