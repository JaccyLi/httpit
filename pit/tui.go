package pit

import (
	"io"
	"math"
	"os"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/charmbracelet/bubbles/progress"
	tea "github.com/charmbracelet/bubbletea"
	lg "github.com/charmbracelet/lipgloss"
	"github.com/valyala/bytebufferpool"
	"github.com/valyala/fasthttp"
)

const (
	done         = 1
	fieldWidth   = 18
	defaultFps   = time.Duration(40)
	padding      = 2
	maxWidth     = 66
	processColor = "#444"
)

type tui struct {
	r io.Reader
	w io.Writer

	throughput *int64
	reqs       int64
	elapsed    int64
	code1xx    int64
	code2xx    int64
	// =============
	code3xx    int64
	code301    int64 // 301 Moved Permanently
	code302    int64 // 302 Found
	code303    int64 // 303 See Other
	code304    int64 // 304 Not Modified
	code307    int64 // 307 Temporary Redirect
	code308    int64 // 308 Permanent Redirect
	code4xx    int64
	code400    int64 // 400 Bad Request
	code401    int64 // 401 Unauthorized
	code403    int64 // 403 Forbidden
	code404    int64 //
	code405    int64 // 405 Method Not Allowed
	code5xx    int64
	code500    int64 // 500 Internal Server Error
	code502    int64 // 502 Bad Gateway
	code503    int64 // 503 Service Unavailable
	code504    int64 // 504 Gateway Timeout
	code505    int64 // 505 HTTP Version Not Supported
	codeOthers int64
	latencies  []int64
	rps        []float64
	mut        sync.Mutex
	errs       map[string]int
	buf        *bytebufferpool.ByteBuffer

	url         string
	count       int
	duration    time.Duration
	connections int
	initCmd     tea.Cmd
	progressBar *progress.Model
	quitting    bool
	done        bool
}

func newTui() *tui {
	progressBar, _ := progress.NewModel(progress.WithSolidFill(processColor))

	return &tui{
		r:           os.Stdin,
		w:           os.Stdout,
		errs:        make(map[string]int),
		buf:         bytebufferpool.Get(),
		progressBar: progressBar,
	}
}

func (t *tui) start() error {
	return tea.NewProgram(t, tea.WithInput(t.r), tea.WithOutput(t.w)).Start()
}

func (t *tui) Init() tea.Cmd {
	return tea.Batch(tickNow, t.initCmd)
}

func (t *tui) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {

	case tea.KeyMsg:
		switch msg.String() {
		case "q":
			fallthrough
		case "esc":
			fallthrough
		case "ctrl+c":
			t.quitting = true
			return t, tea.Quit
		default:
			return t, nil
		}
	case tea.WindowSizeMsg:
		t.progressBar.Width = msg.Width - padding*2 - 4
		if t.progressBar.Width > maxWidth {
			t.progressBar.Width = maxWidth
		}
		return t, nil

	case int:
		var cmd tea.Cmd
		if msg == done {
			t.done = true
			cmd = tea.Quit
		}
		return t, cmd

	default:
		return t, tick()
	}

}

func (t *tui) View() string {
	return t.output()
}

func (t *tui) appendCode(code int) {
	switch code / 100 {
	case 1:
		t.code1xx++
	case 2:
		t.code2xx++
	case 3:
		if code == 301 {
			t.code301++
		}
		if code == 302 {
			t.code302++
		}
		if code == 303 {
			t.code303++
		}
		if code == 304 {
			t.code304++
		}
		if code == 307 {
			t.code307++
		}
		if code == 308 {
			t.code308++
		}
		t.code3xx++
	case 4:
		if code == 400 {
			t.code400++
		}
		if code == 401 {
			t.code401++
		}
		if code == 403 {
			t.code403++
		}
		if code == 404 {
			t.code404++
		}
		if code == 405 {
			t.code405++
		}
		t.code4xx++
	case 5:
		// ==================================================================
		if code == 500 {
			t.code500++
		}
		if code == 502 {
			t.code502++
		}
		if code == 503 {
			t.code503++
		}
		if code == 504 {
			t.code504++
		}
		if code == 505 {
			t.code504++
		}
		t.code5xx++
	default:
		t.codeOthers++
	}
}

