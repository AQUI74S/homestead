package classify

// merchantRule maps merchant names (substrings, lowercase, umlauts as ae/oe/ue)
// to a category. Specific entries come before general ones; the first match wins.
type merchantRule struct {
	patterns []string
	slug     string
	abo      bool // typically billed as a subscription
}

var merchants = []merchantRule{
	// Broadcasting fee, taxes, authorities
	{[]string{"beitragsservice", "rundfunk", "ard zdf"}, "rundfunk", false},
	{[]string{"finanzamt", "bundeskasse", "hauptzollamt", "stadtkasse", "kreiskasse", "gemeindekasse", "gemeinde ", "zulassungsstelle"}, "steuern", false},
	{[]string{"kita", "kindergarten", "kindertagesst", "schule", "hort ", "musikschule"}, "kita-schule", false},

	// Mobile & internet (specific before discounters: "aldi talk" before "aldi")
	{[]string{"aldi talk", "lidl connect", "congstar", "klarmobil", "fraenk", "winsim", "drillisch", "freenet", "blau mobilfunk", "o2", "telefonica", "vodafone gmbh mobil", "ay yildiz", "lebara", "lycamobile", "simyo", "otelo"}, "mobilfunk", false},
	{[]string{"telekom", "1&1", "1und1", "vodafone", "unitymedia", "pyur", "netcologne", "m net", "deutsche glasfaser", "ewe tel", "htp", "wilhelm tel", "glasfaser"}, "internet", false},

	// Energy & water
	{[]string{"wasserversorgung", "wasserverband", "zweckverband wasser", "abwasser", "wasserbeschaffung"}, "wasser", false},
	{[]string{"gasag", "erdgas", "gasversorgung", "fluessiggas", "tyczka", "primagas", "heizoel"}, "gas", false},
	{[]string{"stadtwerke", "e on", "eon energie", "enbw energie", "vattenfall", "rwe", "eprimo", "yello", "lichtblick", "tibber", "ostrom", "octopus energy", "naturstrom", "suewag", "mainova", "eswe versorgung", "entega", "ovag", "syna", "energieversorgung", "energie gmbh", "stromio", "extraenergie", "polarstern", "grünwelt", "gruenwelt"}, "strom", false},

	// Insurance
	{[]string{"versicherung", "allianz", "huk coburg", "huk24", "ergo versicherung", "ergo direkt", "axa", "generali", "r v ", "r+v", "debeka", "signal iduna", "devk", "gothaer", "zurich", "lvm", "provinzial", "wgv", "cosmosdirekt", "cosmos direkt", "hannoversche", "barmenia", "hdi", "getsafe", "wefox", "alte leipziger", "continentale", "nuernberger", "volkswohl", "ruv", "vhv", "arag", "roland rechtsschutz", "adcuri", "check24 versich"}, "versicherung", false},

	// Streaming, software, media -> subscriptions
	{[]string{"netflix", "spotify", "disney", "dazn", "sky deutschland", "wow tv", "rtl interactive", "rtl+", "joyn", "paramount", "apple com bill", "apple services", "itunes", "youtube", "google one", "audible", "prime video", "amazon prime", "prime mitglied", "primevideo", "amazon digital", "kindle", "deezer", "tidal", "microsoft", "msft", "adobe", "dropbox", "icloud", "openai", "chatgpt", "anthropic", "claude ai", "1password", "bitwarden", "nordvpn", "surfshark", "proton", "github", "notion", "canva", "storytel", "bookbeat", "readly", "zeit online", "spiegel", "faz ", "sueddeutsche", "bild plus", "xbox", "playstation", "sony interactive", "nintendo", "ea play", "twitch", "patreon", "onlyfans", "crunchyroll", "magentatv", "waipu", "zattoo", "hbo", "wondery", "blinkist", "duolingo", "babbel", "strava", "komoot", "tinder", "parship", "elitepartner", "home assistant cloud", "nabu casa", "plex"}, "abos", true},

	// Fitness & memberships (behave like subscriptions)
	{[]string{"mcfit", "fitx", "clever fit", "urban sports", "john reed", "fitness", "injoy", "kieser", "adac", "verein", "mitgliedsbeitrag", "gewerkschaft", "bahncard", "deutschlandticket", "d ticket"}, "mitgliedschaft", true},

	// Savings & loans
	{[]string{"trade republic", "scalable", "smartbroker", "traders place", "justtrade", "finanzen net zero", "raisin", "weltsparen", "bausparkasse", "schwaebisch hall", "lbs ", "wuestenrot", "bhw", "union investment", "deka", "fondsdepot", "flatex", "onvista", "consorsbank depot", "sparplan"}, "sparen", false},
	{[]string{"kreditkartenabrechnung", "kreditkarte", "american express", "amex", "barclays", "hanseatic bank", "advanzia", "tf bank", "awa7"}, "kreditkarte", false},
	{[]string{"santander consumer", "bank11", "volkswagen bank", "vw financial", "bmw bank", "mercedes benz bank", "toyota kreditbank", "ford bank", "renault bank", "targobank", "creditplus", "consors finanz", "auxmoney", "smava", "ratenkredit", "darlehen", "paypal ratenzahlung", "klarna ratenkauf", "openbank", "tesla financ", "tesla finanz", "santander"}, "kredite", false},

	// Groceries
	{[]string{"rewe", "edeka", "lidl", "aldi", "kaufland", "penny", "netto", "norma ", "tegut", "globus", "hit markt", "alnatura", "denns", "bio company", "metro", "picnic", "flink", "getir", "marktkauf", "nahkauf", "wasgau", "combi", "famila", "real ", "hofladen", "metzgerei", "fleischerei"}, "lebensmittel", false},

	// Drugstore & household
	{[]string{"dm drogerie", "dm fil", "rossmann", "mueller drogerie", "mueller handels", "mueller gmbh", "budni", "action", "tedi", "woolworth", "kik ", "nanu nana", "idee creativ"}, "drogerie", false},

	// Eating out & delivery
	{[]string{"lieferando", "wolt", "uber eats", "mcdonald", "burger king", "kfc", "subway", "starbucks", "vapiano", "backwerk", "baeckerei", "backhaus", "restaurant", "pizzeria", "pizza", "doener", "kebap", "kebab", "sushi", "cafe", "bistro", "imbiss", "gaststaette", "brauhaus", "ristorante", "trattoria", "l osteria", "block house", "hans im glueck", "five guys", "nordsee"}, "essen-gehen", false},

	// Fuel & EV charging
	{[]string{"aral", "shell", "esso", "jet tankst", "totalenergies", "total tank", "avia", "star tankst", "orlen", "agip", "eni ", "raiffeisen tank", "tankstelle", "ionity", "enbw mobility", "tesla supercharger", "maingau", "ewe go", "plugsurfing", "shell recharge", "allego", "fastned"}, "tanken", false},

	// Mobility
	{[]string{"deutsche bahn", "db vertrieb", "db fernverkehr", "db regio", "rmv", "rhein main verkehrsverbund", "eswe verkehr", "flixbus", "flixtrain", "uber", "bolt", "free now", "sixt", "share now", "miles mobility", "parkhaus", "apcoa", "easypark", "parkster", "parkopedia", "contipark", "tier mobility", "lime", "atu", "pitstop", "tuev", "dekra", "gtue", "autowerkstatt", "reifen", "kfz werkstatt", "lufthansa", "eurowings", "ryanair", "condor"}, "mobilitaet", false},

	// Clothing (before online shopping so Zalando doesn't land in "Online")
	{[]string{"zalando", "h&m", "h m ", "hm hennes", "hennes", "c&a", "c a mode", "primark", "about you", "deichmann", "snipes", "tk maxx", "zara", "peek cloppenburg", "s oliver", "esprit", "jack jones", "vinted", "only store", "new yorker", "bonprix", "tchibo"}, "kleidung", false},

	// Home & garden
	{[]string{"obi", "hornbach", "bauhaus", "toom", "hagebau", "globus baumarkt", "ikea", "dehner", "pflanzen koelle", "jysk", "poco", "roller", "xxxlutz", "hoeffner", "depot", "segmueller", "moemax", "bauking", "raiffeisen markt", "hellweg", "baumarkt", "gartencenter", "sonnenfreunde", "abfallw", "wertstoffhof", "recyclinghof", "entsorgung", "deponie", "kompostier"}, "haus-garten", false},

	// Health
	{[]string{"apotheke", "docmorris", "shop apotheke", "medpex", "zahnarzt", "arztpraxis", "praxis dr", "dr med", "physio", "fielmann", "apollo optik", "krankenhaus", "klinikum", "labor ", "sanitaetshaus", "optiker"}, "gesundheit", false},

	// Children
	{[]string{"mytoys", "smyths", "toys", "babymarkt", "baby walz", "rofu", "spielwaren", "jako o", "kinderladen"}, "kinder", false},

	// Leisure & hobbies
	{[]string{"cinemaxx", "cineplex", "uci kino", "kino", "eventim", "ticketmaster", "steam", "thalia", "hugendubel", "decathlon", "angelsport", "angel ", "askari", "fishermans partner", "fressnapf", "zooplus", "das futterhaus", "schwimmbad", "therme", "freizeitpark", "zoo ", "museum", "prusa", "3djake", "bambu", "conrad", "reichelt", "berrybase", "pollin", "caseking", "mindfactory"}, "freizeit", false},

	// General online shopping
	{[]string{"amazon", "amzn", "ebay", "otto gmbh", "otto versand", "otto de", "temu", "shein", "aliexpress", "mediamarkt", "media markt", "saturn", "cyberport", "alternate", "notebooksbilliger", "galaxus", "etsy", "kaufland de", "check24", "idealo", "apple store", "apple online"}, "online-shopping", false},

	// Gifts & donations
	{[]string{"spende", "unicef", "rotes kreuz", "drk", "aerzte ohne grenzen", "caritas", "diakonie", "greenpeace", "wwf", "betterplace", "gofundme", "blumen", "fleurop", "douglas", "yves rocher"}, "geschenke", false},
}

