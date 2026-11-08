-- Loads a Data.lua the companion wrote from testdata/supply.json, with nothing else in reach, and checks
-- sold and listed sit under their keys beside the numbers, 0 is written, and an unsent figure is nil.
-- Run: luajit check_supply.lua <Data.lua>
local env = {}
local chunk = assert(loadfile(arg[1]))
setfenv(chunk, env)
chunk()
local p = assert(env.GoldForever_Prices, "GoldForever_Prices is set")
local m = assert(p.markets["retail-EU-Draenor"], "the market")
local function same(row, want, sold, listed)
  assert(#row == #want, #want .. " numbers, not " .. #row)
  for i = 1, #want do assert(row[i] == want[i], "value " .. i .. ": " .. tostring(row[i])) end
  assert(row.sold == sold, "sold: " .. tostring(row.sold))
  assert(row.listed == listed, "listed: " .. tostring(row.listed))
end
same(m[2589], { 1234, 1100, 1, 1761140000, 1250 }, 42, 5133)
assert(m[2589].units == 3, "units")
same(m[2592], { 50, 5, 12, 1761148799 }, nil, nil)
same(m[2996], { 40, 38, 2, 1761148000 }, nil, 7)
same(m[3356], { 60, 55, 4, 1761148000 }, 9007199254740991, nil)
same(m[14047], { 0, 0, 3, 1761100000 }, 0, 0)
print("ok")
