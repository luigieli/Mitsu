local socket = require("socket")
-- Bind to localhost on port 8888
local server = assert(socket.bind("*", 8888))
local ip, port = server:getsockname()

console:log("Mitsu Pokémon Bridge: Waiting for connection on port " .. port)

local client = nil

-- Memory Addresses for Pokémon FireRed (US)
local ADDR_MY_HP = 0x0202402C
local ADDR_ENEMY_HP = 0x0202406C
local ADDR_PLAYER_DATA_PTR = 0x03005008

function get_battle_state()
    local my_hp = memory.readword(ADDR_MY_HP)
    local enemy_hp = memory.readword(ADDR_ENEMY_HP)
    return string.format("BATTLE|MyHP:%d|EnemyHP:%d", my_hp, enemy_hp)
end

function get_position()
    local player_base = memory.readword(ADDR_PLAYER_DATA_PTR)
    if player_base == 0 then return "POS:0,0|MAP:0.0" end
    
    local x = memory.readword(player_base + 0x0)
    local y = memory.readword(player_base + 0x2)
    local map_bank = memory.readbyte(player_base + 0x4)
    local map_id   = memory.readbyte(player_base + 0x5)
    
    return string.format("POS:%d,%d|MAP:%d.%d", x, y, map_bank, map_id)
end

-- Main Loop
while true do
    if not client then
        server:settimeout(0.01)
        client = server:accept()
        if client then
            console:log("Go Companion Connected!")
            client:settimeout(0)
        end
    else
        -- 1. Send State to Go
        local state = get_battle_state() .. "|" .. get_position()
        local success, err = client:send(state .. "
")
        if not success then
            console:log("Connection lost: " .. err)
            client = nil
        end
        
        -- 2. Receive Commands from Go
        if client then
            local line, err = client:receive()
            if line then
                console:log("Received command: " .. line)
                
                -- Button Mapping
                local buttons = {}
                if line == "A" then buttons = {A=true}
                elseif line == "B" then buttons = {B=true}
                elseif line == "UP" then buttons = {up=true}
                elseif line == "DOWN" then buttons = {down=true}
                elseif line == "LEFT" then buttons = {left=true}
                elseif line == "RIGHT" then buttons = {right=true}
                elseif line == "START" then buttons = {start=true}
                elseif line == "SELECT" then buttons = {select=true}
                end
                
                if next(buttons) ~= nil then
                    joypad.set(1, buttons)
                    emu.frameadvance()
                    joypad.set(1, {A=false, B=false, up=false, down=false, left=false, right=false, start=false, select=false})
                end
            end
        end
    end
    
    emu.frameadvance()
end
