package main

import (
	"encoding/binary"
	"gobang/pb"
	"net"

	"google.golang.org/protobuf/proto"
)

const Size = 15

func sendMsg(conn net.Conn, msg *pb.GameMsg) error {
	data, err := proto.Marshal(msg)
	if err != nil {
		return err
	}
	lenBuf := make([]byte, 4)
	binary.BigEndian.PutUint32(lenBuf, uint32(len(data)))
	_, err = conn.Write(lenBuf)
	if err != nil {
		return err
	}
	_, err = conn.Write(data)
	return err
}

func readMsg(conn net.Conn) (*pb.GameMsg, error) {
	lenBuf := make([]byte, 4)
	_, err := conn.Read(lenBuf)
	if err != nil {
		return nil, err
	}
	msgLen := binary.BigEndian.Uint32(lenBuf)
	data := make([]byte, msgLen)
	_, err = conn.Read(data)
	if err != nil {
		return nil, err
	}
	var gameMsg pb.GameMsg
	err = proto.Unmarshal(data, &gameMsg)
	return &gameMsg, err
}
