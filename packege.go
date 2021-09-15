package main

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"reflect"
	"sync"
)

const (
	PacketHead    uint8  = 0x02
	PacketTail    uint8  = 0x03
	PacketVersion uint16 = 0x02
	PacketCmd     uint8  = 0 //正常输入
	PacketHeart   uint8  = 1 //心跳
	PacketSeq     uint8  = 2
)

var (
	// 封包头的结构长度
	PacketHeadLen = getPacketHeadSize() //8
	TailError     = errors.New("包尾错误")
	VersionError  = errors.New("数据版本不对")
	LenError      = errors.New("包长度不够")
	HeadError     = errors.New("包头错误")
)

func getPacketHeadSize() (size uintptr) {
	t := reflect.TypeOf(TPacketHead{})
	for i := 0; i < t.NumField(); i++ {
		size += t.Field(i).Type.Size()
	}
	return
}

func Decode(b []byte) (p Packet, err error) {
	l := uintptr(len(b))
	if l < PacketHeadLen+2 {
		return p, LenError
	}
	if b[0] != PacketHead {
		return p, HeadError
	}
	if b[l-1] != PacketTail {
		return p, TailError
	}
	headbyte := b[1 : PacketHeadLen+1]
	head, err := DecodeHead(headbyte)
	if err != nil {
		return p, err
	}
	p.Head = head
	if head.Version != PacketVersion {
		return p, VersionError
	}
	if l < PacketHeadLen+2+uintptr(head.DataLen) {
		return p, LenError
	}
	body := b[PacketHeadLen+1 : PacketHeadLen+uintptr(head.DataLen)+1]
	p.Body = body
	return p, nil
}

func EncodeData(cmd uint8, body []byte) []byte {
	raw := bytes.NewBuffer([]byte{})
	head := TPacketHead{}
	head.Cmd = cmd
	head.Version = PacketVersion

	head.DataLen = uint32(len(body))
	binary.Write(raw, binary.LittleEndian, PacketHead)
	binary.Write(raw, binary.LittleEndian, head)
	raw.Write(body)
	binary.Write(raw, binary.LittleEndian, PacketTail)
	return raw.Bytes()
}

// DecodeHead
func DecodeHead(data []byte) (*TPacketHead, error) {
	head := new(TPacketHead)
	raw := bytes.NewBuffer(data)
	err := binary.Read(raw, binary.LittleEndian, head)
	return head, err
}

type TPacketHead struct {
	//Head uint8
	Version uint16 // 2
	Cmd     uint8  // 2  描述这个包是干什么的
	IsZip   uint16 // 2
	DataLen uint32 // 4
	Seq     uint16
}

type Packet struct {
	Head *TPacketHead
	Body []byte
}

type SeqStruct struct {
	Seq  uint16
	Chan *chan Packet
	Lock sync.RWMutex
}

var (
	Seq         uint16 = 0 //seq在1-65536之间循环
	SeqRouteMap        = map[uint16]*SeqStruct{}
	seqlock     sync.RWMutex
)

func GetSeq() (uint16, *chan Packet) {
	seqlock.Lock()
	defer seqlock.Unlock()
	tmp := Seq
	Seq++
	if Seq > 60000 {
		Seq = 0
	}
	readchan := make(chan Packet, 10)
	seqobj := SeqStruct{Seq: Seq, Chan: &readchan}
	SeqRouteMap[tmp] = &seqobj
	return tmp, &readchan
}

func DeleteSeqRoute(seq uint16) {
	seqlock.Lock()
	defer seqlock.Unlock()
	delete(SeqRouteMap, seq)
}

func WriteChan(seq uint16, data Packet) (ok bool) {
	seqlock.RLock()
	ch, ok := SeqRouteMap[seq]
	if !ok {
		seqlock.RUnlock()
		return
	}
	seqlock.RUnlock()

	ch.Lock.Lock()
	defer ch.Lock.Unlock()
	*((*ch).Chan) <- data
	return
}

func DeleteChan(seq uint16, readchan *chan Packet) {
	seqlock.Lock()
	defer seqlock.Unlock()
	close(*readchan)
	delete(SeqRouteMap, seq)

	return
}

func NewClient(c net.Conn) *Client {
	return &Client{
		c:     c,
		buff:  make(chan Packet, 10),
		heart: make(chan Packet, 10),
	}

}

type Client struct {
	c     net.Conn
	buff  chan Packet
	heart chan Packet
}

// 读数据
func rData(conn net.Conn, bLen int) ([]byte, error) {
	bsBuff := bytes.NewBuffer([]byte{})
	bufLen := bLen
	for {
		if bsBuff.Len() >= bLen {
			break
		}
		var n int
		if bufLen > 4096 {
			n = 4096
		} else {
			n = bufLen
		}
		buf := make([]byte, n)
		nr, err := conn.Read(buf)
		if err != nil {
			return nil, err
		}
		bsBuff.Write(buf[:nr])
		if nr == bLen {
			break
		}
		bufLen = bLen - bsBuff.Len()
	}
	return bsBuff.Bytes(), nil
}
func (c *Client) read() (pack Packet, err error) {
	byteflag := make([]byte, 1)
	_, err = c.c.Read(byteflag)
	if err != nil {
		return
	}
	if byteflag[0] != PacketHead {
		return pack, errors.New("head error")
	}
	headbuff := make([]byte, PacketHeadLen)
	_, err = c.c.Read(headbuff)
	if err != nil {
		return
	}
	head, err := DecodeHead(headbuff)
	if err != nil {
		return
	}
	body, err := rData(c.c, int(head.DataLen))
	if err != nil {
		return
	}
	_, err = c.c.Read(byteflag)
	if err != nil {
		return pack, errors.New("tail errors")
	}
	if byteflag[0] != PacketTail {
		pack.Head = head
		pack.Body = append(body, byteflag...)
		return pack, errors.New("tail errors")
	}
	pack.Head = head
	pack.Body = body
	return pack, nil
}
func (c Client) Close() {
	c.c.Close()
}
func (c Client) headbeat() {
	for {
		select {
		case <-c.heart:
		}
	}

}

func (c *Client) ReadBuff() Packet {
	pack := <-c.buff
	return pack
}

func (c Client) Readloop() {
	for {
		pack, err := c.read()
		if err != nil {
			switch err {
			case TailError:
				fmt.Println("tail error")
			default:
				c.Close()
			}
		}
		switch pack.Head.Cmd {
		case PacketCmd:
			c.buff <- pack
		case PacketHeart:
			c.heart <- pack
		case PacketSeq:
			ok := WriteChan(pack.Head.Seq, pack)
			if !ok {
				fmt.Println("write to seq error")
			}

		}
	}
}

func (c Client) Write(b []byte) (n int, err error) {
	return c.Write(b)
}
