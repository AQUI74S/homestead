package domain

import "strings"

var umlauts = strings.NewReplacer("ä", "ae", "ö", "oe", "ü", "ue", "ß", "ss")

// Fold lowercases s and spells out German umlauts ("Müller" -> "mueller"), so
// that names match no matter how a bank encodes them.
func Fold(s string) string { return umlauts.Replace(strings.ToLower(s)) }

// NormIBAN returns the IBAN in upper case without spaces.
func NormIBAN(s string) string { return strings.ToUpper(strings.ReplaceAll(s, " ", "")) }
