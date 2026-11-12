-- Loads a Data.lua the companion wrote from testdata/day.json, with nothing else in reach, and checks
-- volatile and dayPrice sit under their keys beside the numbers, which an older addon reads unchanged.
-- Run: luajit check_day.lua <Data.lua>
local env = {}
local chunk = assert(loadfile(arg[1]))
setfenv(chunk, env)
chunk()
local p = assert(env.GoldForever_Prices, "GoldForever_Prices is set")
local m = assert(p.markets["retail-EU-Draenor"], "the market")
local function same(row, want, volatile, dayPrice)
  assert(#row == #want, #want .. " numbers, not " .. #row)
  for i = 1, #want do assert(row[i] == want[i], "value " .. i .. ": " .. tostring(row[i])) end
  assert(row.volatile == volatile, "volatile: " .. tostring(row.volatile))
  assert(row.dayPrice == dayPrice, "dayPrice: " .. tostring(row.dayPrice))
end
same(m[2589], { 1234, 1100, 1, 1761140000, 1250 }, true, 20000)
assert(m[2589].units == 3 and m[2589].sold == 42 and m[2589].listed == 5133, "units, sold and listed")
same(m[2592], { 50, 5, 12, 1761148799 }, nil, nil)
same(m[2996], { 40, 38, 2, 1761148000 }, nil, 9007199254740991)
same(m[3356], { 60, 55, 4, 1761148000 }, true, nil)
same(m[14047], { 0, 0, 3, 1761100000 }, nil, 0)
print("ok")
