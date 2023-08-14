package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"time"

	"github.com/avamsi/ergo/check"
	"golang.org/x/sync/errgroup"
)

const tmuxCoolOff = 250 * time.Millisecond

func tmux(ctx context.Context, args ...string) string {
	cmd := exec.CommandContext(ctx, "tmux", args...)
	cmd.Stderr = os.Stderr
	// Let tmux chill a little bit between multiple commands
	// (this seems to help with clean prompt rendering).
	time.Sleep(tmuxCoolOff)
	return string(check.Ok(cmd.Output()))
}

func currentLayout(ctx context.Context) (width, height, panes int) {
	f := "[#{window_width}x#{window_height}:#{window_panes}]"
	s := tmux(ctx, "display-message", "-p", f)
	check.Ok(fmt.Sscanf(s, "[%dx%d:%d]", &width, &height, &panes))
	return
}

func createPane(ctx context.Context, idx int) {
	i := strconv.Itoa(idx)
	s := tmux(ctx, "split-window", "-c", "#{pane_current_path}", "-d", "-t", i)
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
	csum := uint16(0)
	for _, b := range []byte(layout) {
		csum = (csum >> 1) + ((csum & 1) << 15)
		csum += uint16(b)
	}
	return fmt.Sprintf("%04x", csum)
}

func selectLayout(ctx context.Context, layout string) {
	layout = fmt.Sprintf("%s,%s", layoutChecksum(layout), layout)
	if s := tmux(ctx, "select-layout", layout); s != "" {
		panic(s)
	}
}

func adjustLayout(ctx context.Context, desired int) {
	g, ctx := errgroup.WithContext(ctx)
	defer func() {
		check.Nil(g.Wait())
	}()
	// Attach to a tmux session if we're not already under one.
	if _, ok := os.LookupEnv("TMUX"); !ok {
		g.Go(func() error {
			cmd := exec.Command("tmux", "attach-session")
			cmd.Stdin = os.Stdin
			cmd.Stdout = os.Stdout
			cmd.Stderr = os.Stderr
			cmd.Env = os.Environ()
			return cmd.Run()
		})
		// Give some time for tmux to attach to a session.
		time.Sleep(tmuxCoolOff)
	}
	width, height, current := currentLayout(ctx)
	if desired == 0 {
		desired = current
	} else if desired < current {
		fmt.Fprintf(os.Stderr,
			"tmuxl: expected n(=%d) to be >= current(=%d)\n", desired, current)
		return
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
		createPane(ctx, idx)
	}
	selectLayout(ctx, computeLayout(width, height, desired))
	// Focus the bottom left pane (for jjw).
	if current < 3 && desired >= 3 {
		tmux(ctx, "select-pane", "-t", strconv.Itoa(desired-2))
	}
}

func main() {
	ctx := context.Background()
	switch args := os.Args[1:]; len(args) {
	case 0:
		adjustLayout(ctx, 0)
	case 1:
		n := check.Ok(strconv.Atoi(args[0]))
		if n <= 0 || n > 5 {
			fmt.Fprintf(os.Stderr, "tmuxl: expected 0 < n(=%d) <= 5\n", n)
			return
		}
		adjustLayout(ctx, n)
	default:
		fmt.Fprintln(os.Stderr, "tmuxl: expected at most 1 argument, got", args)
	}
}