func (t *tui) appendRps(rps float64) {
	t.rps = append(t.rps, rps)
}

func (t *tui) appendLatency(latency time.Duration) {
	t.latencies = append(t.latencies, latency.Microseconds())
}

func (t *tui) appendError(err error) {
	t.mut.Lock()
	t.errs[err.Error()]++
	t.mut.Unlock()
}

func (t *tui) output() string {
	t.buf.Reset()

	t.writeTitle()
	t.writeProcessBar()
	t.writeTotalRequest()
	t.writeElapsed()
	t.writeThroughput()
	t.writeStatistics()
	t.writeCodes()
	t.writeErrors()
	t.writeHint()

	return t.buf.String()
}

func (t *tui) writeTitle() {
	_, _ = t.buf.WriteString("Benchmarking ")
	_, _ = t.buf.WriteString(t.url)
	_, _ = t.buf.WriteString(" with ")
	t.writeInt(t.connections)
	_, _ = t.buf.WriteString(" connections\n")
}

func (t *tui) writeProcessBar() {
	var percent float64
	if t.count != 0 {
		percent = float64(atomic.LoadInt64(&t.reqs)) / float64(t.count)
	} else {
		percent = float64(atomic.LoadInt64(&t.elapsed)) / float64(t.duration)
	}

	if percent > 1.0 {
		percent = 1.0
	}

	_, _ = t.buf.WriteString(t.progressBar.View(percent))
	_ = t.buf.WriteByte('\n')
}

func (t *tui) writeTotalRequest() {
	_, _ = t.buf.WriteString("Requests:  ")
	t.writeInt(int(atomic.LoadInt64(&t.reqs)))
	if t.count != 0 {
		_ = t.buf.WriteByte('/')
		t.writeInt(t.count)
	}
	_, _ = t.buf.WriteString("  ")
}

func (t *tui) writeElapsed() {
	elapsed := time.Duration(atomic.LoadInt64(&t.elapsed))
	_, _ = t.buf.WriteString("Elapsed:  ")
	if elapsed > t.duration {
		elapsed = t.duration
	}
	t.writeFloat(elapsed.Seconds())
	if t.count == 0 {
		_ = t.buf.WriteByte('/')
		t.writeFloat(t.duration.Seconds())
	}
	_, _ = t.buf.WriteString("s  ")
}

func (t *tui) writeThroughput() {
	_, _ = t.buf.WriteString("Throughput:  ")
	elapsed := time.Duration(atomic.LoadInt64(&t.elapsed))
	if seconds := elapsed.Seconds(); seconds != 0 {
		throughput, unit := formatThroughput(float64(atomic.LoadInt64(t.throughput)) / seconds)
		t.writeFloat(throughput)
		_ = t.buf.WriteByte(' ')
		_, _ = t.buf.WriteString(unit)
	} else {
		_, _ = t.buf.WriteString("0 B/s")
	}
	_ = t.buf.WriteByte('\n')
}

func (t *tui) writeStatistics() {
	_, _ = t.buf.WriteString(lg.NewStyle().Width(12).Align(lg.Center).Render("Statistics  "))

	_, _ = t.buf.WriteString(lg.NewStyle().Width(fieldWidth).Align(lg.Center).Render("Avg"))
	_, _ = t.buf.WriteString(lg.NewStyle().Width(fieldWidth).Align(lg.Center).Render("Stdev"))
	_, _ = t.buf.WriteString(lg.NewStyle().Width(fieldWidth).Align(lg.Center).Render("Max"))
	_ = t.buf.WriteByte('\n')

	rpsAvg, rpsStdev, rpsMax := rpsResult(t.rps)
	_, _ = t.buf.WriteString(lg.NewStyle().Width(12).Align(lg.Center).Render("Reqs/sec  "))

	t.writeRps(rpsAvg)
	t.writeRps(rpsStdev)
	t.writeRps(rpsMax)
	_ = t.buf.WriteByte('\n')

	latencyAvg, latencyStdev, latencyMax := latencyResult(t.latencies)
	_, _ = t.buf.WriteString(lg.NewStyle().Width(12).Align(lg.Center).Render("Latency  "))
	t.writeLatency(latencyAvg)
	t.writeLatency(latencyStdev)
	t.writeLatency(latencyMax)
	_ = t.buf.WriteByte('\n')
}

