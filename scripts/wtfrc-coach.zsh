#!/bin/zsh
# wtfrc-coach.zsh - Shell coaching hooks for wtfrc
# Provides real-time feedback on shell commands and practices

# Standard coaching mode: writes commands to FIFO for async processing
_wtfrc_coach_preexec() {
    local fifo="${XDG_RUNTIME_DIR}/wtfrc/coach.fifo"
    if [[ -p "$fifo" ]]; then
        {
            print "shell	$1" > "$fifo"
        } &!
    fi
}

# Display coaching messages if available
_wtfrc_coach_precmd() {
    local msg_file="${XDG_RUNTIME_DIR}/wtfrc/coach-msg"
    if [[ -f "$msg_file" ]]; then
        cat "$msg_file"
        rm -f "$msg_file"
    fi
}

# Strict mode: synchronously checks commands against policies
# Enabled by setting WTFRC_STRICT=1
_wtfrc_strict_preexec() {
    local sock="${XDG_RUNTIME_DIR}/wtfrc/coach-strict.sock"

    if [[ ! -S "$sock" ]]; then
        # Socket missing or not a socket - fail-open (allow command)
        return 0
    fi

    local response
    response=$(timeout 0.1s socat - UNIX-CONNECT:"$sock" <<< "check	$1" 2>/dev/null)

    if [[ -z "$response" ]] || [[ "$response" == "timeout" ]]; then
        # No response or timeout - fail-open (allow command)
        return 0
    fi

    if [[ "$response" == reject:* ]]; then
        # Rejection message format: reject:<reason>
        local msg="${response#reject:}"
        print -P "%F{red}${msg}%f"
        _wtfrc_rejected=1
        return 1
    fi

    return 0
}

# Register hooks with zsh
autoload -Uz add-zsh-hook

# Standard coaching hooks - always enabled
add-zsh-hook preexec _wtfrc_coach_preexec
add-zsh-hook precmd _wtfrc_coach_precmd

# Strict mode hooks - enabled by setting WTFRC_STRICT=1
if [[ "${WTFRC_STRICT:-0}" == "1" ]]; then
    add-zsh-hook preexec _wtfrc_strict_preexec
fi
