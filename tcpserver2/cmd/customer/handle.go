package customer

import (
	tcp "Go-Tool/tcpserver2"

	"github.com/issue9/logs"
)

//TCPHandle tcpserver使用示例，回复相同的内容
type TCPHandle struct {
	tcp.TCPHandle
}

//ReadPacket .
func (TCPHandle) ReadPacket(conn *tcp.Conn) tcp.Packet {
	//todo 定义读取数据帧的规则
	b := make([]byte, 1024)
	n, err := conn.Read(b)
	if err != nil {
		logs.Error(err)
		conn.Close()
	}
	p := &Packet{}
	p.SetBuffer(b[:n])

	return p
}

//OnConnection .
func (TCPHandle) OnConnection(conn *tcp.Conn) {
	//todo 连接建立时处理，用于一些建立连接时，需要主动下发数据包的场景
	logs.Infof("客户:%s 客人好像对你很感兴趣呦~~", conn.RemoteAddr())
}

//OnMessage .
func (TCPHandle) OnMessage(conn *tcp.Conn, p tcp.Packet) {
	//todo 处理接收的包
	sendP := Packet{}
	data := p.GetBuffer()
	sendP.SetBuffer(data)
	conn.Write(p) //回复客户端发送的内容
}

//OnClose .
func (TCPHandle) OnClose(state tcp.ConnState) {
	logs.Infof("客人好像撤退了呦~~,连接状态:%s", state.String())
}

//OnTimeOut .
func (TCPHandle) OnTimeOut(conn *tcp.Conn, code tcp.TimeOutState) {
	logs.Infof("%s: 客人好像在做一些灰暗的事情呢~~,超时类型:%d", conn.RemoteAddr(), code)
}

//OnPanic .
func (TCPHandle) OnPanic(conn *tcp.Conn, err error) {
	logs.Errorf("%s: 客人好像发生了一些不得了的事情哦~~,错误信息:%s", conn.RemoteAddr(), err)
}