// Keywords in the remittance text (lower priority than the merchant name).
type keywordRule struct {
	words []string
	slug  string
}

var debitKeywords = []keywordRule{
	{[]string{"leistungen per", "leistungen zum", "darlehensleistung", "baufinanzierung", "immobilienfinanzierung", "hausfinanzierung", "wohnbaufinanzierung", "baudarlehen", "immobiliendarlehen", "annuitaetendarlehen", "hypothek", "grundschuld", "kaltmiete", "warmmiete", "miete ", "miete", "nebenkosten", "hausgeld", "hausverwaltung", "wohnungsbau", "wohnbau", "baugenossenschaft"}, "wohnen"},
	{[]string{"tilgung", "darlehen", "kreditrate", "ratenzahlung", "finanzierung", "annuitaet"}, "kredite"},
	{[]string{"sparplan", "depot", "tagesgeld", "festgeld", "bausparvertrag", "vermoegenswirksam", "vwl", "etf"}, "sparen"},
	cashKeywords,
	{[]string{"rundfunkbeitrag"}, "rundfunk"},
	{[]string{"versicherung", "beitrag kfz", "police"}, "versicherung"},
	{[]string{"kfz steuer", "kfzsteuer", "grundsteuer", "hundesteuer", "muellgebuehr", "abfallgebuehr", "steuer"}, "steuern"},
	{[]string{"abschlag strom", "stromabschlag", "strom"}, "strom"},
	{[]string{"gasabschlag", "abschlag gas", "erdgas"}, "gas"},
	{[]string{"wasser", "abwasser"}, "wasser"},
	{[]string{"mitgliedsbeitrag", "vereinsbeitrag", "jahresbeitrag"}, "mitgliedschaft"},
	{[]string{"abonnement", "abo ", "subscription", "monatsbeitrag"}, "abos"},
	{[]string{"kita", "kindergarten", "essensgeld", "schulgeld", "betreuung"}, "kita-schule"},
	{[]string{"kreditkarte", "kreditkartenabrechnung"}, "kreditkarte"},
}

