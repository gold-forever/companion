-- Loads a Data.lua the companion wrote from testdata/units.json, with nothing else in reach, and checks
-- a thin item carries its units under the key units, beside its four or five numbers, and others none.
-- Run: luajit check_units.lua <Data.lua>
local env = {}
local chunk = assert(loadfile(arg[1]))
setfenv(chunk, env)
chunk()
local p = assert(env.GoldForever_Prices, "GoldForever_Prices is set")
local m = assert(p.markets["retail-EU-Draenor"], "the market")
local function same(row, want, units)
  assert(#row == #want, #want .. " numbers, not " .. #row)
  for i = 1, #want do assert(row[i] == want[i], "value " .. i .. ": " .. tostring(row[i])) end
  assert(row.units == units, "units: " .. tostring(row.units))
end
same(m[2589], { 1234, 1100, 1, 1761140000, 1250 }, 3)
same(m[14047], { 0, 0, 3, 1761100000 }, 0)
same(m[2592], { 9007199254740991, 5, 12, 1761148799, 9007199254740991 }, nil)
same(m[2996], { 40, 38, 2, 1761148000 }, 5)
print("ok")
