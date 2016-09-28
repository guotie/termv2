package term

import (
	"fmt"
	"net"
	"strings"
	"sync"

	//"github.com/guotie/deferinit"
	"github.com/smtc/glog"
	"github.com/ziutek/telnet"
	//"strings"
)

const (
	bspace = byte(' ')
	bcomma = byte('"')

	termPort = 6789

	CR = byte(13)
	LF = byte(10)

	TELOPT_ECHO       = byte(1)
	TELOPT_SGA        = byte(3)
	TELCODE_BACKSPACE = byte(8)
	TELCODE_TAB       = byte('\t')
	TELOPT_LINEMODE   = byte(34)
	TELANSI_ESC       = byte(27)

	TELKEY_UP    = byte(65)
	TELKEY_DOWN  = byte(66)
	TELKEY_RIGHT = byte(67)
	TELKEY_LEFT  = byte(68)
	TELKEYBOARD  = byte(91)

	TELCODE_WILL = byte(251)
	TELCODE_WONT = byte(252)
	TELCODE_DO   = byte(253)
	TELCODE_DONT = byte(254)
	TELCODE_IAC  = byte(255)

	MaxHistroyCmds = 200
)

type cmdHistory struct {
	cmds   [MaxHistroyCmds]string
	index  int
	used   int
	pindex int
}

// 处理console命令的函数
type TermFunc func([]string) (string, error)

// console 命令结构体体
// maxParams:  该命令最大参数个数
// minParams:  该命令最小参数个数
// repeatable: 该命令是否可重复
// fn        : 命令处理函数
type ConsoleCmd struct {
	maxParams  int
	minParams  int
	repeatable bool
	fn         TermFunc
}

var (
	termLn      net.Listener
	_           = fmt.Sprint
	_           = telnet.NewConn
	termHandler = make(map[string]ConsoleCmd)
	lastCmd     *ConsoleCmd
	lastArgv    []string
	history     = cmdHistory{}
)

func init() {
	//deferinit.AddInit(startTermServer, stopTermServer, 1)
	//deferinit.AddRoutine(termRoutine)
}

func StartTermServer(port int) {
	var err error

	termLn, err = net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		panic("start term server failed: " + err.Error())
	}

	glog.Info("start terminal server successfully.\n")
}

func StopTermServer() {
	termLn.Close()
}

func TermRoutine(ch chan struct{}, wg *sync.WaitGroup) {
	running := true
	go func() {
		<-ch

		// 关闭所有connection
		running = false
		wg.Done()
	}()

	for running {
		conn, err := termLn.Accept()
		if err != nil {
			glog.Error("term server accept failed: %s\n", err.Error())
			continue
		}
		tconn, _ := telnet.NewConn(conn)
		_ = tconn
		conn.Write([]byte{TELCODE_IAC, TELCODE_WILL, TELOPT_SGA})
		conn.Write([]byte{TELCODE_IAC, TELCODE_WILL, TELOPT_ECHO})
		//tconn.SetEcho(false)
		//tconn.Write([]byte{TELCODE_IAC, TELCODE_DONT, TELOPT_ECHO})
		go handleTermConn(conn)
	}
}

// cmd: 命令名称
// maxParams: 最大参数个数，含命令本身
// minParams: 最少参数个数
// repeat:    再不输入任何字符时，命令是否可以重复执行
// fn:        该命令的执行函数
func RegisterTermCmd(cmd string, maxParams, minParams int, repeat bool, fn TermFunc) {
	termHandler[cmd] = ConsoleCmd{maxParams, minParams, repeat, fn}
}

// 将buff按照空格split, 引号中间的不分割
func splitBuff(cmd string) []string {
	var (
		i       int
		ch      byte
		start   int
		end     int
		inspace bool = true
		argv    []string
	)

	cmd = strings.Trim(strings.Trim(strings.TrimSpace(cmd), "\r"), "\n")

	for i = 0; i < len(cmd); i++ {
		ch = cmd[i]
		if ch == bspace {
			if inspace {
				end = i
				if start != end {
					argv = append(argv, string(cmd[start:end]))
				}
				start = i + 1
			} else {
				inspace = true
				start = i + 1
			}
		} else if ch == bcomma {
			i++
			start = i
			inspace = false
			for ; i < len(cmd); i++ {
				if cmd[i] == bcomma {
					break
				}
			}
			end = i
			if end > start {
				argv = append(argv, cmd[start:end])
			}
		}
	}
	if inspace {
		argv = append(argv, string(cmd[start:]))
	}

	return argv
}

func handleTermConn(conn net.Conn) {
	var (
		err error
		cmd string
	)

	tcpconn := conn.(*net.TCPConn)

	for {
		_, err = tcpconn.Write([]byte("->"))
		if err != nil {
			break
		}

		cmd, err = parseInput(tcpconn)
		if err != nil {
			break
		}

		handleTermCmd(tcpconn, cmd)

	}
	tcpconn.Close()
}

