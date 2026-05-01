package main

import (
	"flag"
	"fmt"
	"image/color"
	"log"
	"math"
	"strconv"
	"strings"
	"time"

	"go.bug.st/serial"
	"gonum.org/v1/plot"
	"gonum.org/v1/plot/plotter"
	"gonum.org/v1/plot/vg"
)

type ChannelData struct {
	pts   plotter.XYs
	yunit string
	xunit string
}

var (
	bgColor      = color.RGBA{R: 39, G: 36, B: 45, A: 255}
	gridColor    = color.RGBA{R: 70, G: 65, B: 80, A: 255}
	axisColor    = color.RGBA{R: 180, G: 175, B: 190, A: 255}
	zeroRefColor = color.RGBA{R: 120, G: 115, B: 130, A: 220}
	textColor    = color.RGBA{R: 210, G: 205, B: 220, A: 255}
)

func main() {
	portName := flag.String("port", "/dev/ttyUSB0", "Serial port")
	baudRate := flag.Int("baud", 19200, "Baud rate")
	ch1Enable := flag.Bool("ch1", true, "Capture channel 1")
	ch2Enable := flag.Bool("ch2", true, "Capture channel 2")
	outFile := flag.String("out", "waveform.png", "Output image file (.png, .svg, .pdf)")
	flag.Parse()

	mode := &serial.Mode{
		BaudRate: *baudRate,
		StopBits: serial.OneStopBit,
		Parity:   serial.NoParity,
	}

	port, err := serial.Open(*portName, mode)
	if err != nil {
		log.Fatal(err)
	}
	defer port.Close()

	channels := []int{}
	if *ch1Enable {
		channels = append(channels, 1)
	}
	if *ch2Enable {
		channels = append(channels, 2)
	}
	if len(channels) == 0 {
		log.Fatal("No channels selected")
	}

	captured := make(map[int]ChannelData)

	for _, ch := range channels {
		fmt.Printf("--- Capturing CH%d ---\n", ch)

		fmt.Fprintf(port, "DATA:SOURCE CH%d\n", ch)
		time.Sleep(200 * time.Millisecond)

		fmt.Fprint(port, "WFMPRE?\n")
		wfmRaw := readAll(port, 2000)

		ymult, yzero, yoff, xincr, xzero, xunit, yunit, err := parseWFMPRE(wfmRaw, ch)
		if err != nil {
			log.Fatalf("CH%d WFMPRE: %v", ch, err)
		}
		fmt.Printf("YMULT: %e  YZERO: %e  YOFF: %e  XINCR: %e\n", ymult, yzero, yoff, xincr)

		fmt.Printf("Requesting CURVE? for CH%d (this may take a while...)\n", ch)
		fmt.Fprint(port, "CURVE?\n")
		curveRaw := readAll(port, 15000)

		raw := parseCurve(curveRaw)
		if len(raw) == 0 {
			log.Fatalf("CH%d: no curve data received", ch)
		}
		fmt.Printf("CH%d: received %d samples\n", ch, len(raw))

		pts := make(plotter.XYs, len(raw))
		for i, v := range raw {
			pts[i].X = xzero + float64(i)*xincr
			pts[i].Y = (float64(v)-yoff)*ymult + yzero
		}

		captured[ch] = ChannelData{pts: pts, xunit: xunit, yunit: yunit}
	}

	if err := renderPlot(captured, channels, *outFile); err != nil {
		log.Fatal(err)
	}
	fmt.Printf("Plot saved to %s\n", *outFile)
}

