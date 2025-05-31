package play

import (
	"log/slog"

	"gabe565.com/ascii-movie/internal/config"
	"gabe565.com/ascii-movie/internal/movie"
	"gabe565.com/ascii-movie/internal/player"
	"gabe565.com/utils/cobrax"
	"gabe565.com/utils/slogx"
	"github.com/spf13/cobra"
)

func NewCommand(conf *config.Config, opts ...cobrax.Option) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "play [movie]",
		Short: "Play an ASCII movie locally.",
		Args:  cobra.MaximumNArgs(1),
		RunE:  run,

		ValidArgsFunction: movie.CompleteMovieName,
	}

	conf.RegisterPlayFlags(cmd)

	for _, opt := range opts {
		opt(cmd)
	}

	return cmd
}

func run(cmd *cobra.Command, args []string) error {
	conf, err := config.Load(cmd)
	if err != nil {
		return err
	}

	if !cmd.Flags().Changed(config.FlagLogLevel) {
		conf.LogLevel = slogx.LevelWarn
		conf.InitLog(cmd.ErrOrStderr())
	}

	var path string
	if len(args) > 0 {
		path = args[0]
	}

	m, err := movie.Load(path, conf.Speed)
	if err != nil {
		return err
	}

	p := player.NewSimplePlayer(&m, slog.Default(), cmd.OutOrStdout())
	return p.Play(cmd.Context())
}
