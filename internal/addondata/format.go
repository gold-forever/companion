package addondata

// The data file is the contract with the addon. In each flavour folder the companion writes
// Interface/AddOns/GoldForever_Data/ with two files: GoldForever_Data.toc, which loads Data.lua, and
// Data.lua, which sets one global and nothing else:
//
//	GoldForever_Prices = {
//	  v = 1,                      -- integer: formatVersion
//	  at = <int>,                 -- epoch seconds: when the server built the prices
//	  shared = {                  -- optional: the account's shared lists and sale profiles
//	    at = <int>,               -- epoch seconds: when the set last changed
//	    lists = { { id = <string>, edited = <int>, string = <string> }, ... },
//	    profiles = { { id = <string>, edited = <int>, string = <string> }, ... },
//	  },
//	  markets = {
//	    [<market key>] = {
//	      [<item id>] = { value, minBuyout, players, scannedAt [, week] [, units = <int>] },
//	    },
//	  },
//	}
//
// An item's numbers are integers: week is optional, and units, keyed so that it never takes week's
// place, is there only for a thin price.
//
// Keys and strings are quoted printable ASCII; every number is an integer Lua holds exactly.

// formatVersion is Data.lua's v. It changes when an addon reading the file as it is would misread it:
// a field removed, renamed or given another type, or the item figures reordered. A new optional field
// does not, nor does dropping one an addon reads as absent: shared, week and units were each added at
// v = 1, and dings was dropped at it.
const formatVersion = 1

// The addon's folder and its files.
const (
	addonName = "GoldForever_Data"
	tocFile   = addonName + ".toc"
	dataFile  = "Data.lua"
)

// dataGlobal is the one global Data.lua sets.
const dataGlobal = "GoldForever_Prices"

// The fields of Data.lua's tables, as the addon reads them.
const (
	fieldVersion  = "v"
	fieldAt       = "at"
	fieldShared   = "shared"
	fieldLists    = "lists"
	fieldProfiles = "profiles"
	fieldID       = "id"
	fieldEdited   = "edited"
	fieldString   = "string"
	fieldMarkets  = "markets"
	fieldUnits    = "units"
)