func renderPlot(captured map[int]ChannelData, channels []int, outFile string) error {
	p := plot.New()

	p.Title.Text = "Tektronix TDS 2000"
	p.Title.TextStyle.Font.Size = 14
	p.Title.TextStyle.Color = textColor
	p.BackgroundColor = bgColor

	// Canvas
	p.X.Color = axisColor
	p.Y.Color = axisColor
	p.X.Label.TextStyle.Color = textColor
	p.Y.Label.TextStyle.Color = textColor
	p.X.Tick.Label.Color = textColor
	p.Y.Tick.Label.Color = textColor
	p.X.Tick.Color = axisColor
	p.Y.Tick.Color = axisColor

	first := captured[channels[0]]
	p.X.Label.Text = fmt.Sprintf("Time (%s)", first.xunit)
	p.Y.Label.Text = fmt.Sprintf("Voltage (%s)", first.yunit)

	// Grid
	grid := plotter.NewGrid()
	grid.Horizontal.Color = gridColor
	grid.Vertical.Color = gridColor
	grid.Horizontal.Dashes = []vg.Length{vg.Points(2), vg.Points(4)}
	grid.Vertical.Dashes = []vg.Length{vg.Points(2), vg.Points(4)}
	p.Add(grid)

	// Zero reference line
	xMin, xMax := math.MaxFloat64, -math.MaxFloat64
	for _, ch := range channels {
		data := captured[ch]
		if data.pts[0].X < xMin {
			xMin = data.pts[0].X
		}
		if data.pts[len(data.pts)-1].X > xMax {
			xMax = data.pts[len(data.pts)-1].X
		}
	}
	zeroLine := plotter.NewFunction(func(float64) float64 { return 0 })
	zeroLine.LineStyle.Width = vg.Points(0.8)
	zeroLine.LineStyle.Color = zeroRefColor
	zeroLine.LineStyle.Dashes = []vg.Length{vg.Points(4), vg.Points(4)}
	zeroLine.XMin = xMin
	zeroLine.XMax = xMax
	p.Add(zeroLine)

	// Channel waveforms
	for _, ch := range channels {
		data := captured[ch]

		line, err := plotter.NewLine(data.pts)
		if err != nil {
			return err
		}
		line.LineStyle.Width = vg.Points(1.2)
		line.LineStyle.Color = channelColor(ch)

		min, max, rms := waveformStats(data.pts)
		label := fmt.Sprintf("CH%d  Min: %.3f%s  Max: %.3f%s  RMS: %.3f%s",
			ch, min, data.yunit, max, data.yunit, rms, data.yunit)

		p.Add(line)
		p.Legend.Add(label, line)
	}

	p.Legend.Top = true
	p.Legend.Left = true
	p.Legend.TextStyle.Color = textColor
	p.Legend.YOffs = 15 * vg.Points(1)
	// p.Legend.TextStyle.Color = color.RGBA{R: 50, G: 47, B: 58, A: 200}
	// p.Legend.BorderColor = gridColor

	return p.Save(14*vg.Inch, 5*vg.Inch, outFile)
}

func channelColor(ch int) color.Color {
	switch ch {
	case 1:
		return color.RGBA{R: 255, G: 210, B: 0, A: 255} // yellow
	case 2:
		return color.RGBA{R: 0, G: 185, B: 255, A: 255} // cyan
	case 3:
		return color.RGBA{R: 255, G: 100, B: 0, A: 255} // orange
	case 4:
		return color.RGBA{R: 200, G: 80, B: 255, A: 255} // purple
	default:
		return color.RGBA{R: 0, G: 220, B: 100, A: 255} // green
	}
}

func waveformStats(pts plotter.XYs) (min, max, rms float64) {
	min, max = pts[0].Y, pts[0].Y
	sumSq := 0.0
	for _, p := range pts {
		if p.Y < min {
			min = p.Y
		}
		if p.Y > max {
			max = p.Y
		}
		sumSq += p.Y * p.Y
	}
	rms = math.Sqrt(sumSq / float64(len(pts)))
	return
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
	if idx := strings.IndexByte(s, '#'); idx >= 0 {
		s = s[idx+1:]
		if len(s) > 1 {
			nDigits := int(s[0] - '0')
			if len(s) > 1+nDigits {
				s = s[1+nDigits:]
			}
		}
	}
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

func parseWFMPRE(s string, channel int) (ymult, yzero, yoff, xincr, xzero float64, xunit, yunit string, err error) {
	parts := strings.Split(s, ";")
	if len(parts) < 16 {
		return 0, 0, 0, 0, 0, "", "", fmt.Errorf("could not parse WFMPRE for channel %d (got %d fields)", channel, len(parts))
	}
	xincr, _ = strconv.ParseFloat(parts[8], 64)
	xzero, _ = strconv.ParseFloat(parts[10], 64)
	xunit = strings.Trim(parts[11], "\"")
	ymult, _ = strconv.ParseFloat(parts[12], 64)
	yzero, _ = strconv.ParseFloat(parts[13], 64)
	yoff, _ = strconv.ParseFloat(parts[14], 64)
	yunit = strings.Trim(parts[15], "\"\n")
	return
}

