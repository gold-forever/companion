-- Loads a Data.lua the companion wrote, with nothing else in reach, and checks it holds
-- testdata/answer.json. Run: luajit check.lua <Data.lua>
local env = {}
local chunk = assert(loadfile(arg[1]))
setfenv(chunk, env)
chunk()
local names = 0
for _ in pairs(env) do names = names + 1 end
assert(names == 1, "Data.lua defines only GoldForever_Prices")
local p = assert(env.GoldForever_Prices, "GoldForever_Prices is set")
assert(p.v == 1 and p.at == 1761148800, "v and at")
local m = assert(p.markets["forever-BETA-normal-horde"], "the market")
local function same(row, want)
  assert(#row == #want, "four numbers")
  for i = 1, #want do assert(row[i] == want[i], "value " .. i .. ": " .. tostring(row[i])) end
end
same(m[2589], { 1234, 1100, 1, 1761140000 })
same(m[14047], { 0, 0, 3, 1761100000 })
same(m[2592], { 9007199254740991, 5, 12, 1761148799 })
local items = 0
for _ in pairs(m) do items = items + 1 end
assert(items == 3, "three items")
print("ok")
