package player

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"time"

	"gabe565.com/ascii-movie/internal/movie"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	zone "github.com/lrstanley/bubblezone"
)

func NewPlayer(m *movie.Movie, logger *slog.Logger, renderer *lipgloss.Renderer) *Player {
	if renderer == nil {
		renderer = lipgloss.DefaultRenderer()
	}

	playCtx, playCancel := context.WithCancel(context.Background())
	player := &Player{
		movie:      m,
		log:        logger,
		start:      time.Now(),
		zone:       zone.New(),
		speed:      1,
		styles:     NewStyles(m, renderer),
		playCtx:    playCtx,
		playCancel: playCancel,
	}

	return player
}

func NewSimplePlayer(m *movie.Movie, logger *slog.Logger, output io.Writer) *SimplePlayer {
	return &SimplePlayer{
		movie:  m,
		log:    logger,
		start:  time.Now(),
		speed:  1,
		output: output,
	}
}

type Player struct {
	movie *movie.Movie
	frame int
	log   *slog.Logger
	start time.Time
	zone  *zone.Manager

	speed      float64
	playCtx    context.Context
	playCancel context.CancelFunc

	styles Styles
}

type SimplePlayer struct {
	movie  *movie.Movie
	frame  int
	log    *slog.Logger
	start  time.Time
	speed  float64
	output io.Writer
}

func (p *Player) Init() tea.Cmd {
	return tick(p.playCtx, p.movie.Frames[p.frame].Duration, frameTickMsg{})
}

func (p *Player) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case frameTickMsg:
		var frameDiff int
		switch {
		case p.speed >= 0:
			frameDiff = 1
			if p.frame+frameDiff >= len(p.movie.Frames) {
				return p, tea.Quit
			}
		case p.frame <= 0:
			p.speed = 1
			p.pause()
			return p, nil
		default:
			frameDiff = -1
		}
		p.frame += frameDiff
		speed := p.speed
		if speed < 0 {
			speed *= -1
		}
		duration := p.movie.Frames[p.frame].CalcDuration(speed)
		for duration < time.Second/15 {
			if p.frame+frameDiff >= len(p.movie.Frames) {
				return p, tea.Quit
			} else if p.frame+frameDiff <= 0 {
				p.speed = 1
				p.pause()
				return p, nil
			}
			p.frame += frameDiff
			duration += p.movie.Frames[p.frame].CalcDuration(speed)
		}
		return p, tick(p.playCtx, duration, frameTickMsg{})
	case tea.WindowSizeMsg:
		p.styles.MarginX, p.styles.MarginY = "", ""
		if width := msg.Width/2 - p.movie.Width/2 - 1; width > 0 {
			p.styles.MarginX = strings.Repeat(" ", width)
		}
		if height := msg.Height/2 - lipgloss.Height(p.View())/2; height > 0 {
			p.styles.MarginY = strings.Repeat("\n", height)
		}
	}
	return p, nil
}

func (p *Player) View() string {
	content := lipgloss.JoinHorizontal(
		lipgloss.Top,
		p.styles.MarginX,
		lipgloss.JoinVertical(
			lipgloss.Center,
			p.styles.MarginY,
			p.styles.Screen.Render(p.movie.Frames[p.frame].Data),
			p.zone.Mark("progress", p.styles.Progress.Render(p.movie.Frames[p.frame].Progress)),
		),
	)

	return p.zone.Scan(content)
}

func (p *Player) pause() {
	p.clearTimeouts()
}

func (p *Player) play() tea.Cmd {
	p.clearTimeouts()
	p.playCtx, p.playCancel = context.WithCancel(context.Background())
	return func() tea.Msg {
		return frameTickMsg{}
	}
}

func (p *Player) isPlaying() bool {
	return p.playCtx.Err() == nil
}

func (p *Player) clearTimeouts() {
	if p.playCancel != nil {
		p.playCancel()
	}
}

func (p *Player) Close() {
	p.log = p.log.With("duration", time.Since(p.start).Truncate(100*time.Millisecond))
	if p.frame >= len(p.movie.Frames)-1 {
		p.log.Info("Finished movie")
	} else {
		p.log.Info("Disconnected early")
	}
	p.clearTimeouts()
	p.zone.Close()
}

func (p *SimplePlayer) Play(ctx context.Context) error {
	for p.frame < len(p.movie.Frames) {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		frame := p.movie.Frames[p.frame]
		_, err := fmt.Fprint(p.output, "\033[2J\033[999;1H") // Clear screen and move cursor to top
		if err != nil {
			return err
		}
		_, err = fmt.Fprint(p.output, frame.Data)
		if err != nil {
			return err
		}

		duration := frame.CalcDuration(p.speed)
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(duration):
		}

		p.frame++
	}

	p.log = p.log.With("duration", time.Since(p.start).Truncate(100*time.Millisecond))
	p.log.Info("Finished movie")
	return nil
}
