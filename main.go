package main

import (
	"fmt"
	"log"
	"strconv"
	"strings"
	"time"

	"go.bug.st/serial"
)

func main() {
	// portName := flag.String("port", "/dev/ttyUSB0", "Serial port")
	portName := "/dev/ttyUSB0"
	// channel := flag.Int("ch", 2, "Channel (1 or 2)")
	// channel := 2
	// output := flag.String("out", "plot.png", "Output PNG")
	// flag.Parse()

	mode := &serial.Mode{
		BaudRate: 19200,
		StopBits: serial.OneStopBit,
		Parity:   serial.NoParity,
	}

	port, err := serial.Open(portName, mode)
	if err != nil {
		log.Fatal(err)
	}

	port.Write([]byte("DATA:SOURCE CH1\n"))
	time.Sleep(200 * time.Millisecond)
	n, err := port.Write([]byte("WFMPRE?\n"))
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("Sent %v bytes\n", n)

	ymult, yzero, yoff, xincr := parseWFMPRE(readAll(port, 2000))
	fmt.Printf("YMULT: %e\nYZERO: %e\nY_OFF: %e\nXINCR: %e\n", ymult, yzero, yoff, xincr)
}

func readAll(port serial.Port, timeout_ms int) string {
	var sb strings.Builder
	buf := make([]byte, 1024)
	port.SetReadTimeout(time.Duration(timeout_ms) * time.Millisecond)

	for {
		n, err := port.Read(buf)
		if err != nil || n == 0 {
			break
		}
		sb.Write(buf[:n])
	}

	return sb.String()
}

func parseCurve(s string) []int {
	parts := strings.Split(strings.TrimSpace(s), ",")
	out := make([]int, 0, len(parts))

	for _, p := range parts {
		v, err := strconv.Atoi(strings.TrimSpace(p))
		if err == nil {
			out = append(out, v)
		}
	}
	return out
}

func parseWFMPRE(s string) (ymult, yzero, yoff, xincr float64) {
	// separar por ;
	parts := strings.Split(s, ";")

	// según Tektronix:
	// index típicos:
	// 8 -> YMULT
	// 9 -> YZERO
	// 10 -> YOFF
	// 11 -> XUNIT (skip)
	// 12 -> XINCR

	ymult, _ = strconv.ParseFloat(parts[8], 64)
	yzero, _ = strconv.ParseFloat(parts[9], 64)
	yoff, _ = strconv.ParseFloat(parts[10], 64)
	xincr, _ = strconv.ParseFloat(parts[12], 64)

	return
}
