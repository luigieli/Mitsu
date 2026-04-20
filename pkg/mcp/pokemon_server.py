import socket
import time
import os
import re
import json
from fastmcp import FastMCP

# Create an MCP server
mcp = FastMCP("Pokemon")

BRIDGE_ADDR = ("127.0.0.1", 8888)
ROADMAP_FILE = os.path.join(os.path.dirname(__file__), "roadmap.json")

def load_roadmap():
    if not os.path.exists(ROADMAP_FILE):
        return []
    with open(ROADMAP_FILE, "r") as f:
        return json.load(f)

def save_roadmap(data):
    with open(ROADMAP_FILE, "w") as f:
        json.dump(data, f, indent=2)

def send_bridge_cmd(cmd: str) -> str:
    """Sends a command to the Lua bridge and returns the response."""
    try:
        with socket.socket(socket.AF_INET, socket.SOCK_STREAM) as s:
            s.settimeout(2.0)
            s.connect(BRIDGE_ADDR)
            s.sendall(f"{cmd}\n".encode())
            data = s.recv(1024).decode()
            return data.strip()
    except Exception as e:
        return f"ERROR: {str(e)}"

def parse_state(state_str: str):
    """Parses the Lua bridge state string into a dict."""
    parts = state_str.split("|")
    data = {}
    for p in parts:
        if ":" in p:
            k, v = p.split(":", 1)
            data[k] = v
    return data

def verify_roadmap_from_memory(roadmap, state):
    """Automatically updates roadmap completion based on bridge memory data."""
    changed = False
    
    # Check Badges (Bitfield)
    try:
        badge_bits = int(state.get("BADGES", "0"))
        # Map badge index to goal_id
        badge_map = {
            0: "badge_1", 1: "badge_2", 2: "badge_3", 3: "badge_4",
            4: "badge_5", 5: "badge_6", 6: "badge_7", 7: "badge_8"
        }
        for bit, goal_id in badge_map.items():
            if (badge_bits >> bit) & 1:
                for item in roadmap:
                    if item['id'] == goal_id and item['status'] == "PENDING":
                        item['status'] = "COMPLETED"
                        changed = True
    except:
        pass

    # Check Map IDs for location-based goals (Examples)
    map_str = state.get("MAP", "0.0")
    if map_str == "3.0" and state.get("POS") != "0,0": # Pallet Town
        # This could be more granular, but for demo:
        pass

    return changed

@mcp.tool()
def get_game_state() -> str:
    """Returns the current player position, map ID, and game status (FREE, DIALOG, or BATTLE)."""
    return send_bridge_cmd("GET_STATE")

@mcp.tool()
def get_roadmap() -> str:
    """
    Returns the current list of game achievements and their status.
    This list is auto-verified against game memory.
    """
    state_raw = send_bridge_cmd("GET_STATE")
    state = parse_state(state_raw)
    roadmap = load_roadmap()
    
    if verify_roadmap_from_memory(roadmap, state):
        save_roadmap(roadmap)
        
    output = "POKEMON FIRERED ROADMAP (VERIFIED):\n"
    for item in roadmap:
        status_marker = "✓" if item['status'] == "COMPLETED" else "✗"
        output += f"{status_marker} {item['name']}: {item['description']}\n"
        
    return output

@mcp.tool()
def move_to(target_x: int, target_y: int) -> str:
    """
    Moves the player toward the target coordinates using simple directional steps.
    This tool performs multiple steps until the target is reached or an obstacle is hit.
    """
    # Fail-Fast: Out of bounds check
    if not (0 <= target_x <= 255 and 0 <= target_y <= 255):
        return f"Error: Target ({target_x}, {target_y}) is out of bounds."

    state_raw = send_bridge_cmd("GET_STATE")
    if "ERROR" in state_raw: return state_raw
    
    state = parse_state(state_raw)
    # Fail-Fast: Game state check
    if state.get("STATUS") != "FREE":
        return f"Cannot move: Game is in {state.get('STATUS')} state."

    start_pos = state.get("POS", "0,0").split(",")
    cur_x, cur_y = int(start_pos[0]), int(start_pos[1])

    path_log = []
    max_steps = 15 # Safety limit per tool call
    
    for _ in range(max_steps):
        if cur_x == target_x and cur_y == target_y:
            break
        
        dx = target_x - cur_x
        dy = target_y - cur_y
        
        cmd = None
        if abs(dx) > abs(dy):
            cmd = "RIGHT" if dx > 0 else "LEFT"
        else:
            cmd = "DOWN" if dy > 0 else "UP"
            
        res = send_bridge_cmd(cmd)
        if res != "OK":
            return f"Movement failed at {cur_x},{cur_y}: {res}"
        
        time.sleep(0.1)
        
        new_state_raw = send_bridge_cmd("GET_STATE")
        new_state = parse_state(new_state_raw)
        new_pos = new_state.get("POS", "0,0").split(",")
        nx, ny = int(new_pos[0]), int(new_pos[1])
        
        # Fail-Fast: Obstacle detection
        if nx == cur_x and ny == cur_y:
            return f"BLOCKED at {cur_x},{cur_y}. Cannot move further toward {target_x},{target_y}."
        
        cur_x, cur_y = nx, ny
        path_log.append(f"({cur_x},{cur_y})")

    return f"Moved to {cur_x},{cur_y}. Path: {' -> '.join(path_log)}"

@mcp.tool()
def interact() -> str:
    """Presses the A button to interact with objects, NPCs, or menus."""
    return send_bridge_cmd("A")

@mcp.tool()
def spam_confirm(count: int = 5) -> str:
    """Spams the A button multiple times to skip through dialogue or intro screens."""
    for _ in range(count):
        send_bridge_cmd("A")
        time.sleep(0.2)
    return f"Pressed A {count} times."

@mcp.tool()
def run_away() -> str:
    """Attempts to flee from a wild Pokémon battle."""
    send_bridge_cmd("DOWN")
    time.sleep(0.1)
    send_bridge_cmd("RIGHT")
    time.sleep(0.1)
    send_bridge_cmd("A")
    return "Attempted to run."

if __name__ == "__main__":
    mcp.run()
