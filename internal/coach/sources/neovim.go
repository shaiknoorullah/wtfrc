package sources

import (
	"context"
	"os"

	"github.com/charmbracelet/log"
	"github.com/shaiknoorullah/wtfrc/internal/coach"
)

// RunNeovimOptional is a stub that discovers Neovim instances via the $NVIM
// environment variable. Full RPC subscription is deferred — the Neovim Lua
// plugin (Task 10) already sends events via the FIFO.
//
// If $NVIM is set, the detected socket path is logged. The function returns
// immediately in all cases (stub behaviour).
func RunNeovimOptional(_ context.Context, _ chan<- coach.Event) {
	nvim := os.Getenv("NVIM")
	if nvim == "" {
		log.Debug("neovim: $NVIM not set, source disabled (events arrive via FIFO)")
		return
	}
	log.Infof("neovim: instance detected at %s (full RPC subscription deferred)", nvim)
}
