-- wtfrc E2E test neovim config
-- Loads the wtfrc coaching plugin if available.

local ok, _ = pcall(function()
    local plugin_path = vim.fn.expand("~/.config/nvim/wtfrc-coach.lua")
    if vim.fn.filereadable(plugin_path) == 1 then
        dofile(plugin_path)
    end
end)
if not ok then
    -- Plugin not available; proceed without it
end
