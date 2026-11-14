-- Loads a Data.lua the companion wrote with the names wanted, with nothing else in reach, and checks the
-- list the addon asks its own client for. Run: luajit check_names.lua <Data.lua>
local env = {}
local chunk = assert(loadfile(arg[1]))
setfenv(chunk, env)
chunk()
local names = 0
for _ in pairs(env) do names = names + 1 end
assert(names == 1, "Data.lua defines only GoldForever_Prices")
local p = assert(env.GoldForever_Prices, "GoldForever_Prices is set")
assert(p.v == 1 and p.at == 1761148800, "v and at")
local w = assert(p.namesWanted, "the namesWanted table")
assert(w.locale == "frFR", "its locale")
assert(#w.items == 2 and w.items[1] == 2589 and w.items[2] == 2592, "the items, in the server's order")
assert(#w.recipes == 1 and w.recipes[1] == 2963, "the recipes")
assert(type(w.quests) == "table" and #w.quests == 0, "a kind with none is an empty table")
assert(#w.factions == 1 and w.factions[1] == 76, "the factions")
assert(#w.maps == 1 and w.maps[1] == 1411, "the maps")
assert(p.markets["forever-BETA-normal-horde"][2589][1] == 1234, "the prices beside it")
print("ok")
