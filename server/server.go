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
	arenaHeight = 22

	boardHorWidth         = 2
	boardVerHeight        = 1
	defaultBoardVelocityX = 1
	defaultBoardVelocityY = 1

	defaultBallVelocityX = 1.5 * boardHorWidth / fps
	defaultBallVelocityY = 1.5 * boardVerHeight / fps

	gameModeFastBallVelocityScalar   = 3
	gameModeFastBoardVelocityScalar  = 1
	gameModeDoubleBallVelocityScalar = 1.5

	// only store flagDouble's plaintext in program
	// flagFast   = "HW1{d0_y0u_knovv_wH0_KaienLin_1s?}"
	flagFast1  = "HW1{DoubleGunKaiDislikeGrepAndStrings}"
	flagFast2  = "\x00\x00\x00\x00 _*\x1b\\\x10\x18\x1e\x00$\x17\x1f\x1b\x1e;\\6 \x04.\x17\x0b<(\x00;b\x07M\x14"
	flagDouble = "HW1{Dou8l3_b@ll_d0uB1e_Fun!}"
)

const (
	actionNone = iota
	actionUp
	actionDown
	actionLeft
	actionRight
)

const (
	gameModeDefault = iota
	gameModeFast
	gameModeDouble
)

var serverIP = flag.String("bind_addr", "0.0.0.0", "bind address of game server")
var serverPort = flag.Int("port", 9393, "port number of game server")
var isDaemon = flag.Bool("daemon", false, "whether server is a daemon")

func main() {
	flag.Parse()
	setupLogger()

	go runServer("secret", portSecret, secretHandler)
	runServer("game", *serverPort, gameHandler)
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
	mode int

	horizontal float64 // the x coordinate of left top corner of horizontal board
	vertical   float64 // the y coordinate of left top corner of vertical board

	ballx     []float64 // ball width is 1
	bally     []float64 // ball height is 1
	ballDirx  []int     // +1 or -1
	ballDiry  []int     // +1 or -1
	countdown int       // stores the number of remaining ticks

	ballVelocityX  float64
	ballVelocityY  float64
	boardVelocityX float64
	boardVelocityY float64
}

func (env gameEnv) String() string {
	result := fmt.Sprintf("horizontal: %d\n", int(env.horizontal))
	result += fmt.Sprintf("vertical: %d\n", int(env.vertical))
	{
		result += fmt.Sprintf("ballx:")
		for _, val := range env.ballx {
			result += fmt.Sprintf(" %v", int(val))
		}
		result += fmt.Sprintf("\n")
	}
	{
		result += fmt.Sprintf("bally:")
		for _, val := range env.bally {
			result += fmt.Sprintf(" %v", int(val))
		}
		result += fmt.Sprintf("\n")
	}
	result += fmt.Sprintf("countdown: %d\n", int(float64(env.countdown)/fps+1))

	return result
}

func newGame(gameModeStr string) *gameEnv {
	env := new(gameEnv)

	switch gameModeStr {
	case "fast":
		env.mode = gameModeFast
	case "double":
		env.mode = gameModeDouble
	default:
		env.mode = gameModeDefault
	}

	// default values
	env.horizontal = arenaWidth / 2
	env.vertical = arenaHeight / 2

	env.ballx = []float64{float64(arenaWidth/2 + getRandSign()*(2+rand.Intn(2)))}
	env.bally = []float64{float64(arenaHeight/2 + getRandSign()*(2+rand.Intn(4)))}
	env.ballDirx = []int{getRandSign()}
	env.ballDiry = []int{getRandSign()}
	env.countdown = duration * fps

	env.ballVelocityX = defaultBallVelocityX
	env.ballVelocityY = defaultBallVelocityY
	env.boardVelocityX = defaultBoardVelocityX
	env.boardVelocityY = defaultBoardVelocityY

	// special tuning
	if env.mode == gameModeFast {
		env.ballVelocityX *= gameModeFastBallVelocityScalar
		env.ballVelocityY *= gameModeFastBallVelocityScalar
		env.boardVelocityX *= gameModeFastBoardVelocityScalar
		env.boardVelocityY *= gameModeFastBoardVelocityScalar
	}

	if env.mode == gameModeDouble {
		env.ballVelocityX *= gameModeDoubleBallVelocityScalar
		env.ballVelocityY *= gameModeDoubleBallVelocityScalar
		env.ballx = []float64{float64(arenaWidth/2 - arenaWidth/4 + 1), float64(arenaWidth/2 + arenaWidth/4 - 1)}
		env.bally = []float64{float64(arenaHeight/2 - arenaHeight/4 - 2), float64(arenaHeight/2 + arenaHeight/4)}
		tmp := getRandSign()
		env.ballDirx = []int{tmp, tmp}
		env.ballDiry = []int{-tmp, -tmp}
	}

	return env
}

