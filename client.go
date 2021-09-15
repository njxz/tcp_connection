package main

import (
	"fmt"
	"net"
)

func ServerForward() {
	conn, err := net.Dial("tcp", "127.0.0.1:5000")
	if err != nil {
		return
	}
	c := NewClient(conn)
	go c.Readloop()
	data := EncodeData(PacketCmd, []byte("client data"))
	conn.Write(data)
	d := c.ReadBuff()
	fmt.Println(d.Body)

}
func main() {
	ServerForward()
}