func (t *tui) writeRps(rps float64) {
	s := strconv.FormatFloat(rps, 'f', 2, 64)
	_, _ = t.buf.WriteString(lg.NewStyle().Width(fieldWidth).Align(lg.Center).Render(s))
}

func (t *tui) writeLatency(latency float64) {
	s := strconv.FormatFloat(latency, 'f', 2, 64)
	_, _ = t.buf.WriteString(lg.NewStyle().Width(fieldWidth).Align(lg.Center).Render(s + "ms"))
}

func (t *tui) writeCodes() {
	_, _ = t.buf.WriteString("HTTP codes:\n  ")

	_, _ = t.buf.WriteString("1xx - ")
	t.writeInt(int(atomic.LoadInt64(&t.code1xx)), "#ffaf00")
	_, _ = t.buf.WriteString(", ")

	_, _ = t.buf.WriteString("2xx - ")
	t.writeInt(int(atomic.LoadInt64(&t.code2xx)), "#00ff00")
	_, _ = t.buf.WriteString("\n  ")

	_, _ = t.buf.WriteString("3xx - ")
	t.writeInt(int(atomic.LoadInt64(&t.code3xx)), "#ffff00")
	_, _ = t.buf.WriteString("|")
	_, _ = t.buf.WriteString("301 - ")
	t.writeInt(int(atomic.LoadInt64(&t.code301)), "#ffff00")
	_, _ = t.buf.WriteString("|")
	_, _ = t.buf.WriteString("302 - ")
	t.writeInt(int(atomic.LoadInt64(&t.code302)), "#ffff00")
	_, _ = t.buf.WriteString("|")
	_, _ = t.buf.WriteString("303 - ")
	t.writeInt(int(atomic.LoadInt64(&t.code303)), "#ffff00")
	_, _ = t.buf.WriteString("|")
	_, _ = t.buf.WriteString("304 - ")
	t.writeInt(int(atomic.LoadInt64(&t.code304)), "#ffff00")
	_, _ = t.buf.WriteString("|")
	_, _ = t.buf.WriteString("307 - ")
	t.writeInt(int(atomic.LoadInt64(&t.code307)), "#ffff00")
	_, _ = t.buf.WriteString("|")
	_, _ = t.buf.WriteString("308 - ")
	t.writeInt(int(atomic.LoadInt64(&t.code308)), "#ffff00")
	_, _ = t.buf.WriteString("\n  ")

	_, _ = t.buf.WriteString("4xx - ")
	t.writeInt(int(atomic.LoadInt64(&t.code4xx)), "#ff8700")
	_, _ = t.buf.WriteString("|")
	_, _ = t.buf.WriteString("400 - ")
	t.writeInt(int(atomic.LoadInt64(&t.code400)), "#ff8700")
	_, _ = t.buf.WriteString("|")
	_, _ = t.buf.WriteString("401 - ")
	t.writeInt(int(atomic.LoadInt64(&t.code401)), "#ff8700")
	_, _ = t.buf.WriteString("|")
	_, _ = t.buf.WriteString("403 - ")
	t.writeInt(int(atomic.LoadInt64(&t.code403)), "#ff8700")
	_, _ = t.buf.WriteString("|")
	_, _ = t.buf.WriteString("404 - ")
	t.writeInt(int(atomic.LoadInt64(&t.code404)), "#ff8700")
	_, _ = t.buf.WriteString("|")
	_, _ = t.buf.WriteString("405 - ")
	t.writeInt(int(atomic.LoadInt64(&t.code405)), "#ff8700")
	_, _ = t.buf.WriteString("\n  ")

	_, _ = t.buf.WriteString("5xx - ")
	t.writeInt(int(atomic.LoadInt64(&t.code5xx)), "#870000")
	_, _ = t.buf.WriteString("|")
	_, _ = t.buf.WriteString("500 - ")
	t.writeInt(int(atomic.LoadInt64(&t.code500)), "#444")
	_, _ = t.buf.WriteString("|")
	_, _ = t.buf.WriteString("502 - ")
	t.writeInt(int(atomic.LoadInt64(&t.code502)), "#444")
	_, _ = t.buf.WriteString("|")
	_, _ = t.buf.WriteString("503 - ")
	t.writeInt(int(atomic.LoadInt64(&t.code503)), "#444")
	_, _ = t.buf.WriteString("|")
	_, _ = t.buf.WriteString("504 - ")
	t.writeInt(int(atomic.LoadInt64(&t.code504)), "#444")
	_, _ = t.buf.WriteString("|")
	_, _ = t.buf.WriteString("505 - ")
	t.writeInt(int(atomic.LoadInt64(&t.code505)), "#444")
	_, _ = t.buf.WriteString("\n")

	_, _ = t.buf.WriteString("Others - ")
	t.writeInt(int(atomic.LoadInt64(&t.codeOthers)), "#444")
	_, _ = t.buf.WriteString("\n")
}

