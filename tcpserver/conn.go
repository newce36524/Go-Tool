package tcpserver

import (
	"context"
	"net"
	"time"
)

//fnProxy 代理执行方法,用于检测执行超时
func fnProxy(fn func()) <-chan struct{} {
	result := make(chan struct{}, 1)
	go func() {
		defer close(result)
		fn()
		result <- struct{}{}
	}()
	return result
}

//Conn 连接代理对象
type Conn struct {
	conn     net.Conn        //tcp连接对象
	option   ConnOption      //连接配置项
	state    ConnState       //连接状态
	context  context.Context //全局上下文
	recvChan <-chan Packet   //接收消息队列
	sendChan chan<- Packet   //发送消息队列
	handChan chan<- Packet   //处理消息队列
	cancel   func()          //全局上下文取消函数
	isDebug  bool            //是否打印框架内部debug信息
}

//NewConn returns a wrapper of raw conn
func NewConn(conn net.Conn, option ConnOption) (result *Conn) {
	result = &Conn{
		conn:   conn,
		option: option,
		state: ConnState{
			ActiveTime: time.Now(),
			RemoteAddr: conn.RemoteAddr().String(),
		},
		isDebug: false,
	}
	result.context, result.cancel = context.WithCancel(context.Background())
	return
}

//UseDebug 打开框架内部Debug信息
func (c *Conn) UseDebug() {
	c.isDebug = true
}

func (c *Conn) Read(b []byte) (int, error) {
	n, err := c.conn.Read(b)
	if err != nil {
		c.Close()
	}
	return n, err
}

//RemoteAddr 客户端IP地址
func (c *Conn) RemoteAddr() string {
	return c.conn.RemoteAddr().String()
}

//run 固定处理流程
func (c *Conn) run() {
	c.recvChan = c.recv(c.option.MaxRecvChanCount)
	c.sendChan = c.send(c.option.MaxSendChanCount)
	c.handChan = c.message()
	go func() {
		select {
		case <-fnProxy(func() { c.option.Handle.OnConnection(c) }):
		case <-time.After(c.option.SendTimeOut):
			c.option.Logger.Debugf("%s: Conn.run: OnConnection funtion invoke used time was too long", c.RemoteAddr())
		}
		defer func() {
			close(c.handChan)
			if c.isDebug {
				c.option.Logger.Debugf("%s: Conn.run: handChan is closed", c.RemoteAddr())
			}
			close(c.sendChan)
			if c.isDebug {
				c.option.Logger.Debugf("%s: Conn.Close: sendChan is closed", c.RemoteAddr())
				c.option.Logger.Debugf("%s: Conn.run: proxy goruntinue exit", c.RemoteAddr())
			}
		}()
		for {
			select {
			case <-c.context.Done():
				return
			case p, ok := <-c.recvChan:
				if !ok {
					c.option.Logger.Errorf("%s: Conn.run: recvChan is closed", c.RemoteAddr())
				}
				select {
				case <-c.context.Done():
					return
				case c.handChan <- p:
				}
			}
		}
	}()
}

//Send 发送消息到设备
func (c *Conn) Send(packet Packet) {
	if packet == nil {
		c.option.Logger.Errorf("%s: packet is nil,do nothing", c.RemoteAddr())
		return
	}
	select {
	case <-c.context.Done():
		return
	case c.sendChan <- packet:
	}
}

// Close 关闭服务器和设备的连接
func (c *Conn) Close() {
	defer c.conn.Close()
	c.option.Handle.OnClose(c.state)
	c.state.Message = "conn is closed"
	c.state.ComplateTime = time.Now()
	c.cancel()
	// runtime.GC()         //强制GC      待定可能有问题
	// debug.FreeOSMemory() //强制释放内存 待定可能有问题
}

//ReadPacket 读取一个包
func (c *Conn) readPacket(ctx context.Context) <-chan Packet {
	result := make(chan Packet)
	go func() {
		select {
		case <-ctx.Done():
			return
		default:
		}
		defer func() {
			close(result)
		}()
		p, err := c.option.Handle.ReadPacket(ctx, c)
		if err != nil {
			c.option.Logger.Error(err)
		} else {
			if c.isDebug {
				c.option.Logger.Debugf("%s: read a packet", c.RemoteAddr())
			}
			select {
			case <-ctx.Done():
			case result <- p:
			}
		}
	}()
	return result
}

//recv 创建一个包接收channel
func (c *Conn) recv(maxRecvChanCount int) <-chan Packet {
	result := make(chan Packet, maxRecvChanCount)
	go func() {
		defer func() {
			close(result)
			if c.isDebug {
				c.option.Logger.Debugf("%s: recvChan is closed")
				c.option.Logger.Debugf("%s: recv goruntinue exit", c.RemoteAddr())
			}
		}()
		for {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			ch := c.readPacket(ctx)
			select {
			case <-c.context.Done():
				return
			case <-time.After(c.option.RecvTimeOut):
				c.option.Handle.OnTimeOut(c, RecvTimeOut)
			case p, ok := <-ch:
				if ok {
					select {
					case <-c.context.Done():
						return
					case result <- p:
					}
				}
			}
		}
	}()
	return result
}

//send 创建一个包发送channel
func (c *Conn) send(maxSendChanCount int) chan<- Packet {
	result := make(chan Packet, maxSendChanCount)
	go func() {
		defer func() {
			if c.isDebug {
				c.option.Logger.Debugf("%s: send goruntinue exit", c.RemoteAddr())
			}
		}()
		for {
			select {
			case <-c.context.Done():
				return
			case packet, ok := <-result:
				if !ok {
					if c.isDebug {
						c.option.Logger.Debugf("%s: Conn.Send:sendChan is closed", c.RemoteAddr())
					}
					return
				}
				ctx, cancel := context.WithTimeout(context.Background(), c.option.SendTimeOut)
				select {
				case <-c.context.Done():
					cancel()
					return
				case <-time.After(c.option.SendTimeOut):
					c.option.Handle.OnTimeOut(c, SendTimeOut)
				case <-fnProxy(func() {
					sendData, err := packet.Serialize(ctx)
					if err != nil {
						c.option.Logger.Error(err)
					} else {
						select {
						case <-ctx.Done():
						default:
							_, err = c.conn.Write(sendData)
							if err != nil {
								c.option.Logger.Error(err)
							} else {
								if c.isDebug {
									c.option.Logger.Debugf("%s: send a packet", c.RemoteAddr())
								}
							}
							cancel()
						}
					}
				}):
				}
			}
		}
	}()
	return result
}

//message 创建一个消息处理channel
func (c *Conn) message() chan<- Packet {
	result := make(chan Packet, 1)
	go func() {
		defer func() {
			if c.isDebug {
				c.option.Logger.Debugf("%s: hand goruntinue exit", c.RemoteAddr())
			}
		}()
		for {
			select {
			case <-c.context.Done():
				return
			case p, ok := <-result:
				if !ok {
					c.option.Logger.Error("%s: Conn.Message: hand packet chan was closed", c.RemoteAddr())
					return
				}
				select {
				case <-c.context.Done():
				case <-time.After(c.option.HandTimeOut):
					c.option.Handle.OnTimeOut(c, HandTimeOut)
				case <-fnProxy(func() {
					c.option.Handle.OnMessage(c, p)
					if c.isDebug {
						c.option.Logger.Debugf("%s: hand a packet", c.RemoteAddr())
					}
				}):
				}
			}
		}
	}()
	return result
}