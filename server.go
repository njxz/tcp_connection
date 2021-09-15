package main

import (
	"fmt"
	"net"
	"sync"
)

func tcpserver(wg *sync.WaitGroup) {
	defer wg.Done()
	l, err := net.Listen("tcp", "0.0.0.0:5000")
	if err != nil {
		return
	}
	for {
		conn, err := l.Accept()
		if err != nil {
			fmt.Println(err)
			continue
		}
		go handleConnection(conn)
	}
}

func handleConnection(conn net.Conn) {
	c := NewClient(conn)
	go c.Readloop()
	forward(c)
}

func forward(c *Client) {
	for {
		p := c.ReadBuff()
		data := EncodeData(PacketCmd, p.Body)
		c.Write(data)
	}

}
func main() {
	wg := &sync.WaitGroup{}
	go tcpserver(wg)
	wg.Add(1)
	wg.Wait()
}
