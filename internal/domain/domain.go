// Package domain defines the vocabulary the other packages share: the two books,
// category groups, kinds and statuses of recurring payments, who set a category,
// setting keys, date formats and payment cycles.
//
// The values are stored in the database (see the CHECK constraints in the
// migrations) and sent to the web UI unchanged, so they must not be renamed.
package domain

// Book is the part of the app an account belongs to.
type Book string

const (
	BookHousehold Book = "haushalt"   // Haushaltsbuch: personal budget
	BookProperty  Book = "verwaltung" // Hausverwaltung: rental properties
)

// ParseBook maps anything but the property book to the household book.
func ParseBook(s string) Book {
	if Book(s) == BookProperty {
		return BookProperty
	}
	return BookHousehold
}

// Group is the top level of the category tree.
type Group string

const (
	GroupIncome   Group = "income"
	GroupBills    Group = "bills"    // fixed costs
	GroupExpenses Group = "expenses" // variable spending
	GroupSavings  Group = "savings"
	GroupDebts    Group = "debts"
	GroupTransfer Group = "transfer" // between own accounts, never counted
)

// Valid reports whether g is one of the known groups.
func (g Group) Valid() bool {
	switch g {
	case GroupIncome, GroupBills, GroupExpenses, GroupSavings, GroupDebts, GroupTransfer:
		return true
	}
	return false
}

// Direction of a payment: money coming in or going out.
type Direction string

const (
	DirectionIn  Direction = "in"
	DirectionOut Direction = "out"
)

// DirectionOf returns the direction of a signed amount.
func DirectionOf(cents int64) Direction {
	if cents > 0 {
		return DirectionIn
	}
	return DirectionOut
}

// Owner is the account holder for the couple split.
type Owner string

const (
	OwnerA     Owner = "A"
	OwnerB     Owner = "B"
	OwnerJoint Owner = "" // joint account
)

// Valid reports whether o is one of the known owners.
func (o Owner) Valid() bool { return o == OwnerA || o == OwnerB || o == OwnerJoint }

// Source says who set the category of a transaction.
type Source string

const (
	SourceAuto   Source = "auto"   // classifier
	SourceRule   Source = "rule"   // one of the user's rules
	SourceManual Source = "manual" // chosen by hand, never overwritten
)

// Kind of a recurring payment.
type Kind string

const (
	KindSubscription Kind = "abo"
	KindFixedCost    Kind = "fixkosten"
	KindLoan         Kind = "kredit"
	KindIncome       Kind = "einkommen"
	KindSavings      Kind = "sparen"
	KindOther        Kind = "sonstiges"
)

// Valid reports whether k is one of the known kinds.
func (k Kind) Valid() bool {
	switch k {
	case KindSubscription, KindFixedCost, KindLoan, KindIncome, KindSavings, KindOther:
		return true
	}
	return false
}

// RecurringStatus is the user's verdict on a detected series.
type RecurringStatus string

const (
	StatusDetected  RecurringStatus = "detected"
	StatusConfirmed RecurringStatus = "confirmed"
	StatusIgnored   RecurringStatus = "ignored"
)

// Valid reports whether s is one of the known statuses.
func (s RecurringStatus) Valid() bool {
	return s == StatusDetected || s == StatusConfirmed || s == StatusIgnored
}

// ConnStatus is the state of a bank connection.
type ConnStatus string

const (
	ConnPending ConnStatus = "pending" // user is at the bank
	ConnActive  ConnStatus = "active"
	ConnExpired ConnStatus = "expired" // consent ran out or was revoked by the bank
	ConnError   ConnStatus = "error"
	ConnRevoked ConnStatus = "revoked" // disconnected by the user
)

// PeriodMode says how budget months are cut.
type PeriodMode string

const (
	PeriodSalary   PeriodMode = "salary"   // from one salary to the next
	PeriodCalendar PeriodMode = "calendar" // 1st to end of month
)

// RuleField is the transaction field a user rule matches.
type RuleField string

const (
	RuleMerchant     RuleField = "merchant"
	RuleCounterparty RuleField = "counterparty"
	RuleIBAN         RuleField = "iban"
	RuleRemittance   RuleField = "remittance"
)

// Keys in the settings table.
const (
	SettingNameA        = "name_a"        // display name of owner A
	SettingNameB        = "name_b"        // display name of owner B
	SettingOwnNames     = "own_names"     // extra own names, comma-separated
	SettingPeriodMode   = "period_mode"   // PeriodMode
	SettingSalarySeries = "salary_series" // recurring IDs that start a period, comma-separated
)

// Category slugs the code refers to. The full list lives in the migrations.
const (
	SlugOther          = "sonstiges"       // spending the classifier could not place
	SlugOtherIncome    = "einnahmen-sonst" // income the classifier could not place
	SlugSalary         = "gehalt"
	SlugRefund         = "erstattung"
	SlugRental         = "vermietung"          // money from the rent account
	SlugRentalSubsidy  = "zuschuss-vermietung" // money to the rent account
	SlugTransfer       = "umbuchung"
	SlugSavings        = "sparen"
	SlugSubscriptions  = "abos"
	SlugMemberships    = "mitgliedschaft"
	SlugLoans          = "kredite"
	SlugCreditCard     = "kreditkarte"
	SlugOnlineShopping = "online-shopping"
	SlugLeisure        = "freizeit"
	SlugCash           = "bargeld"
	SlugBankFees       = "kontofuehrung" // account fees, closings, overdraft interest
)

// CatchAllSlugs are the categories a transaction lands in when the classifier
// found nothing. It counts as "not assigned" until the user confirms it.
var CatchAllSlugs = []string{SlugOther, SlugOtherIncome}