func (t *tui) writeErrors() {
	t.mut.Lock()
	defer t.mut.Unlock()

	if len(t.errs) == 0 {
		return
	}
	_, _ = t.buf.WriteString("Errors:\n")
	for err, count := range t.errs {
		_, _ = t.buf.WriteString("  ")
		_, _ = t.buf.WriteString(lg.NewStyle().Underline(true).Render(err))
		_, _ = t.buf.WriteString(": ")
		t.writeInt(count)
		_ = t.buf.WriteByte('\n')
	}
}

func (t *tui) writeHint() {
	if t.done {
		_, _ = t.buf.WriteString(lg.NewStyle().Background(lg.Color("#008700")).Render(" Done! \n"))
	} else if t.quitting {
		_, _ = t.buf.WriteString(lg.NewStyle().Background(lg.Color("#870000")).Render(" Terminated! \n"))
	} else {
		_, _ = t.buf.WriteString(lg.NewStyle().Background(lg.Color("#444")).Render(" press q/esc/ctrl+c to quit "))
	}
}

func (t *tui) writeInt(i int, colorStr ...string) {
	if i <= 0 || len(colorStr) == 0 {
		t.buf.B = fasthttp.AppendUint(t.buf.B, i)
		return
	}

	_, _ = t.buf.WriteString(lg.NewStyle().Foreground(lg.Color(colorStr[0])).Render(strconv.Itoa(i)))
}

func (t *tui) writeFloat(f float64) {
	t.buf.B = strconv.AppendFloat(t.buf.B, f, 'f', 2, 64)
}

func rpsResult(rps []float64) (avg float64, stdev float64, max float64) {
	l := len(rps)
	if l == 0 {
		return
	}

	var sum, sum2 float64
	for _, r := range rps {
		sum += r
		if r > max {
			max = r
		}
	}

	avg = sum / float64(l)

	var diff float64
	for _, r := range rps {
		diff = avg - r
		sum2 += diff * diff
	}

	stdev = math.Sqrt(sum2 / float64(l-1))

	return
}

func latencyResult(latencies []int64) (avg float64, stdev float64, max float64) {
	l := len(latencies)
	if l == 0 {
		return
	}

	var sum float64
	for _, latency := range latencies {
		// us -> ms
		r := float64(latency) / 1000
		sum += r
		if r > max {
			max = r
		}
	}

	avg = sum / float64(l)

	var diff, sum2 float64
	for _, latency := range latencies {
		// us -> ms
		r := float64(latency) / 1000
		diff = avg - r
		sum2 += diff * diff
	}

	stdev = math.Sqrt(sum2 / float64(l-1))

	return
}

func formatThroughput(throughput float64) (float64, string) {
	switch {
	case throughput < 1e3:
		return throughput, "B/s"
	case throughput < 1e6:
		return throughput / 1e3, "KB/s"
	case throughput < 1e9:
		return throughput / 1e6, "MB/s"
	default:
		return throughput / 1e9, "GB/s"
	}
}

type tickMsg struct {
	Time time.Time
}

func tickNow() tea.Msg {
	return tickMsg{Time: time.Now()}
}

func tick() tea.Cmd {
	return tea.Tick(time.Second/defaultFps, func(t time.Time) tea.Msg {
		return tickMsg{Time: t}
	})
}
