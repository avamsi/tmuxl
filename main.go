package main

import (
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"time"

	"github.com/avamsi/ergo"
)

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func tmux(args ...string) string {
	cmd := exec.Command("tmux", args...)
	// Let tmux chill between multiple commands.
	time.Sleep(42 * time.Millisecond)
	return string(ergo.Must1(cmd.CombinedOutput()))
}

func currentLayout() (width, height, panes int) {
	f := "[#{window_width}x#{window_height}:#{window_panes}]"
	s := tmux("display-message", "-p", f)
	ergo.Must1(fmt.Sscanf(s, "[%dx%d:%d]", &width, &height, &panes))
	return
}

func createPane(idx int) {
	i := fmt.Sprintf("%d", idx)
	s := tmux("split-window", "-c", "#{pane_current_path}", "-d", "-t", i)
	if s != "" {
		panic(s)
	}
}

type direction int

const (
	vertical = iota
	horizontal
)

type ratio struct {
	p, q int
}

var (
	equal      = ratio{50, 50}
	smallLarge = ratio{40, 60}
	largeSmall = ratio{60, 40}
)

type split struct {
	d      direction
	p1, p2 pane
}

type pane struct {
	id            int
	x, y          int
	width, height int
	s             *split
}

func (p *pane) split(d direction, r ratio) (*pane, *pane) {
	if p.s != nil {
		panic(p)
	}
	switch d {
	case vertical:
		// Minus 1 for the pane separator (here and below).
		top := (p.height - 1) * r.p / (r.p + r.q)
		bottom := p.height - top - 1
		p.s = &split{
			vertical,
			pane{
				id:     2*p.id + 1,
				x:      p.x,
				y:      p.y,
				width:  p.width,
				height: top,
			},
			pane{
				id:     2*p.id + 2,
				x:      p.x,
				y:      p.y + top + 1,
				width:  p.width,
				height: bottom,
			},
		}
		return &p.s.p1, &p.s.p2
	case horizontal:
		left := (p.width - 1) * r.p / (r.p + r.q)
		right := p.width - left - 1
		p.s = &split{
			horizontal,
			pane{
				id:     2*p.id + 1,
				x:      p.x,
				y:      p.y,
				width:  left,
				height: p.height,
			},
			pane{
				id:     2*p.id + 2,
				x:      p.x + left + 1,
				y:      p.y,
				width:  right,
				height: p.height,
			},
		}
		return &p.s.p1, &p.s.p2
	}
	panic(d)
}

func (p *pane) layout() string {
	l := fmt.Sprintf("%dx%d,%d,%d", p.width, p.height, p.x, p.y)
	if p.s != nil {
		l1, l2 := p.s.p1.layout(), p.s.p2.layout()
		switch p.s.d {
		case vertical:
			return fmt.Sprintf("%s[%s,%s]", l, l1, l2)
		case horizontal:
			return fmt.Sprintf("%s{%s,%s}", l, l1, l2)
		}
		panic(p.s)
	}
	return fmt.Sprintf("%s,%d", l, p.id)
}

func computeLayout(width, height, n int) string {
	p := pane{id: 0, x: 0, y: 0, width: width, height: height}
	if n > 1 {
		top, bottom := p.split(vertical, equal)
		if n > 2 {
			bottom.split(horizontal, smallLarge)
			if n > 3 {
				_, topRight := top.split(horizontal, largeSmall)
				if n > 4 {
					topRight.split(vertical, equal)
				}
			}
		}
	}
	return p.layout()
}

// From https://github.com/tmux/tmux/blob/493922dc4b15/layout-custom.c#L47.
func layoutChecksum(layout string) string {
	csum := 0
	for _, b := range []byte(layout) {
		csum = (csum >> 1) + ((csum & 1) << 15)
		csum += int(b)
	}
	return fmt.Sprintf("%04x", csum)
}

func selectLayout(layout string) {
	layout = fmt.Sprintf("%s,%s", layoutChecksum(layout), layout)
	if s := tmux("select-layout", layout); s != "" {
		panic(s)
	}
}

func adjustLayout(desired int) {
	width, height, current := currentLayout()
	if desired == 0 {
		desired = current
	} else if desired < current {
		fmt.Fprintf(os.Stderr,
			"tmuxl: expected n(=%d) to be >= current(=%d)\n", desired, current)
		os.Exit(1)
	}
	for i := current; i < desired; i++ {
		var idx int
		switch i {
		case 1, 3:
			// 2nd and 4th panes are split from the 1st (idx=0) pane.
			idx = 0
		case 2, 4:
			// 3rd pane is split from the 2nd (idx=1) pane.
			// 5th pane is split from the 4th pane but it's idx will be 1 as
			// tmux indices are incremented left to right and top to bottom.
			idx = 1
		}
		createPane(idx)
	}
	selectLayout(computeLayout(width, height, desired))
}

func main() {
	switch args := os.Args[1:]; len(args) {
	case 0:
		adjustLayout(0)
	case 1:
		n := ergo.Must1(strconv.Atoi(args[0]))
		if n <= 0 || n > 5 {
			fmt.Fprintf(os.Stderr, "tmuxl: expected 0 < n(=%d) <= 5\n", n)
			os.Exit(1)
		}
		adjustLayout(n)
	default:
		fmt.Fprintln(os.Stderr, "tmuxl: expected at most 1 argument, got", args)
		os.Exit(1)
	}
}
