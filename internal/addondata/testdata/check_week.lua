-- Loads a Data.lua the companion wrote from testdata/week.json, with nothing else in reach, and checks
-- an item with a week mean carries it fifth and one without keeps four numbers.
-- Run: luajit check_week.lua <Data.lua>
local env = {}
local chunk = assert(loadfile(arg[1]))
setfenv(chunk, env)
chunk()
local p = assert(env.GoldForever_Prices, "GoldForever_Prices is set")
local m = assert(p.markets["retail-EU-Draenor"], "the market")
local function same(row, want)
  assert(#row == #want, #want .. " numbers, not " .. #row)
  for i = 1, #want do assert(row[i] == want[i], "value " .. i .. ": " .. tostring(row[i])) end
end
same(m[2589], { 1234, 1100, 1, 1761140000, 1250 })
same(m[14047], { 0, 0, 3, 1761100000 })
same(m[2592], { 9007199254740991, 5, 12, 1761148799, 9007199254740991 })
same(m[2996], { 40, 38, 2, 1761148000 })
assert(m[2589][5] - m[2589][1] == 16, "the week mean is a number")
print("ok")