// 控制客户端删除输入的字符
// 先后退， 再打印空格，再后退
func backspace(conn *net.TCPConn, n int) {
	bck := []byte{TELANSI_ESC, byte('[')}
	bck = append(bck, []byte(fmt.Sprintf("%d", n))...)
	bck = append(bck, TELKEY_LEFT)
	conn.Write(bck)
	for i := 0; i < n; i++ {
		conn.Write([]byte(" "))
	}
	conn.Write(bck)
}

// 解析用户输入
func parseInput(conn *net.TCPConn) (cmd string, err error) {
	var (
		//n       int
		ch, ch2 byte
		repeat  bool // 由上下键带来的命令
		buflen  int
		ccmd    string
		buf     = make([]byte, 1024)
	)

	for buflen < 1000 {
		_, err = conn.Read(buf[buflen : buflen+1])
		if err != nil {
			glog.Error("handleTermConn: %s\n", err.Error())
			return
		}
		ch = buf[buflen]
		switch ch {
		case TELCODE_IAC:
			// 控制命令，忽略
			conn.Read(buf[buflen+1 : buflen+3])
			buflen = 0
			buf = buf[0:]
			cmd = ""
			continue
		case 0:
			buflen++
			continue
		case TELANSI_ESC:
			buflen++
			conn.Read(buf[buflen : buflen+1])
			ch2 = buf[buflen]
			buflen++
			if ch2 != TELKEYBOARD {
				glog.Warn("invalid TELANSI_ESC: 0x%x\n", ch2)
				continue
			}
			conn.Read(buf[buflen : buflen+1])
			ch2 = buf[buflen]
			buflen++
			switch ch2 {
			case TELKEY_UP:
				ccmd = getHistoryCmd(TELKEY_UP)
			case TELKEY_DOWN:
				ccmd = getHistoryCmd(TELKEY_DOWN)

			default:
				// 左右键不处理
				//fmt.Println(ch)
				continue
			}
			//fmt.Println(cmd, ccmd)
			repeat = true
			//光标退到开始位置
			if len(cmd) > 0 {
				backspace(conn, len(cmd))
			}
			// 回显
			cmd = ccmd
			conn.Write([]byte(cmd))
			buflen = len(cmd)
		case CR:
			fallthrough
		case LF:
			conn.Write([]byte("\r\n"))
			goto out
		case TELCODE_BACKSPACE:
			if len(cmd) > 0 {
				backspace(conn, 1)
				cmd = cmd[0 : len(cmd)-1]
				buflen--
				repeat = false
			}
			continue
		case TELCODE_TAB:
		// 自动补全功能
		default:
			//fmt.Println(ch, string(ch))
			cmd += string(ch)
			conn.Write([]byte{ch})
			repeat = false
		}

		buflen++
	}
out:
	if repeat == false {
		setHistoryCmd(cmd)
	} else {
		if history.used > 0 {
			history.index = history.pindex
		}
	}
	return
}

// 历史命令
func setHistoryCmd(cmd string) {
	if strings.TrimSpace(cmd) == "" {
		return
	}
	if history.used >= MaxHistroyCmds {
		history.used = MaxHistroyCmds
	}
	if history.index >= MaxHistroyCmds {
		history.index = 0
	}

	history.cmds[history.index] = cmd
	history.index++
	history.pindex = history.index
	history.used++
}

// 调试函数，打印history command
func printHistoryCmd() {
	fmt.Printf("History index: %d pindex: %d used: %d\n",
		history.index, history.pindex, history.used)
	fmt.Printf("commands: %v\n", history.cmds[0:MaxHistroyCmds])
}

// 获取历史命令
func getHistoryCmd(key byte) string {
	if key == TELKEY_UP {
		history.index--
		if history.index < 0 {
			history.index = history.used
		}
	} else if key == TELKEY_DOWN {
		history.index++
		if history.index >= history.used {
			history.index = history.used
		}
	} else {
		return ""
	}
	//printHistoryCmd()
	//fmt.Printf("index: %d used: %d %s\n", history.index, history.used, history.cmds[history.index])
	return history.cmds[history.index]
}

// 处理telnet 连接命令
func handleTermCmd(c *net.TCPConn, cmd string) error {
	argv := splitBuff(cmd)
	//fmt.Println(argv)
	if len(argv) == 0 {
		return nil
	}
	if argv[0] == "" {
		if lastCmd != nil {
			res, err := lastCmd.fn(lastArgv)
			c.Write([]byte(res))
			c.Write([]byte("\r\n"))
			return err
		}
		return nil
	}

	if argv[0] == "exit" || argv[0] == "quit" || argv[0] == "bye" {
		c.Close()
		return nil
	}

	console, ok := termHandler[argv[0]]
	if !ok {
		c.Write([]byte(fmt.Sprintf("Not found term command %s\r\n", argv[0])))
		return nil
	}

	// 检查参数个数是否合法
	if len(argv) < console.minParams || len(argv) > console.maxParams {
		c.Write([]byte(fmt.Sprintf("Params of command %s should be %d - %d\r\n",
			argv[0], console.minParams, console.maxParams)))
		return nil
	}

	if console.repeatable {
		lastCmd = &console
		lastArgv = argv
	} else {
		lastCmd = nil
	}

	res, err := console.fn(argv)
	c.Write([]byte(res))
	c.Write([]byte("\r\n"))

	return err
}
