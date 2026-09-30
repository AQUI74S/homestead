package hv

// Cost types the code refers to. The full list with names is CostTypes.
const (
	CostRent        = "miete"          // rent incl. utility prepayment
	CostNKBackpay   = "nk_nachzahlung" // back payment after the utility settlement
	CostDeposit     = "kaution"
	CostOtherIncome = "einnahme_sonst" // income that could not be assigned
	CostOther       = "sonstiges"      // cost that could not be assigned
	CostTransfer    = "entnahme"       // money between own accounts
)

// CostKind groups cost types for the reports.
const (
	KindIncome  = "einnahme"
	KindCost    = "kosten"
	KindNeutral = "neutral" // deposits, transfers
)

// Allocation keys of the utility settlement (Umlageschlüssel).
const (
	KeyArea    = "flaeche"   // by living area
	KeyPersons = "personen"  // by number of occupants
	KeyUnits   = "einheiten" // equally per unit
)

// ValidKey reports whether k is one of the allocation keys.
func ValidKey(k string) bool { return k == KeyArea || k == KeyPersons || k == KeyUnits }

// Rent types of a lease.
const (
	RentFixed  = "fest"    // increases up to the local comparative rent (§558 BGB)
	RentStaged = "staffel" // agreed steps (Staffelmiete)
	RentIndex  = "index"   // tied to the consumer price index (Indexmiete)
)

// Lease defaults.
const (
	DefaultDueDay = 3  // rent due on the 3rd working day, simplified to the 3rd
	MaxDueDay     = 28 // due day must exist in every month
)

// Status of a month in the rent ledger.
const (
	StatusPaid    = "bezahlt"
	StatusPartial = "teilweise"
	StatusOpen    = "offen"
	StatusDue     = "faellig" // due date not reached yet
)

// Kinds of deadlines.
const (
	DeadlineSettlement = "nk"         // utility settlement
	DeadlineStep       = "staffel"    // next step of a staged rent
	DeadlineIncrease   = "erhoehung"  // rent increase possible
	DeadlineIndex      = "index"      // index adjustment possible
	DeadlineEnd        = "ende"       // lease ends
	DeadlineDeposit    = "kaution"    // settle the deposit
	DeadlineArrears    = "rueckstand" // rent arrears
	DeadlineManual     = "manuell"    // reminder added by the user
)

// Time spans of the assignment, ledger and deadlines.
const (
	// A payment matches a lease from this many days before its start ...
	leaseMatchDaysBefore = 45
	// ... until this many days after its end (late payments, back payments).
	leaseMatchDaysAfter = 60
	// Payments this many days before the tracked period still count (early rent).
	earlyPaymentDays = 20
	// A deadline this close is shown as urgent.
	soonDays = 30
	// The utility settlement is urgent this many months before its deadline.
	settlementUrgentMonths = 2
	// Lease ends are shown this many months in advance.
	leaseEndNoticeMonths = 3
	// Rent increase to the comparative rent: at the earliest 15 months after the last one (§558 BGB).
	increaseWaitMonths = 15
	// Index adjustment: at the earliest one year after the last one (§557b BGB).
	indexWaitYears = 1
	// UpcomingMonths is the window of deadlines on the overview.
	UpcomingMonths = 2
)
