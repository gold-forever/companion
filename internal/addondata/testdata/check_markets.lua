-- Loads a Data.lua the companion wrote for an account's markets, with nothing else in reach, and
-- checks it holds testdata/markets.json. Run: luajit check_markets.lua <Data.lua>
local env = {}
local chunk = assert(loadfile(arg[1]))
setfenv(chunk, env)
chunk()
local names = 0
for _ in pairs(env) do names = names + 1 end
assert(names == 1, "Data.lua defines only GoldForever_Prices")
local p = assert(env.GoldForever_Prices, "GoldForever_Prices is set")
assert(p.v == 1 and p.at == 1761148800, "v and at")
local function same(row, want)
  assert(#row == #want, "four numbers")
  for i = 1, #want do assert(row[i] == want[i], "value " .. i .. ": " .. tostring(row[i])) end
end
local horde = assert(p.markets["forever-BETA-normal-horde"], "the Horde market")
local alliance = assert(p.markets["forever-BETA-normal-alliance"], "the Alliance market")
same(horde[2589], { 1234, 1100, 1, 1761140000 })
same(horde[2592], { 9007199254740991, 5, 12, 1761148799 })
same(alliance[2589], { 1500, 1400, 4, 1761140001 })
same(alliance[14047], { 0, 0, 3, 1761100000 })
local markets = 0
for _ in pairs(p.markets) do markets = markets + 1 end
assert(markets == 2, "two markets")
print("ok")