var creditKeywords = []keywordRule{
	{[]string{"mieteinnahme", "mietzahlung", "mietkonto", "miete ", "kaltmiete", "untermiete"}, "vermietung"},
	{[]string{"kindergeld", "familienkasse"}, "kindergeld"},
	{[]string{"gehalt", "lohn", "bezuege", "entgelt", "besoldung", "verguetung", "salary", "payroll", "rente ", "pension", "elterngeld", "krankengeld", "arbeitslosengeld"}, "gehalt"},
	{[]string{"erstattung", "rueckerstattung", "gutschrift", "refund", "storno", "retoure", "rueckzahlung", "rueckbuchung", "cashback"}, "erstattung"},
}

var cashKeywords = keywordRule{[]string{"bargeldauszahlung", "geldautomat", "auszahlung gaa", " gaa ", "bargeld", "cash"}, "bargeld"}

// Energy suppliers often deliver several utilities; the remittance text decides.
var utilityKeywords = []keywordRule{
	{[]string{"erdgas", "gasabschlag", "gas abschlag", "gas ", "gasliefer", "fernwaerme", "waerme", "heizstrom", "heizoel"}, "gas"},
	{[]string{"wasser", "abwasser"}, "wasser"},
	{[]string{"strom", "oekostrom", "autostrom", "wallbox"}, "strom"},
}
