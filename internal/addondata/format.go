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
//	  dings = {                   -- optional: the site's side of the dings switch
//	    at = <int>,               -- epoch seconds: when it last changed
//	    webhook = <bool>,         -- whether a level-up webhook is connected
//	    characters = { [<guid>] = <bool>, ... },
//	  },
//	  markets = {
//	    [<market key>] = {
//	      [<item id>] = { value, minBuyout, players, scannedAt [, week] },  -- integers; week optional
//	    },
//	  },
//	}
//
// Keys and strings are quoted printable ASCII; every number is an integer Lua holds exactly.

// formatVersion is Data.lua's v. It changes when an addon reading the file as it is would misread it:
// a field removed, renamed or given another type, or the item figures reordered. A new optional field
// does not: shared, dings and week were each added at v = 1.
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
	fieldVersion    = "v"
	fieldAt         = "at"
	fieldShared     = "shared"
	fieldLists      = "lists"
	fieldProfiles   = "profiles"
	fieldID         = "id"
	fieldEdited     = "edited"
	fieldString     = "string"
	fieldDings      = "dings"
	fieldWebhook    = "webhook"
	fieldCharacters = "characters"
	fieldMarkets    = "markets"
)
