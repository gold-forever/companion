-- Loads a Data.lua the companion wrote with the account's characters, with nothing else in reach, and
-- checks the table the addon matches mail recipients against. Run: luajit check_characters.lua <Data.lua>
local env = {}
local chunk = assert(loadfile(arg[1]))
setfenv(chunk, env)
chunk()
local names = 0
for _ in pairs(env) do names = names + 1 end
assert(names == 1, "Data.lua defines only GoldForever_Prices")
local p = assert(env.GoldForever_Prices, "GoldForever_Prices is set")
assert(p.v == 1 and p.at == 1761148800, "v and at")
local c = assert(p.characters, "the characters table")
assert(#c == 2, "the two Forever characters, not the retail one")
assert(c[1].realm == "Soulseeker" and c[1].name == "Goldsmith", "the first character")
assert(c[2].realm == 'Zul\'jin "EU"' and c[2].name == "\195\134rendil", "a realm with quotes, a name outside ASCII")
assert(p.markets["forever-BETA-normal-horde"][2589][1] == 1234, "the prices beside it")
print("ok")