func gameHandler(conn net.Conn) {
	rand.Seed(time.Now().UnixNano())
	ticker := time.NewTicker(time.Second / fps)

	// receive "start" from client
	var env *gameEnv
	{
		buf := make([]byte, 20)
		n, err := conn.Read(buf)
		if err != nil {
			return
		}
		buf = buf[:n]

		modeStr := strings.Fields(string(buf))[1]
		log.Printf("start game (mode: %v), remote = %v", modeStr, conn.RemoteAddr())
		defer log.Printf("end game (mode: %v), remote = %v", modeStr, conn.RemoteAddr())

		env = newGame(modeStr)
	}

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

	// main game loop
	for {
		select {
		case <-ticker.C:
			// update board position
			switch clientAction {
			case actionUp:
				env.vertical = math.Max(env.vertical-env.boardVelocityY, 0)
			case actionDown:
				env.vertical = math.Min(env.vertical+env.boardVelocityY, arenaHeight-boardVerHeight)
			case actionLeft:
				env.horizontal = math.Max(env.horizontal-env.boardVelocityX, 0)
			case actionRight:
				env.horizontal = math.Min(env.horizontal+env.boardVelocityX, arenaWidth-boardHorWidth)
			}
			clientAction = actionNone

			// update ball position
			var ballOutOfBound bool
			for i := range env.ballx {
				env.ballx[i] += float64(env.ballDirx[i]) * env.ballVelocityX
				if env.ballx[i] >= float64(arenaWidth-1) || env.ballx[i] <= 1 {
					if env.vertical <= env.bally[i] && env.bally[i] <= env.vertical+float64(boardVerHeight) {
						env.ballDirx[i] *= -1
						env.ballx[i] = math.Max(math.Min(env.ballx[i], arenaWidth-1), 1)
					}
				}
				env.bally[i] += float64(env.ballDiry[i]) * env.ballVelocityY
				if env.bally[i] >= float64(arenaHeight-1) || env.bally[i] <= 1 {
					if env.horizontal <= env.ballx[i] && env.ballx[i] <= env.horizontal+float64(boardHorWidth) {
						env.ballDiry[i] *= -1
						env.bally[i] = math.Max(math.Min(env.bally[i], arenaHeight-1), 1)
					}
				}

				ballOutOfBound = ballOutOfBound || env.ballx[i] < 0 || env.ballx[i] > arenaWidth || env.bally[i] < 0 || env.bally[i] > arenaHeight
			}

			env.countdown--

			var message []byte
			var exit bool = false
			if env.countdown < 0 {
				switch env.mode {
				case gameModeFast:
					message = append([]byte("win "), getFlagFast()...)
					message = append(message, '\n')
				case gameModeDouble:
					message = []byte(fmt.Sprintf("win %v\n", flagDouble))
				default:
					message = []byte("win\n")
				}
				exit = true
			} else if ballOutOfBound {
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

func getFlagFast() []byte {
	result := make([]byte, len(flagFast2))
	for i := range flagFast2 {
		result[i] = flagFast1[i] ^ flagFast2[i]
	}
	return result
}

// ----------------------------------------------------------------------------
// secret

func secretHandler(conn net.Conn) {
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
