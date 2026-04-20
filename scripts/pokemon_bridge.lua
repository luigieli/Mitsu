local socket = require("socket")
-- Bind to localhost on port 8888
local server = assert(socket.bind("*", 8888))
local ip, port = server:getsockname()

console:log("Mitsu Pokémon Bridge: Waiting for connection on port " .. port)

-- Memory Addresses for Pokémon FireRed (US)
local ADDR_MY_HP = 0x0202402C
local ADDR_ENEMY_HP = 0x0202406C
local ADDR_PLAYER_DATA_PTR = 0x03005008

function get_game_state()
    local my_hp = memory.readword(ADDR_MY_HP)
    local enemy_hp = memory.readword(ADDR_ENEMY_HP)
    
    local player_base = memory.readword(ADDR_PLAYER_DATA_PTR)
    local x, y, bank, map = 0, 0, 0, 0
    
    if player_base ~= 0 then
        x = memory.readword(player_base + 0x0)
        y = memory.readword(player_base + 0x2)
        bank = memory.readbyte(player_base + 0x4)
        map   = memory.readbyte(player_base + 0x5)
    end
    
    return string.format("BATTLE|MyHP:%d|EnemyHP:%d|POS:%d,%d|MAP:%d.%d", 
                         my_hp, enemy_hp, x, y, bank, map)
end

function handle_command(client, line)
    local buttons = {
        A = {A=true}, B = {B=true}, 
        UP = {up=true}, DOWN = {down=true}, 
        LEFT = {left=true}, RIGHT = {right=true},
        START = {start=true}, SELECT = {select=true}
    }

    local btn = buttons[line]
    if btn then
        joypad.set(1, btn)
        emu.frameadvance()
        joypad.set(1, {A=false, B=false, up=false, down=false, left=false, right=false, start=false, select=false})
    end
end

-- Main Loop
local client = nil
while true do
    if not client then
        server:settimeout(0)
        client = server:accept()
        if client then
            console:log("Go Companion Connected!")
            client:settimeout(0)
        end
    else
        -- 1. Pulse State
        local success, err = client:send(get_game_state() .. "\n")
        if not success then
            console:log("Connection lost: " .. err)
            client = nil
        end
        
        -- 2. Process Command
        if client then
            local line, err = client:receive()
            if line then handle_command(client, line) end
        end
    end
    
    emu.frameadvance()
end
