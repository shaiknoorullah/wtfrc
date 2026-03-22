-- wtfrc-coach.lua - Neovim coaching plugin
-- Tracks user keystrokes and mouse actions to send to the wtfrc coach daemon

local M = {}

-- State: file descriptor for the FIFO, nil until opened
local fifo_fd = nil
local last_event_time = 0
local throttle_ms = 200

-- Get the FIFO path from XDG_RUNTIME_DIR
local function get_fifo_path()
  local xdg_runtime_dir = os.getenv("XDG_RUNTIME_DIR")
  if not xdg_runtime_dir or xdg_runtime_dir == "" then
    xdg_runtime_dir = "/run/user/" .. os.getenv("UID")
  end
  return xdg_runtime_dir .. "/wtfrc/coach.fifo"
end

-- Lazy open the FIFO on first event
-- Returns true if fd is available (either was already open or successfully opened)
local function ensure_fifo_open()
  if fifo_fd then
    return true
  end

  local fifo_path = get_fifo_path()
  local ok, fd = pcall(vim.uv.fs_open, fifo_path, "w", 0o644)

  if not ok then
    -- Failed to open, will retry on next event
    return false
  end

  fifo_fd = fd
  return true
end

-- Write an event to the FIFO
-- If write fails, close fd and set to nil for lazy re-open on next event
local function write_event(source, action, context)
  if not ensure_fifo_open() then
    return
  end

  local message = source .. "\t" .. action .. "\t" .. context .. "\n"
  local ok, err = pcall(vim.uv.fs_write, fifo_fd, message)

  if not ok then
    -- Write failed, close fd and reset for lazy re-open
    if fifo_fd then
      pcall(vim.uv.fs_close, fifo_fd)
    end
    fifo_fd = nil
  end
end

-- Check if current mode is normal mode
local function is_normal_mode()
  return vim.api.nvim_get_mode().mode == "n"
end

-- Main key handler
local function on_key_handler(key)
  -- Check throttle
  local now = vim.uv.now()
  if now - last_event_time < throttle_ms then
    return
  end

  -- Only track in normal mode
  if not is_normal_mode() then
    return
  end

  local keytrans = vim.fn.keytrans(key)

  -- Check for arrow keys: <Up>, <Down>, <Left>, <Right>
  if keytrans:match("^<[UDLR]") then
    last_event_time = now
    write_event("nvim", "arrow-key", "normal-mode")
    return
  end

  -- Check for mouse scroll: <ScrollWheel(Up|Down|Left|Right)>
  if keytrans:match("^<ScrollWheel") then
    last_event_time = now
    write_event("nvim", "mouse-scroll", "normal-mode")
    return
  end
end

-- Initialize the plugin
local function init()
  -- Register the key handler with namespace "wtfrc"
  vim.on_key(on_key_handler, vim.api.nvim_create_namespace("wtfrc"))

  -- Register cleanup on VimLeavePre
  local group = vim.api.nvim_create_augroup("wtfrc_coach", { clear = true })
  vim.api.nvim_create_autocmd("VimLeavePre", {
    group = group,
    callback = function()
      if fifo_fd then
        pcall(vim.uv.fs_close, fifo_fd)
        fifo_fd = nil
      end
    end,
  })
end

-- Initialize on load
init()

return M
