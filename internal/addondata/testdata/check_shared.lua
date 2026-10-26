-- Loads a Data.lua the companion wrote with shared lists and profiles, with nothing else in reach, and
-- checks the table the addon merges from. Run: luajit check_shared.lua <Data.lua>
local env = {}
local chunk = assert(loadfile(arg[1]))
setfenv(chunk, env)
chunk()
local names = 0
for _ in pairs(env) do names = names + 1 end
assert(names == 1, "Data.lua defines only GoldForever_Prices")
local p = assert(env.GoldForever_Prices, "GoldForever_Prices is set")
assert(p.v == 1 and p.at == 1761148800, "v and at")
local s = assert(p.shared, "the shared table")
assert(s.at == 1761200000, "the time the account's set last changed")
assert(#s.lists == 2 and #s.profiles == 1, "two lists and a profile")
local a, b, c = s.lists[1], s.lists[2], s.profiles[1]
assert(a.id == "L-a1_b2" and a.edited == 1761190000 and a.string == "GFL1:Herbs 2449 20", "the first list")
assert(b.id == "L2" and b.edited == 1761190001 and b.string == 'GFL1:Ore "best" \\ 2770 40', "a list with a quote and a backslash")
assert(c.id == "p9" and c.edited == 1761190002 and c.string == "GFP1:c2VsbCAyNTky", "the profile")
assert(p.dings.characters["Player-4620-006C0FC1"] == true, "the dings beside it")
assert(p.markets["forever-BETA-normal-horde"][2589][1] == 1234, "the prices beside it")
print("ok")
