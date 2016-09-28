# term
golang terminal


# simple usage

    import "github.com/guotie/term"

## create & start terminal

	ch := make(chan struct{})
	wg := sync.WaitGroup{}
    term.StartTermServer(9988)
	go term.TermRoutine(ch, &wg)
	
## stop terminal

    term.StopTermServer()
    
## register terminal commands

    RegisterTermCmd(cmd string, maxParams, minParams int, repeat bool, fn TermFunc)

TermFunc定义：

    type TermFunc func([]string) (string, error)
	
## usage

telnet localhost port
->

# todo

## help command
