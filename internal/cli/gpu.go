package cli

import (
	"fmt"
	"os/exec"
	"strconv"
	"strings"
)

// detectVRAM attempts to detect the total VRAM in MB using nvidia-smi.
// Returns 0 if no NVIDIA GPU is detected or the command fails.
func detectVRAM() int {
	out, err := exec.Command(
		"nvidia-smi",
		"--query-gpu=memory.total",
		"--format=csv,noheader,nounits",
	).Output()
	if err != nil {
		return 0
	}

	// nvidia-smi may report multiple GPUs; take the first line.
	line := strings.TrimSpace(strings.Split(strings.TrimSpace(string(out)), "\n")[0])
	mb, err := strconv.Atoi(line)
	if err != nil {
		return 0
	}
	return mb
}

// recommendedModel returns the best enrichment model name and a human-readable
// reason string based on the available VRAM (in MB).
func recommendedModel(vramMB int) (model string, reason string) {
	gb := vramMB / 1024
	switch {
	case gb >= 24:
		return "qwen2.5-coder:32b", fmt.Sprintf("fits %dGB VRAM", gb)
	case gb >= 12: // <=16GB bucket
		return "qwen2.5-coder:14b", fmt.Sprintf("fits %dGB VRAM", gb)
	case gb >= 6: // <=8GB bucket
		return "qwen2.5-coder:7b", fmt.Sprintf("fits %dGB VRAM", gb)
	default: // <=4GB
		return "qwen2.5-coder:3b", fmt.Sprintf("fits %dGB VRAM", gb)
	}
}
