-- Loads a Data.lua the companion wrote with a dings state, with nothing else in reach, and checks the
-- table the addon reads beside `at`. Run: luajit check_dings.lua <Data.lua>
local env = {}
local chunk = assert(loadfile(arg[1]))
setfenv(chunk, env)
chunk()
local names = 0
for _ in pairs(env) do names = names + 1 end
assert(names == 1, "Data.lua defines only GoldForever_Prices")
local p = assert(env.GoldForever_Prices, "GoldForever_Prices is set")
assert(p.v == 1 and p.at == 1761148800, "v and at")
local d = assert(p.dings, "the dings table")
assert(d.at == 1761150000, "the time the state last changed")
assert(d.webhook == true, "webhook")
assert(d.characters["Player-4620-006C0FC1"] == true, "a character on")
assert(d.characters["Player-4620-006C0FC2"] == false, "a character off")
local n = 0
for _ in pairs(d.characters) do n = n + 1 end
assert(n == 2, "two characters")
assert(p.markets["forever-BETA-normal-horde"][2589][1] == 1234, "the prices beside it")
print("ok")
